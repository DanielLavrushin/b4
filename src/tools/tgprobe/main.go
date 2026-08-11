package main

import (
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
)

const protoAbridged = 0xefefefef

func reqPQ() []byte {
	body := make([]byte, 20)
	binary.LittleEndian.PutUint32(body[0:4], 0x60469778)
	_, _ = rand.Read(body[4:20])

	msg := make([]byte, 0, 40)
	msg = append(msg, make([]byte, 8)...)
	msgID := make([]byte, 8)
	binary.LittleEndian.PutUint64(msgID, (uint64(time.Now().Unix())<<32)|uint64(time.Now().Nanosecond()/1000)&0xfffffffc)
	msg = append(msg, msgID...)
	l := make([]byte, 4)
	binary.LittleEndian.PutUint32(l, uint32(len(body)))
	msg = append(msg, l...)
	msg = append(msg, body...)

	out := make([]byte, 0, 1+len(msg))
	out = append(out, byte(len(msg)/4))
	out = append(out, msg...)
	return out
}

func readPacket(c io.Reader) ([]byte, error) {
	var b [1]byte
	if _, err := io.ReadFull(c, b[:]); err != nil {
		return nil, err
	}
	n := int(b[0]&0x7f) * 4
	if b[0] == 0x7f {
		var ext [3]byte
		if _, err := io.ReadFull(c, ext[:]); err != nil {
			return nil, err
		}
		n = (int(ext[0]) | int(ext[1])<<8 | int(ext[2])<<16) * 4
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func main() {
	mode := flag.String("mode", "ws", "upstream_mode: ws|tcp|auto")
	worker := flag.String("worker", "", "cloudflare worker domain(s)")
	skipEdge := flag.Bool("skip-edge", true, "BridgeSkipNativeEdge")
	dc := flag.Int("dc", 4, "DC number")
	rounds := flag.Int("rounds", 15, "how many req_pq rounds")
	interval := flag.Duration("interval", 2*time.Second, "delay between rounds")
	mark := flag.Uint("mark", 0, "bypass fwmark for upstream dials")
	verbose := flag.Bool("v", false, "debug logging")
	flag.Parse()

	if *verbose {
		log.SetLevel(log.LevelDebug)
	} else {
		log.SetLevel(log.LevelError)
	}

	cfg := &config.Config{}
	config.ApplyConfigDefaults(cfg)
	mt := cfg.System.MTProto
	mt.UpstreamMode = *mode
	mt.CFWorkerDomain = *worker
	mt.DCRelay = ""
	mt.BridgeSkipNativeEdge = *skipEdge
	qc := cfg.Queue
	qc.Mark = uint(*mark)
	qc.IPv6Enabled = false

	start := time.Now()
	conn, transport, err := mtproto.DialObfuscatedDC(&mt, qc, *dc, protoAbridged)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dial DC%d failed: %v\n", *dc, err)
		os.Exit(1)
	}
	defer conn.Close()
	fmt.Printf("dialed DC%d via %s in %v\n", *dc, transport, time.Since(start).Round(time.Millisecond))

	ok, fail := 0, 0
	for i := 1; i <= *rounds; i++ {
		sent := time.Now()
		if _, err := conn.Write(reqPQ()); err != nil {
			fmt.Printf("round %-2d write failed after %v: %v\n", i, time.Since(start).Round(time.Millisecond), err)
			fail++
			break
		}
		_ = conn.SetReadDeadline(time.Now().Add(8 * time.Second))
		pkt, rerr := readPacket(conn)
		if rerr != nil {
			fmt.Printf("round %-2d t+%-8v NO REPLY: %v\n", i, time.Since(start).Round(time.Millisecond), rerr)
			fail++
			break
		}
		tag := "?"
		if len(pkt) >= 24 {
			tag = fmt.Sprintf("0x%08x", binary.LittleEndian.Uint32(pkt[20:24]))
		}
		fmt.Printf("round %-2d t+%-8v rtt=%-8v reply %d B ctor=%s\n",
			i, time.Since(start).Round(time.Millisecond), time.Since(sent).Round(time.Millisecond), len(pkt), tag)
		ok++
		time.Sleep(*interval)
	}
	fmt.Printf("RESULT transport=%s ok=%d fail=%d elapsed=%v\n", transport, ok, fail, time.Since(start).Round(time.Millisecond))
}
