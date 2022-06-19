package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var flagSource string
var flagTarget string
var flagVerbose bool
var flagVersion bool
var flagDebug bool
var flagMetricsListen string
var flagPIDFile string

var BuildBranch string
var BuildVersion string
var BuildTime string
var BuildRevision string

const applicationName = "jumpgate"

type connectionStruct struct {
	Name        string
	ConnectTime time.Time
	Status      uint8 // See Connection*
	Mode        uint  // See Mangle*
	RxBytes     int64
	TxBytes     int64
	SrcConn     net.Conn
	DstConn     net.Conn
}

var connectionMap map[uint]*connectionStruct
var connectionMapMutex sync.RWMutex

const (
	MangleNone  uint = 0
	MangleClose uint = 1
	MangleDrop  uint = 2
	MangleLag   uint = 3
)

const (
	ConnectionNew        uint8 = 1 // Fresh and allocated
	ConnectionConnecting uint8 = 2 // When the connection is first connecting
	ConnectionConnected  uint8 = 3 // When the connection is completed (post-mangle)
	ConnectionOver       uint8 = 6 // When the last byte has been forwarded
	ConnectionClosing    uint8 = 7 // Closing the connection
	ConnectionClosed     uint8 = 8 // Connection closed
	ConnectionUnused     uint8 = 9 // After the connection is closed
)

var mangleMode uint
var mangleLag uint64      // Seconds to lag before mangle
var manglePercent int = 0 // Percentage chance of mangle (% chance a mangle will occur, default 0)
var mangleMutex sync.RWMutex

var (
	metricsConnectionID = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_connections_id",
		Help: "The current connection ID",
	})
	metricsConnectionTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_connections_total",
		Help: "The total number of connections",
	})
	metricsMangledConnectionTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_connections_mangled_total",
		Help: "The total number of mangled connections",
	})
	metricsErrorsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_errors_total",
		Help: "The total number of errors",
	})
	metricsProxyRx = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_proxy_rx_bytes",
		Help: "Bytes received by the service",
	})
	metricsProxyTx = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_proxy_tx_bytes",
		Help: "Bytes transmitted by the service",
	})
	mangleModeGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_mangle_mode",
		Help: "Mangle mode",
	})
	mangleLagGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_mangle_lag",
		Help: "Mangle lag before mangle",
	})
	manglePercentGauge = promauto.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_mangle_percent",
		Help: "Mangle percentage of connections",
	})
)

func main() {
	log.Printf("%s version %s (Rev: %s Branch: %s) built on %s", applicationName, BuildVersion, BuildRevision, BuildBranch, BuildTime)
	parseFlags()
	if len(flagPIDFile) > 0 {
		deferCleanup() // This installs a handler to remove PID file when we quit
		savePIDFile(flagPIDFile)
	}
	if len(flagMetricsListen) > 0 { // Start metrics engine
		metricHttpServerStart()
	}
	listenLoop()
	log.Printf("Quit")
}

func deferCleanup() { // Installs a handler to perform clean up
	// https://stackoverflow.com/questions/18908698/go-signal-handling
	c := make(chan os.Signal)
	// signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGPIPE)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-c
		switch sig {
		case syscall.SIGPIPE: // Not installed
			log.Printf("Received signal %v, ignoring", sig)
			metricsErrorsTotal.Inc()
		default:
			log.Printf("Received signal %v, exiting", sig)
			cleanup()
			os.Exit(1)
		}
	}()

}

func cleanup() {
	if len(flagPIDFile) > 0 {
		os.Remove(flagPIDFile)
	}
	log.Printf("%s perform clean up on process end", applicationName)
}

func parseFlags() {
	flag.StringVar(&flagMetricsListen, "metrics-listen", "", "metrics listener <host>:<port>") // Recommend 0.0.0.0:9878
	flag.StringVar(&flagSource, "source", "", "source <host>:<port>")
	flag.StringVar(&flagTarget, "target", "", "target <host>:<port>")
	flag.StringVar(&flagPIDFile, "pidfile", "", "pidfile")
	flag.BoolVar(&flagVerbose, "verbose", false, "verbose flag")
	flag.BoolVar(&flagDebug, "debug", false, "debug flag")
	flag.BoolVar(&flagVersion, "version", false, "get version")
	flag.Parse()
	if flagDebug {
		flagVerbose = true // Its confusing if flagDebug is on, but flagVerbose isn't
	}
	if flagVersion { // Only print version (We always print version), then exit.
		os.Exit(0)
	}
	if len(flagSource) == 0 || len(flagTarget) == 0 {
		log.Fatal("Please provide --source and --target")
	}
}

