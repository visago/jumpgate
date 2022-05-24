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
	Status  uint8 // 0 - new, 1 - active, 8 - closing, 9 - closed
	Name    string
	RxBytes int64
	TxBytes int64
}

var connectionMap map[uint]*connectionStruct
var connectionMapMutex sync.RWMutex

const (
	MangleNone  uint = 0
	MangleClose uint = 1
	MangleDrop  uint = 2
	MangleLag   uint = 3
)

var mangleMode uint
var mangleLag uint64      // Seconds to lag before mangle
var manglePercent int = 0 // Percentage chance of mangle (% chance a mangle will occur, default 0)
var mangleMutex sync.RWMutex

var (
	metricsConnectionTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: applicationName + "_connections_total",
		Help: "The total number of connections",
	})
	metricsProxyRx = promauto.NewCounter(prometheus.CounterOpts{
		Name: applicationName + "_proxy_rx_bytes",
		Help: "Bytes received by the service",
	})
	metricsProxyTx = promauto.NewCounter(prometheus.CounterOpts{
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
	log.Printf("quit")
}

func deferCleanup() { // Installs a handler to perform clean up
	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGINT, syscall.SIGPIPE)
	go func() {
		<-c
		cleanup()
		os.Exit(1)
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
	http.Handle("/metrics", promhttp.Handler()) // Do we really want this ?
	go func() {
		if err := http.ListenAndServe(flagMetricsListen, nil); err != nil {
			log.Fatalf("FATAL: Failed to start metrics http engine - %v", err)
		}
	}()
	log.Printf("%s metrics engine listening on %s", applicationName, flagMetricsListen)
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
		fmt.Fprintf(w, "Mangle mode is now %s(%0d)\n", requestMode, mangleMode)
	} else {
		fmt.Fprintf(w, "Mangle mode not set. Set ?mode=<close|drop|lag|none>\n")
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
		connectionID++ // This is usually a uint64, and will overflow to 0
		connectionMap[connectionID] = &connectionStruct{}
		connectionMap[connectionID].Status = 1
		connectionMapMutex.Unlock()
		go handleRequest(conn, flagTarget, connectionID)
	}
}

func handleRequest(conn net.Conn, flagTarget string, connectionID uint) {
	if flagDebug { // This is optional since we will have the next section
		log.Printf("[%0d] Connection from %s to %s", connectionID, conn.RemoteAddr().String(), conn.LocalAddr())
	}
	if rand.Intn(100) > (100 - manglePercent) {
		if mangleMode == MangleDrop { // We don't close the connection for DROP
			if flagVerbose {
				log.Printf("[%0d] Mangle: DROP %s", connectionID, conn.RemoteAddr().String())
			}
			return
		} else if mangleMode == MangleClose {
			if mangleLag > 0 {
				if flagVerbose {
					log.Printf("[%0d] Mangle: LAG-CLOSE %s START %0ds", connectionID, conn.RemoteAddr().String(), mangleLag)
				}
				time.Sleep(time.Duration(mangleLag) * time.Second)
				if flagVerbose {
					log.Printf("[%0d] Mangle: LAG-CLOSE %s DONE %0ds", connectionID, conn.RemoteAddr().String(), mangleLag)
				}
			}
			if flagVerbose {
				log.Printf("[%0d] Mangle: CLOSE %s", connectionID, conn.RemoteAddr().String())
			}
			closeConnection(conn, connectionID)
			return
		} else if mangleMode == MangleLag {
			if mangleLag > 0 {
				if flagVerbose {
					log.Printf("[%0d] Mangle: LAG %s START %0ds", connectionID, conn.RemoteAddr().String(), mangleLag)
				}
				time.Sleep(time.Duration(mangleLag) * time.Second)
				if flagVerbose {
					log.Printf("[%0d] Mangle: LAG %s DONE %0ds", connectionID, conn.RemoteAddr().String(), mangleLag)
				}
			} else {
				log.Printf("[%0d] Mangle: LAG %s NOSLEEP %0ds", connectionID, conn.RemoteAddr().String(), mangleLag)
			}
		}
	}
	proxy, err := net.Dial("tcp", flagTarget)
	if err != nil {
		log.Printf("[%0d] ERROR: Failed to connect to %s - %v", connectionID, flagTarget, err)
		closeConnection(conn, connectionID)
		return
	}
	if flagVerbose {
		log.Printf("[%0d] Forwarding %s to %s", connectionID, conn.RemoteAddr(), proxy.RemoteAddr().String())
	}

	// server <- proxy <- applicationName -> conn -> user
	go forwardIO(conn, proxy, connectionID, true)  // Packets from server to user (tx)
	go forwardIO(proxy, conn, connectionID, false) // Packets from user to server (tx)
}

