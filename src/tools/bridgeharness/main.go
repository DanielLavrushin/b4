package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:24500", "local listen addr")
	dst := flag.String("dst", "149.154.167.51", "original destination IP the client thinks it dialled")
	worker := flag.String("worker", "", "cloudflare worker domain(s), comma separated")
	cfproxy := flag.Bool("cfproxy", false, "enable shared CF proxy pool")
	flag.Parse()

	log.SetLevel(log.LevelDebug)

	cfg := &config.Config{}
	config.ApplyConfigDefaults(cfg)
	cfg.System.MTProto.UpstreamMode = "auto"
	cfg.System.MTProto.CFWorkerDomain = *worker
	cfg.System.MTProto.CFProxyEnabled = *cfproxy
	cfg.Queue.Mark = 0

	bridge := mtproto.NewTransparentBridge(cfg)

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	fmt.Printf("bridge harness on %s -> orig dst %s:443 (worker=%q cfproxy=%v)\n",
		*listen, *dst, *worker, *cfproxy)

	ip := net.ParseIP(*dst)
	for {
		c, err := ln.Accept()
		if err != nil {
			fmt.Fprintln(os.Stderr, "accept:", err)
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			handled, passthrough := bridge.Handle(c, ip, 443)
			if !handled {
				fmt.Println("bridge failed open, passthrough =", passthrough != nil)
			}
		}(c)
	}
}