func savePIDFile(pidFile string) {
	file, err := os.Create(pidFile)
	if err != nil {
		log.Fatalf("Unable to create pid file : %v", err)
	}
	defer file.Close()

	pid := os.Getpid()
	if _, err = file.WriteString(strconv.Itoa(pid)); err != nil {
		log.Fatalf("Unable to create pid file : %v", err)
	}
	if flagVerbose {
		log.Printf("Wrote PID %0d to %s", pid, flagPIDFile)
	}

	file.Sync() // flush to disk

}

func metricHttpServerStart() {
	var buildInfoMetric = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: applicationName + "_build_info", Help: "Shows the build info/version",
		ConstLabels: prometheus.Labels{"branch": BuildBranch, "revision": BuildRevision, "version": BuildVersion, "buildTime": BuildTime, "goversion": runtime.Version()}})
	prometheus.MustRegister(buildInfoMetric)
	buildInfoMetric.Set(1)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><a href=/metrics>metrics</a></body></html>"))
	})
	http.HandleFunc("/mangle", mangleServer)
	http.HandleFunc("/mangle/reset", resetConnections)
	http.HandleFunc("/mangle/dump", dumpConnections)
	http.HandleFunc("/metrics/reset", resetMetrics)
	http.Handle("/metrics", promhttp.Handler())
	go func() {
		if err := http.ListenAndServe(flagMetricsListen, nil); err != nil {
			log.Fatalf("FATAL: Failed to start metrics http engine - %v", err)
		}
	}()
	log.Printf("%s metrics engine listening on %s", applicationName, flagMetricsListen)
}

func resetMetrics(w http.ResponseWriter, r *http.Request) {
	mangleMutex.Lock()
	metricsMangledConnectionTotal.Set(0)
	metricsConnectionTotal.Set(0)
	metricsProxyRx.Set(0)
	metricsProxyTx.Set(0)
	mangleMutex.Unlock()
	log.Printf("Metrics resetted\n")
	fmt.Fprintf(w, "Metrics resetted\n")
}

func resetConnections(w http.ResponseWriter, r *http.Request) {
	mangleMutex.Lock()
	connectionMapMutex.Lock()
	count := 0
	connectionIDs := make([]uint, 0, len(connectionMap))
	for connectionID, _ := range connectionMap {
		connectionIDs = append(connectionIDs, connectionID)
		count++
	}
	connectionMapMutex.Unlock()
	sort.Slice(connectionIDs, func(i, j int) bool { return connectionIDs[i] < connectionIDs[j] })
	for _, connectionID := range connectionIDs {
		closeConn(connectionID)
	}
	log.Printf("%0d connections resetted\n", count)
	fmt.Fprintf(w, "%0d connections resetted\n", count)
	mangleMutex.Unlock()
}

func dumpConnections(w http.ResponseWriter, r *http.Request) {
	connectionMapMutex.Lock()
	connectionIDs := make([]uint, 0, len(connectionMap))
	count := 0
	for connectionID, _ := range connectionMap {
		connectionIDs = append(connectionIDs, connectionID)
		count++
	}
	sort.Slice(connectionIDs, func(i, j int) bool { return connectionIDs[i] < connectionIDs[j] })
	for _, connectionID := range connectionIDs {
		fmt.Fprintf(w, "[%5d] %2d %d - %s\n", connectionID, connectionMap[connectionID].Status, connectionMap[connectionID].Mode, connectionMap[connectionID].ConnectTime.Format(time.RFC3339))
	}
	connectionMapMutex.Unlock()
	log.Printf("%0d connections dumped\n", count)
}

