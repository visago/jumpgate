# Jumpgate

## A simple TCP Connection Forwarder

As an exercise to learn more network development with
[Golang](https://golang.org), I decided to rewrite [jumpgate](http://jumpgate.sourceforge.net) myself. This
became a rabbit hole of a "simple" TCP connection forwarding to multiple new
features that I use. (I'm a big prometheus fan)

## Usage

Basic usage will require a source (Listening port, use 0.0.0.0 to bind to all interfaces) and a target service

```
jumpgate --source 0.0.0.0:8000 --target 127.0.0.0:80 
```

Adding `--verbose` and `--debug` adds more details. By default, it only logs complete connection
## Metrics

Another option you can add to jumpgate is `--metrics-listen 0.0.0.0:9878` to export prometheus style metrics. (All metrics are prefixed with jumpgate_*)

## Building

A simple `make` should suffice to build the binary after checkout

## History / Why ?

For many years, i was using [jumpgate](http://jumpgate.sourceforge.net) to forward connections from either a router/firewall to a service.
It provided a simple binary, and worked with Linux/BSD, and didn't need root
(Unlike IPTABLES) I've since moved on to using [socat](https://www.cyberciti.biz/faq/linux-unix-tcp-port-forwarding/) which works great, but lacked decent logging (It required -d -d before you get the IP details)