func forwardIO(dst net.Conn, src net.Conn, connectionID uint, tx bool) { // rx flag is just for traffic direction
	defer closeConnections(dst, src, connectionID, tx)
	bytesCopied, err := io.Copy(dst, src) // func Copy(dst Writer, src Reader)
	if err != nil {
		if flagDebug {
			log.Printf("[%0d] COPY-ERROR: Copying %s -> %s [%0d bytes] [TX %t] - %v", connectionID, dst.LocalAddr(), dst.RemoteAddr(), bytesCopied, tx, err)
		}
	} else {
		if flagDebug {
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

func closeConnections(src net.Conn, dst net.Conn, connectionID uint, tx bool) {
	src.Close()
	dst.Close()
	connectionMapMutex.Lock()
	if connectionMap[connectionID] != nil { // Exists, first delete
		if connectionMap[connectionID].Status < 8 {
			if flagDebug {
				log.Printf("[%0d] Closing %s to %s [TX %0d / RX %0d]", connectionID, src.RemoteAddr(), dst.RemoteAddr(), connectionMap[connectionID].TxBytes, connectionMap[connectionID].RxBytes)
			}
			connectionMap[connectionID].Status = 8
		} else if connectionMap[connectionID].Status == 8 {
			if flagVerbose {
				log.Printf("[%0d] Closed %s to %s [TX %0d / RX %0d]", connectionID, src.RemoteAddr(), dst.RemoteAddr(), connectionMap[connectionID].TxBytes, connectionMap[connectionID].RxBytes)
			}
			connectionMap[connectionID].Status = 9
			delete(connectionMap, connectionID)
		}
	} else {
		if flagDebug {
			log.Printf("[%0d] Already closed %s to %s [TX %t]", connectionID, src.RemoteAddr(), dst.RemoteAddr(), tx)
		}
	}
	connectionMapMutex.Unlock()
}

func closeConnection(src net.Conn, connectionID uint) { // This is usually used when the target connection is not created yet
	src.Close()
	connectionMapMutex.Lock()
	if connectionMap[connectionID] != nil { // Exists, first delete
		if connectionMap[connectionID].Status < 8 {
			if flagDebug {
				log.Printf("[%0d] Closing %s [TX %0d / RX %0d]", connectionID, src.RemoteAddr(), connectionMap[connectionID].TxBytes, connectionMap[connectionID].RxBytes)
			}
			connectionMap[connectionID].Status = 8
		} else if connectionMap[connectionID].Status == 8 {
			if flagVerbose {
				log.Printf("[%0d] Closed %s [TX %0d / RX %0d]", connectionID, src.RemoteAddr(), connectionMap[connectionID].TxBytes, connectionMap[connectionID].RxBytes)
			}
			connectionMap[connectionID].Status = 9
			delete(connectionMap, connectionID)
		}
	} else {
		if flagDebug {
			log.Printf("[%0d] Already closed %s", connectionID, src.RemoteAddr())
		}
	}
	connectionMapMutex.Unlock()
}