func mangleServer(w http.ResponseWriter, r *http.Request) {
	requestMode := r.URL.Query().Get("mode")
	requestLag := r.URL.Query().Get("lag")
	requestRandom := r.URL.Query().Get("percent")
	mangleMutex.Lock()
	if len(requestMode) > 0 {
		switch requestMode {
		case "close":
			mangleMode = MangleClose
			manglePercent = 100
			mangleLag = 0
		case "drop":
			mangleMode = MangleDrop
			manglePercent = 100
			mangleLag = 0
		case "lag":
			mangleMode = MangleLag
			manglePercent = 100
			mangleLag = 1
		case "none":
			mangleMode = MangleNone
			manglePercent = 0
			mangleLag = 0
		default:
			mangleMode = MangleNone
			manglePercent = 0
			mangleLag = 0
			requestMode = "none"
		}
		mangleModeGauge.Set(float64(mangleMode))
		log.Printf("Mangle mode is now %s(%0d)\n", requestMode, mangleMode)
	}
	if len(requestLag) > 0 {
		i, err := strconv.ParseUint(requestLag, 10, 64)
		if err != nil {
			// handle error
			i = 1
		}
		mangleLag = i
		log.Printf("Mangle lag is now %0d\n", mangleLag)
	}
	mangleLagGauge.Set(float64(mangleLag))

	if len(requestRandom) > 0 {
		i, err := strconv.Atoi(requestRandom)
		if err != nil {
			// handle error
			i = 1
		}
		manglePercent = i
		if manglePercent < 0 {
			manglePercent = 0
		} else if manglePercent > 100 {
			manglePercent = 100
		}
		log.Printf("Mangle will occur on %0d%% of connections\n", manglePercent)

	}

	if len(requestLag) > 0 || len(requestMode) > 0 || len(requestRandom) > 0 {
		fmt.Fprintf(w, "Mangle mode is now %s(%0d) for %0d%% of connections (With %0ds of lag)\n", requestMode, mangleMode, manglePercent, mangleLag)
	} else {
		fmt.Fprintf(w, "Mangle mode not set. Set ?mode=<close|drop|lag|none>&percent=<percentage>&lag=<seconds>\n")
	}

	manglePercentGauge.Set(float64(manglePercent)) // We use / as its default handler
	mangleMutex.Unlock()
}

func listenLoop() {
	connectionMap = make(map[uint]*connectionStruct)
	var connectionID uint
	listener, err := net.Listen("tcp", flagSource)
	if err != nil {
		log.Fatalf("PANIC: Fail to listen on %s - %v", flagSource, err)
	}
	log.Printf("%s listening on %s", applicationName, flagSource)
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatalf("FATAL: Fail to accept connection - %v", err) // Should this even be fatal ?
		}
		metricsConnectionTotal.Inc()
		connectionMapMutex.Lock()
		connectionID = 0
		for _, exist := connectionMap[connectionID]; exist; _, exist = connectionMap[connectionID] { // This ensures we have no collision
			connectionID++
		}
		metricsConnectionID.Set(float64(connectionID))
		connectionMap[connectionID] = &connectionStruct{}
		connectionMap[connectionID].Status = ConnectionConnecting
		connectionMap[connectionID].Mode = mangleMode
		connectionMap[connectionID].SrcConn = conn
		connectionMap[connectionID].ConnectTime = time.Now()
		if flagVerbose {
			log.Printf("[%0d] Created new connection from %s [Total %0d connections]", connectionID, conn.RemoteAddr().String(), len(connectionMap))
		}
		connectionMapMutex.Unlock()
		go handleRequest(flagTarget, connectionID)
	}
}

