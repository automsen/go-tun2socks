package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/eycorsican/go-tun2socks/common/blockdns"
	"github.com/eycorsican/go-tun2socks/common/dns"
	"github.com/eycorsican/go-tun2socks/common/log"
	_ "github.com/eycorsican/go-tun2socks/common/log/simple" // Register a simple logger.
	"github.com/eycorsican/go-tun2socks/core"
	"github.com/eycorsican/go-tun2socks/filter"
	"github.com/eycorsican/go-tun2socks/tun"
)

var version = "undefined"

var handlerCreater = make(map[string]func(), 0)

func registerHandlerCreater(name string, creater func()) {
	handlerCreater[name] = creater
}

var postFlagsInitFn = make([]func(), 0)

func addPostFlagsInitFn(fn func()) {
	postFlagsInitFn = append(postFlagsInitFn, fn)
}

type CmdArgs struct {
	Version               *bool
	TunName               *string
	TunAddr               *string
	TunGw                 *string
	TunMask               *string
	TunDns                *string
	ProxyType             *string
	VConfig               *string
	SniffingType          *string
	ProxyServer           *string
	ProxyHost             *string
	ProxyPort             *uint16
	ProxyCipher           *string
	ProxyPassword         *string
	DelayICMP             *int
	RelayICMP             *bool
	UdpTimeout            *time.Duration
	DisableDnsCache       *bool
	DnsFallback           *bool
	LogLevel              *string
	EnableFakeDns         *bool
	FakeDnsMinIP          *string
	FakeDnsMaxIP          *string
	FakeDnsCacheDir       *string
	FakeDnsExcludeDomains *string
	ExceptionApps         *string
	ExceptionSendThrough  *string
	Stats                 *bool
	SendThrough           *string
	RpcPort               *int
	Encry                 *bool
	EncryKey              *string
}

type cmdFlag uint

const (
	fProxyServer cmdFlag = iota
	fUdpTimeout
)

var flagCreaters = map[cmdFlag]func(){
	fProxyServer: func() {
		if args.ProxyServer == nil {
			args.ProxyServer = flag.String("proxyServer", "1.2.3.4:1087", "Proxy server address")
		}
	},
	fUdpTimeout: func() {
		if args.UdpTimeout == nil {
			args.UdpTimeout = flag.Duration("udpTimeout", 1*time.Minute, "UDP session timeout")
		}
	},
}

func (a *CmdArgs) addFlag(f cmdFlag) {
	if fn, found := flagCreaters[f]; found && fn != nil {
		fn()
	} else {
		log.Fatalf("unsupported flag")
	}
}

var args = new(CmdArgs)

var lwipWriter io.Writer

var dnsCache dns.DnsCache

var fakeDns dns.FakeDns

const (
	MTU = 1500
)

func main() {
	args.Version = flag.Bool("version", false, "Print version")
	args.TunName = flag.String("tunName", "tun1", "TUN interface name")
	args.TunAddr = flag.String("tunAddr", "10.255.0.2", "TUN interface address")
	args.TunGw = flag.String("tunGw", "10.255.0.1", "TUN interface gateway")
	args.TunMask = flag.String("tunMask", "255.255.255.0", "TUN interface netmask, it should be a prefixlen (a number) for IPv6 address")
	args.TunDns = flag.String("tunDns", "223.5.5.5,1.1.1.1", "DNS resolvers for TUN interface (only need on Windows)")
	args.ProxyType = flag.String("proxyType", "socks", "Proxy handler type")
	args.DelayICMP = flag.Int("delayICMP", 10, "Delay ICMP packets for a short period of time, in milliseconds")
	args.RelayICMP = flag.Bool("relayICMP", false, "Relay ICMP packets")
	args.LogLevel = flag.String("loglevel", "info", "Logging level. (debug, info, warn, error, none)")
	args.SendThrough = flag.String("sendThrough", "192.168.0.100", "Send through address.")
	args.RpcPort = flag.Int("rpcPort", 6002, "Management RPC port.")

	flag.Parse()

	if *args.Version {
		fmt.Println(version)
		os.Exit(0)
	}

	// Initialization ops after parsing flags.
	for _, fn := range postFlagsInitFn {
		if fn != nil {
			fn()
		}
	}

	// Set log level.
	switch strings.ToLower(*args.LogLevel) {
	case "debug":
		log.SetLevel(log.DEBUG)
	case "info":
		log.SetLevel(log.INFO)
	case "warn":
		log.SetLevel(log.WARN)
	case "error":
		log.SetLevel(log.ERROR)
	case "none":
		log.SetLevel(log.NONE)
	default:
		panic("unsupport logging level")
	}

	// Open the tun device.
	dnsServers := strings.Split(*args.TunDns, ",")
	tunDev, err := tun.OpenTunDevice(*args.TunName, *args.TunAddr, *args.TunGw, *args.TunMask, dnsServers)
	if err != nil {
		log.Fatalf("failed to open tun device: %v", err)
	}

	// Setup TCP/IP stack.
	lwipWriter := core.NewLWIPStack().(io.Writer)

	// Apply ICMP filters.
	if *args.RelayICMP {
		log.Infof("Relay ICMP packets.")
		privileged := false
		if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
			privileged = true
		}
		lwipWriter = filter.NewICMPRelayFilter(lwipWriter, tunDev, *args.SendThrough, privileged).(io.Writer)
	} else {
		if *args.DelayICMP > 0 {
			log.Infof("ICMP packets will be delayed for %dms", *args.DelayICMP)
			lwipWriter = filter.NewICMPEchoFilter(lwipWriter, *args.DelayICMP).(io.Writer)
		}
	}

	// Register TCP and UDP handlers to handle accepted connections.
	if creater, found := handlerCreater[*args.ProxyType]; found {
		creater()
	} else {
		log.Fatalf("unsupported proxy type")
	}

	if args.DnsFallback != nil && *args.DnsFallback {
		// Override the UDP handler with a DNS-over-TCP (fallback) UDP handler.
		if creater, found := handlerCreater["dnsfallback"]; found {
			creater()
		} else {
			log.Fatalf("DNS fallback connection handler not found, build with `dnsfallback` tag")
		}
	}

	// Register an output callback to write packets output from lwip stack to tun
	// device, output function should be set before input any packets.
	core.RegisterOutputFn(func(data []byte) (int, error) {
		return tunDev.Write(data)
	})

	// Copy packets from tun device to lwip stack, it's the main loop.
	go func() {
		_, err := io.CopyBuffer(lwipWriter, tunDev, make([]byte, MTU))
		if err != nil {
			log.Fatalf("copying data failed: %v", err)
		}
	}()

	log.Infof("Running tun2socks")

	if runtime.GOOS == "windows" {
		blockdns.FixDnsLeakage(*args.TunName)
		handleRpc()
	} else {
		osSignals := make(chan os.Signal, 1)
		signal.Notify(osSignals, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGHUP)
		<-osSignals
		stop()
	}
}

func handleRpc() {
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(*args.RpcPort)))
	if err != nil {
		panic(err)
	}
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			panic(err)
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := core.NewBytes(core.BufSize)
			defer core.FreeBytes(buf)
			n, err := c.Read(buf)
			if err != nil {
				log.Debugf("read rpc failed: %v", err)
			}
			switch string(buf[:n]) {
			case "SIGINT":
				stop()
				c.Write([]byte("OK"))
				c.Close()
				os.Exit(0)
			default:
			}
		}(conn)
	}
}

func stop() {
	if fakeDns != nil {
		err := fakeDns.Stop()
		if err != nil {
			log.Errorf("Error stopping Fake DNS: %v", err)
		}
	}
}
