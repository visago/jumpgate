# Jumpgate Version b3ccba5-dev Performance tests

## IPERF Native performance, single connection
```
# iperf -c 127.0.0.1 --port 5001 --parallel 1
------------------------------------------------------------
Client connecting to 127.0.0.1, TCP port 5001
TCP window size: 2.50 MByte (default)
------------------------------------------------------------
[  3] local 127.0.0.1 port 49266 connected with 127.0.0.1 port 5001
[ ID] Interval       Transfer     Bandwidth
[  3]  0.0-10.0 sec  69.3 GBytes  59.6 Gbits/sec
```

## IPERF Native performance, 10 connection
```
# iperf -c 127.0.0.1 --port 5001 --parallel 10
------------------------------------------------------------
Client connecting to 127.0.0.1, TCP port 5001
TCP window size: 2.50 MByte (default)
------------------------------------------------------------
[  3] local 127.0.0.1 port 49268 connected with 127.0.0.1 port 5001
[ ID] Interval       Transfer     Bandwidth
[  3]  0.0-10.0 sec  68.6 GBytes  58.9 Gbits/sec
```

## IPERF Jumpgate performance, single connection
```
# iperf -c 127.0.0.1 --port 5002 --parallel 1
------------------------------------------------------------
Client connecting to 127.0.0.1, TCP port 5002
TCP window size: 2.50 MByte (default)
------------------------------------------------------------
[  3] local 127.0.0.1 port 33112 connected with 127.0.0.1 port 5002
[ ID] Interval       Transfer     Bandwidth
[  3]  0.0-10.0 sec  68.5 GBytes  58.8 Gbits/sec
```

## IPERF Jumpgate performance, 10 connection
```
# iperf -c 127.0.0.1 --port 5002 --parallel 10
------------------------------------------------------------
Client connecting to 127.0.0.1, TCP port 5002
TCP window size: 2.50 MByte (default)
------------------------------------------------------------
[ 14] local 127.0.0.1 port 33152 connected with 127.0.0.1 port 5002
[  7] local 127.0.0.1 port 33146 connected with 127.0.0.1 port 5002
[  9] local 127.0.0.1 port 33154 connected with 127.0.0.1 port 5002
[  4] local 127.0.0.1 port 33120 connected with 127.0.0.1 port 5002
[  6] local 127.0.0.1 port 33130 connected with 127.0.0.1 port 5002
[  5] local 127.0.0.1 port 33122 connected with 127.0.0.1 port 5002
[  8] local 127.0.0.1 port 33138 connected with 127.0.0.1 port 5002
[ 10] local 127.0.0.1 port 33136 connected with 127.0.0.1 port 5002
[ 12] local 127.0.0.1 port 33144 connected with 127.0.0.1 port 5002
[  3] local 127.0.0.1 port 33128 connected with 127.0.0.1 port 5002
[ ID] Interval       Transfer     Bandwidth
[ 14]  0.0-10.0 sec  8.48 GBytes  7.28 Gbits/sec
[  7]  0.0-10.0 sec  9.59 GBytes  8.23 Gbits/sec
[  4]  0.0-10.0 sec  9.80 GBytes  8.41 Gbits/sec
[  6]  0.0-10.0 sec  9.64 GBytes  8.27 Gbits/sec
[  5]  0.0-10.0 sec  9.70 GBytes  8.32 Gbits/sec
[  8]  0.0-10.0 sec  9.44 GBytes  8.11 Gbits/sec
[ 10]  0.0-10.0 sec  9.47 GBytes  8.13 Gbits/sec
[ 12]  0.0-10.0 sec  8.13 GBytes  6.98 Gbits/sec
[  9]  0.0-10.0 sec  8.54 GBytes  7.33 Gbits/sec
[  3]  0.0-10.0 sec  9.69 GBytes  8.31 Gbits/sec
[SUM]  0.0-10.0 sec  92.5 GBytes  79.4 Gbits/sec
```

## Jumpgate metrics
```
jumpgate_build_info{branch="main",buildTime="2021-07-06T09:58:14+00:00",goversion="go1.16.3",revision="b3ccba5ceb95fdd5a985a9301d07284ed17ad948",version="b3ccba5-dev"} 1
jumpgate_connections_total 11
jumpgate_proxy_rx_bytes 1.72823937024e+11
jumpgate_proxy_tx_bytes 0
```