func handleRequest(flagTarget string, connectionID uint) {
	if flagDebug { // This is optional since we will have the next section
		log.Printf("[%0d] Connection from %s to %s", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr().String(), connectionMap[connectionID].SrcConn.LocalAddr())
	}
	if rand.Intn(100) > (100 - manglePercent) {
		metricsMangledConnectionTotal.Inc()
		if mangleMode == MangleDrop { // We don't close the connection for DROP
			if flagVerbose {
				log.Printf("[%0d] Mangle: DROP %s", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr().String())
			}
			return
		} else if mangleMode == MangleClose {
			if mangleLag > 0 {
				if flagVerbose {
					log.Printf("[%0d] Mangle: LAG-CLOSE %s START %0ds", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr().String(), mangleLag)
				}
				time.Sleep(time.Duration(mangleLag) * time.Second)
				if flagVerbose {
					log.Printf("[%0d] Mangle: LAG-CLOSE %s DONE %0ds", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr().String(), mangleLag)
				}
			}
			if flagVerbose {
				log.Printf("[%0d] Mangle: CLOSE %s", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr().String())
			}
			closeConn(connectionID)
			return
		} else if mangleMode == MangleLag {
			if mangleLag > 0 {
				if flagVerbose {
					log.Printf("[%0d] Mangle: LAG %s START %0ds", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr().String(), mangleLag)
				}
				time.Sleep(time.Duration(mangleLag) * time.Second)
				if flagVerbose {
					log.Printf("[%0d] Mangle: LAG %s DONE %0ds", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr().String(), mangleLag)
				}
			} else {
				log.Printf("[%0d] Mangle: LAG %s NOSLEEP %0ds", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr().String(), mangleLag)
			}
		}
	}
	proxy, err := net.Dial("tcp", flagTarget)
	if err != nil {
		log.Printf("[%0d] ERROR: Failed to connect to %s - %v", connectionID, flagTarget, err)
		closeConn(connectionID)
		return
	}
	connectionMapMutex.Lock()
	if flagVerbose {
		log.Printf("[%0d] Forwarding %s to %s", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr(), proxy.RemoteAddr().String())
	}
	connectionMap[connectionID].Status = ConnectionConnected
	connectionMap[connectionID].DstConn = proxy
	connectionMapMutex.Unlock()
	// server <- proxy <- applicationName -> conn -> user
	go forwardIO(connectionID, true)  // Packets from server to user (tx)
	go forwardIO(connectionID, false) // Packets from user to server (rx)
}

func forwardIO(connectionID uint, tx bool) { // rx flag is just for traffic direction
	defer closeConn(connectionID)
	var dst, src net.Conn
	connectionMapMutex.Lock()
	if tx {
		dst = connectionMap[connectionID].SrcConn
		src = connectionMap[connectionID].DstConn
	} else {
		dst = connectionMap[connectionID].DstConn
		src = connectionMap[connectionID].SrcConn
	}
	connectionMapMutex.Unlock()

	bytesCopied, err := io.Copy(dst, src) // func Copy(dst Writer, src Reader)
	if flagDebug {
		if err != nil {
			log.Printf("[%0d] COPY-ERROR: Copying %s -> %s [%0d bytes] [TX %t] - %v", connectionID, dst.LocalAddr(), dst.RemoteAddr(), bytesCopied, tx, err)
		} else {
			log.Printf("[%0d] COPY-EOF: Copying %s -> %s [%0d bytes]", connectionID, dst.LocalAddr(), dst.RemoteAddr(), bytesCopied)
		}
	}

	connectionMapMutex.Lock()
	if connectionMap[connectionID] != nil {
		if tx {
			connectionMap[connectionID].TxBytes = bytesCopied
		} else {
			connectionMap[connectionID].RxBytes = bytesCopied
		}
	}
	connectionMapMutex.Unlock()
	if tx {
		metricsProxyTx.Add(float64(bytesCopied))
	} else {
		metricsProxyRx.Add(float64(bytesCopied))
	}

}

func closeConn(connectionID uint) { // Close a connection
	connectionMapMutex.Lock()
	if flagVerbose {
		log.Printf("[%0d] Closing connection from %s", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr())
	}
	if connectionMap[connectionID] != nil { // Exists, first delete
		singleConn := true
		if connectionMap[connectionID].DstConn != nil {
			connectionMap[connectionID].DstConn.Close()
			singleConn = false
		}
		if connectionMap[connectionID].SrcConn != nil {
			connectionMap[connectionID].SrcConn.Close()
		}

		if connectionMap[connectionID].Status == ConnectionClosing || singleConn {
			if flagVerbose {
				log.Printf("[%0d] Closed %s [TX %0d / RX %0d]", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr(), connectionMap[connectionID].TxBytes, connectionMap[connectionID].RxBytes)
			}
			connectionMap[connectionID].Status = ConnectionClosed
			delete(connectionMap, connectionID)
		} else if connectionMap[connectionID].Status < ConnectionClosing {
			if flagDebug {
				log.Printf("[%0d] Closing %s [TX %0d / RX %0d]", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr(), connectionMap[connectionID].TxBytes, connectionMap[connectionID].RxBytes)
			}
			connectionMap[connectionID].Status = ConnectionClosing
		}
	} else {
		if flagDebug {
			log.Printf("[%0d] Already closed %s", connectionID, connectionMap[connectionID].SrcConn.RemoteAddr())
		}
	}
	connectionMapMutex.Unlock()

}
