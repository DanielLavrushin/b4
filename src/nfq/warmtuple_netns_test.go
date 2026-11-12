package nfq

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/socks5"
	"github.com/daniellavrushin/b4/warmtuple"
	"github.com/urlesistiana/v2dat/v2data"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
)

const warmTestHost = "warm.example.org"

type warmRecordConn struct {
	net.Conn
	read, written bytes.Buffer
}

func (c *warmRecordConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.read.Write(b[:n])
	return n, err
}

func (c *warmRecordConn) Write(b []byte) (int, error) {
	n, err := c.Conn.Write(b)
	c.written.Write(b[:n])
	return n, err
}

func warmTestCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: warmTestHost},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{warmTestHost}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

type warmTestRead struct {
	peer, method string
	hello        []byte
	err          error
}

func warmFirstRecord(b []byte) []byte {
	if len(b) < 5 {
		return nil
	}
	n := 5 + int(binary.BigEndian.Uint16(b[3:5]))
	if n > len(b) || b[0] != 22 {
		return nil
	}
	return append([]byte(nil), b[:n]...)
}

func warmTLSServer(t *testing.T, pid int, cert tls.Certificate, status int) <-chan warmTestRead {
	t.Helper()
	var ln net.Listener
	netnsHoldInside(t, pid, func() error {
		var err error
		ln, err = net.Listen("tcp4", netnsHoldServer+":443")
		return err
	})
	done := make(chan warmTestRead, 16)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(12 * time.Second))
				raw := &warmRecordConn{Conn: conn}
				c := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13, NextProtos: []string{"http/1.1"}})
				defer c.Close()
				req, err := http.ReadRequest(bufio.NewReader(c))
				r := warmTestRead{peer: conn.RemoteAddr().String(), hello: warmFirstRecord(raw.read.Bytes()), err: err}
				if err == nil {
					r.method = req.Method
					if req.Host != warmTestHost {
						r.err = fmt.Errorf("unexpected Host %q", req.Host)
					}
					_, err = fmt.Fprintf(c, "HTTP/1.1 %d test\r\nContent-Length: 0\r\n\r\n", status)
					if err == nil {
						_, err = c.Read(make([]byte, 1))
						if err == io.EOF {
							err = nil
						}
					}
					if r.err == nil {
						r.err = err
					}
				}
				done <- r
			}()
		}
	}()
	t.Cleanup(func() { ln.Close(); wg.Wait() })
	return done
}

func warmWire(t *testing.T, pid int) func() []netnsWireSegment {
	t.Helper()
	proto := binary.NativeEndian.Uint16(binary.BigEndian.AppendUint16(nil, unix.ETH_P_IP))
	fd := -1
	netnsHoldInside(t, pid, func() error {
		iface, err := net.InterfaceByName(netnsHoldPeer)
		if err != nil {
			return err
		}
		fd, err = unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, int(proto))
		if err != nil {
			return err
		}
		if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: proto, Ifindex: iface.Index}); err != nil {
			return err
		}
		return unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Usec: 50000})
	})
	type received struct {
		packet []byte
		at     time.Time
	}
	var packets []received
	overflow := false
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65536)
		for {
			select {
			case <-stop:
				return
			default:
			}
			n, from, err := unix.Recvfrom(fd, buf, 0)
			if ll, ok := from.(*unix.SockaddrLinklayer); ok && ll.Pkttype == unix.PACKET_OUTGOING {
				continue
			}
			if err == nil {
				if len(packets) == 10000 {
					overflow = true
					return
				}
				packets = append(packets, received{append([]byte(nil), buf[:n]...), time.Now()})
			}
		}
	}()
	var once sync.Once
	collect := func() []netnsWireSegment {
		once.Do(func() { close(stop); <-done; unix.Close(fd) })
		if overflow {
			t.Error("packet capture overflow, possible recursive reinjection")
		}
		var seen []netnsWireSegment
		var openings []uint32
		var pcap bytes.Buffer
		for _, v := range []uint32{0xa1b2c3d4, 0x00040002, 0, 0, 65535, 101} {
			binary.Write(&pcap, binary.LittleEndian, v)
		}
		for _, received := range packets {
			pkt := received.packet
			if s, ok := netnsHoldParse(pkt); ok {
				if s.flags&2 != 0 && (len(openings) == 0 || s.seq != openings[len(openings)-1]) {
					openings = append(openings, s.seq)
				}
				if !sock.TCPChecksumValid(pkt) {
					ihl := int(pkt[0]&15) * 4
					payload := pkt[ihl+int(pkt[ihl+12]>>4)*4:]
					probe := bytes.Clone(pkt)
					sock.FixTCPChecksum(probe)
					corruptTCPChecksum(probe, ihl)
					if len(openings) != 1 || pkt[8] >= 30 || len(payload) == 0 || !bytes.Equal(payload, make([]byte, len(payload))) || !bytes.Equal(probe, pkt) {
						t.Error("unexpected bad checksum, outside native Combo's deliberate overlap decoys")
					}
				} else {
					seen = append(seen, s)
				}
			}
			for _, v := range []uint32{uint32(received.at.Unix()), uint32(received.at.Nanosecond() / 1000), uint32(len(pkt)), uint32(len(pkt))} {
				binary.Write(&pcap, binary.LittleEndian, v)
			}
			pcap.Write(pkt)
		}
		if path := os.Getenv("B4_WARMTUPLE_PCAP"); path != "" {
			if err := os.WriteFile(path, pcap.Bytes(), 0600); err != nil {
				t.Error(err)
			}
		}
		return seen
	}
	t.Cleanup(func() { collect() })
	return collect
}

func warmNetnsSetup(t *testing.T) int {
	return warmNetnsSetupWithQueue(t, true)
}

func warmNetnsSetupWithQueue(t *testing.T, startPool bool) int {
	t.Helper()
	netnsHoldRequire(t)
	if _, err := exec.LookPath("ethtool"); err != nil {
		t.Fatal("ethtool is required to check wire checksums without offload")
	}
	pid := netnsHoldStartPeer(t)
	netnsHoldRun(t, "ethtool", "-K", netnsHoldLink, "tx", "off", "rx", "off", "tso", "off", "gso", "off", "gro", "off")
	netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(pid), "-n", "ethtool", "-K", netnsHoldPeer, "tx", "off", "rx", "off", "tso", "off", "gso", "off", "gro", "off")
	cfg := warmtuple.QueueConfig(netip.MustParseAddrPort(netnsHoldServer+":443"), netnsHoldQueue)
	cfg.ConfigPath = t.TempDir() + "/b4.json"
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")
	netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(pid), "-n", "iptables", "-w", "-t", "mangle", "-A", "PREROUTING", "-p", "tcp", "--dport", "443", "-m", "ttl", "--ttl-lt", "30", "-j", "DROP")
	mark := fmt.Sprintf("0x%x/0x%x", cfg.Queue.Mark, cfg.Queue.Mark)
	rule := []string{"-t", "mangle", "OUTPUT", "-o", netnsHoldLink, "-s", netnsHoldRouter, "-d", netnsHoldServer, "-p", "tcp", "--sport", "40000", "--dport", "443", "-m", "mark", "!", "--mark", mark, "-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue)}
	netnsHoldRun(t, append([]string{"iptables", "-w", rule[0], rule[1], "-A"}, rule[2:]...)...)
	t.Cleanup(func() {
		_ = exec.Command("iptables", append([]string{"-w", rule[0], rule[1], "-D"}, rule[2:]...)...).Run()
	})
	if startPool {
		p := NewPool(&cfg)
		t.Cleanup(p.Stop)
		if err := p.Start(); err != nil {
			t.Fatal(err)
		}
	}
	return pid
}

func warmManager(t *testing.T, roots *x509.CertPool) *warmtuple.Manager {
	t.Helper()
	m, err := warmtuple.New(warmtuple.Config{
		Host: warmTestHost, Target: netip.MustParseAddrPort(netnsHoldServer + ":443"), Source: netip.MustParseAddr(netnsHoldRouter),
		FirstPort: 40000, LastPort: 40000, WarmTimeout: 3 * time.Second, ReuseTimeout: 5 * time.Second,
		RetryInterval: 50 * time.Millisecond, ReconnectDelay: 50 * time.Millisecond, PassLifetime: 10 * time.Second, RootCAs: roots,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m
}

func TestNetnsWarmTupleSOCKSOriginalTLSAndSameTuple(t *testing.T) {
	warmTupleExchange(t, "")
}

func TestNetnsWarmTupleB4Command(t *testing.T) {
	binary := os.Getenv("B4_WARMTUPLE_BINARY")
	if binary == "" {
		t.Skip("set B4_WARMTUPLE_BINARY to the built b4 binary")
	}
	warmTupleExchange(t, binary)
}

func TestNetnsWarmTupleIntegratedB4API(t *testing.T) {
	binary := os.Getenv("B4_WARMTUPLE_BINARY")
	if binary == "" {
		t.Skip("set B4_WARMTUPLE_BINARY to the built b4 binary")
	}
	t.Run("domain", func(t *testing.T) { warmTupleExchange(t, "integrated:"+binary) })
	t.Run("geoip", func(t *testing.T) {
		t.Setenv("B4_WARMTUPLE_GEOIP", "1")
		warmTupleExchange(t, "integrated:"+binary)
	})
}

func warmStartCommand(t *testing.T, binary string, cert tls.Certificate, proxyPort int) {
	t.Helper()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "b4.log")
	output, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { output.Close() })
	c := exec.Command(binary, "warm-tuple", "--host", warmTestHost, "--target", netnsHoldServer+":443", "--source", netnsHoldRouter,
		"--first-port", "40000", "--last-port", "40000", "--queue", strconv.Itoa(netnsHoldQueue),
		"--listen", "127.0.0.1:"+strconv.Itoa(proxyPort), "--duration", "30s")
	apiURL := ""
	if strings.HasPrefix(binary, "integrated:") {
		reserved, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		apiPort := reserved.Addr().(*net.TCPAddr).Port
		reserved.Close()
		cfg := warmtuple.QueueConfig(netip.MustParseAddrPort(netnsHoldServer+":443"), netnsHoldQueue)
		cfg.System.Tables.SkipSetup = true
		cfg.System.WebServer.Port = apiPort
		cfg.System.WebServer.BindAddress = "127.0.0.1"
		cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: proxyPort}
		cfg.System.Socks5.WarmTuple = config.Socks5WarmTupleConfig{SourceAddress: netnsHoldRouter, FirstPort: 40000, LastPort: 40000, ReconnectDelayMs: 100,
			Targets: map[string]string{warmTestHost: netnsHoldServer}}
		set := cfg.Sets[0]
		set.TCP.WarmTuple.Enabled = true
		set.Targets = config.TargetsConfig{SNIDomains: []string{"example.org"}}
		if os.Getenv("B4_WARMTUPLE_GEOIP") == "1" {
			geo, err := proto.Marshal(&v2data.GeoIPList{Entry: []*v2data.GeoIP{{CountryCode: "BLOCKED_TLS_RU",
				Cidr: []*v2data.CIDR{{Ip: net.ParseIP(netnsHoldServer).To4(), Prefix: 32}}}}})
			if err != nil {
				t.Fatal(err)
			}
			cfg.System.Geo.GeoIpPath = filepath.Join(dir, "googlevideo.dat")
			if err := os.WriteFile(cfg.System.Geo.GeoIpPath, geo, 0600); err != nil {
				t.Fatal(err)
			}
			set.Targets = config.TargetsConfig{GeoIpCategories: []string{"BLOCKED_TLS_RU"}, IPPriority: true}
			ordinary := config.NewSetConfig()
			ordinary.Id, ordinary.Name = "ordinary", "Ordinary fallback"
			ordinary.Targets.SNIDomains = []string{"example.org"}
			cfg.Sets = append(cfg.Sets, &ordinary)
		}
		data, err := json.Marshal(&cfg)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "b4.json")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		c = exec.Command(strings.TrimPrefix(binary, "integrated:"), "--config", path)
		apiURL = fmt.Sprintf("http://127.0.0.1:%d", apiPort)
	}
	c.Env = append(os.Environ(), "SSL_CERT_FILE="+ca, "SSL_CERT_DIR="+dir)
	c.Stdout, c.Stderr = output, output
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var exitErr error
	go func() { exitErr = c.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = c.Process.Signal(unix.SIGTERM)
		select {
		case <-done:
			if exitErr != nil {
				log, _ := os.ReadFile(logPath)
				t.Errorf("native b4 command: %v: %s", exitErr, log)
			}
		case <-time.After(5 * time.Second):
			c.Process.Kill()
			<-done
			t.Error("native b4 command did not shut down on SIGTERM")
		}
	})
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		log, _ := os.ReadFile(logPath)
		if apiURL != "" {
			client := http.Client{Timeout: 200 * time.Millisecond}
			if resp, err := client.Get(apiURL + "/api/config"); err == nil {
				var cfg map[string]any
				err = json.NewDecoder(resp.Body).Decode(&cfg)
				resp.Body.Close()
				if err == nil && resp.StatusCode == 200 {
					cfg["system"].(map[string]any)["socks5"].(map[string]any)["warm_tuple"].(map[string]any)["compact_hello"] = true
					body, _ := json.Marshal(cfg)
					req, _ := http.NewRequest(http.MethodPut, apiURL+"/api/config", bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					resp, err := client.Do(req)
					if err != nil {
						t.Fatal("API apply:", err)
					}
					applied, _ := io.ReadAll(resp.Body)
					resp.Body.Close()
					if resp.StatusCode != 200 {
						t.Fatalf("API apply %d: %s", resp.StatusCode, applied)
					}
					return
				}
			}
		}
		if bytes.Contains(log, []byte(`"event":"ready"`)) {
			return
		}
		select {
		case <-done:
			t.Fatalf("native command exited before ready: %v: %s", exitErr, log)
		case <-time.After(10 * time.Millisecond):
		}
	}
	log, _ := os.ReadFile(logPath)
	t.Fatalf("native command did not start: %s", log)
}

func warmTupleExchange(t *testing.T, executable string) {
	t.Helper()
	pid := warmNetnsSetupWithQueue(t, executable == "")
	cert, roots := warmTestCertificate(t)
	reads := warmTLSServer(t, pid, cert, 204)
	wire := warmWire(t, pid)
	var m *warmtuple.Manager
	cfg := config.NewConfig()
	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	proxyPort := reserved.Addr().(*net.TCPAddr).Port
	reserved.Close()
	cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: proxyPort}
	if executable == "" {
		m = warmManager(t, roots)
		s := socks5.NewServerWithConnector(&cfg, m.DialContext)
		if err := s.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Stop() })
	} else {
		warmStartCommand(t, executable, cert, proxyPort)
	}
	clientCfg := socks5.ClientConfig{Host: "127.0.0.1", Port: proxyPort, Timeout: 9 * time.Second}
	if c, err := socks5.DialUpstream(context.Background(), clientCfg, "127.0.0.1", 1); err == nil {
		c.Close()
		t.Fatal("unselected request connected")
	}
	var sawTimeWait atomic.Bool
	procSource := fmt.Sprintf("%08X:%04X", binary.NativeEndian.Uint32(net.ParseIP(netnsHoldRouter).To4()), 40000)
	stopObserve, observed := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(observed)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopObserve:
				return
			case <-ticker.C:
				state, _ := os.ReadFile("/proc/net/tcp")
				for _, line := range bytes.Split(state, []byte{'\n'}) {
					fields := bytes.Fields(line)
					if len(fields) >= 4 && string(fields[1]) == procSource && bytes.Equal(fields[3], []byte("06")) {
						sawTimeWait.Store(true)
					}
				}
			}
		}
	}()
	raw, err := socks5.DialUpstream(context.Background(), clientCfg, warmTestHost, 443)
	close(stopObserve)
	<-observed
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if !sawTimeWait.Load() {
		t.Fatal("warm TIME_WAIT was not observed before pass, so socket reuse was not proved")
	}
	original := &warmRecordConn{Conn: raw}
	c := tls.Client(original, &tls.Config{ServerName: warmTestHost, RootCAs: roots, MinVersion: tls.VersionTLS13, NextProtos: []string{"http/1.1"}})
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if err := c.HandshakeContext(context.Background()); err != nil {
		t.Fatal("application TLS:", err)
	}
	fmt.Fprintf(c, "GET /application HTTP/1.1\r\nHost: %s\r\n\r\n", warmTestHost)
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		t.Fatal(resp.Status)
	}
	c.Close()
	raw.Close()
	var exchanges []warmTestRead
	for range 2 {
		select {
		case r := <-reads:
			if r.err != nil {
				t.Fatal(r.err)
			}
			exchanges = append(exchanges, r)
		case <-time.After(5 * time.Second):
			t.Fatal("server did not finish both TLS sessions")
		}
	}
	if exchanges[0].peer != netnsHoldRouter+":40000" || exchanges[0].peer != exchanges[1].peer || exchanges[0].method != "HEAD" || exchanges[1].method != "GET" {
		t.Fatalf("warm and pass did not use the same tuple: %+v", exchanges)
	}
	if !bytes.Equal(exchanges[1].hello, warmFirstRecord(original.written.Bytes())) || bytes.Equal(exchanges[0].hello, exchanges[1].hello) {
		t.Fatal("application ClientHello was replaced or warm reused it")
	}
	if strings.HasPrefix(executable, "integrated:") && len(exchanges[0].hello) > 600 {
		t.Fatalf("compact warm Hello too large: %d", len(exchanges[0].hello))
	}
	deadline := time.Now().Add(time.Second)
	active := func() bool {
		if m != nil {
			return m.Active() != 0
		}
		state, err := os.ReadFile("/proc/net/tcp")
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range bytes.Split(state, []byte{'\n'}) {
			f := bytes.Fields(line)
			if len(f) >= 4 && string(f[1]) == procSource && bytes.Equal(f[3], []byte("01")) {
				return true
			}
		}
		return false
	}
	for active() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if active() {
		t.Fatal("relay closure retained its lease or established socket")
	}
	time.Sleep(80 * time.Millisecond)
	var syns []netnsWireSegment
	var firstSeq uint32
	tinyWarm, ordinaryData := false, false
	for _, p := range wire() {
		if p.dport != 443 {
			continue
		}
		if p.sport != 40000 {
			t.Fatal("a connection escaped the fixed source port")
		}
		if p.flags&2 != 0 && (len(syns) == 0 || p.seq != syns[len(syns)-1].seq) {
			syns = append(syns, p)
			if len(syns) == 1 {
				firstSeq = p.seq + 1
			}
		}
		if len(syns) == 1 && p.flags&4 != 0 {
			t.Fatal("warm ended with RST instead of graceful TIME_WAIT")
		}
		if len(syns) == 1 && p.seq == firstSeq-1 && p.payload == 2 && p.window == 1 {
			tinyWarm = true
		}
		if len(syns) == 2 && p.payload > 0 {
			ordinaryData = true
			if p.window == 1 || p.payload <= 2 {
				t.Fatal("pass data still carries warm manipulation")
			}
		}
	}
	if len(syns) != 2 || syns[0].seq == syns[1].seq || syns[0].window != 1 || syns[0].scale != 1 || syns[1].window == 1 || syns[1].scale <= 1 || !tinyWarm || !ordinaryData {
		t.Fatalf("two TCP openings and warm-only manipulation not proved: SYNs=%+v tiny=%v pass=%v", syns, tinyWarm, ordinaryData)
	}
	t.Logf("two verified TLS/204 sessions on %s:40000 -> %s:443, original application Hello intact, independent ISNs/WS; standalone SOCKS5", netnsHoldRouter, netnsHoldServer)
}

func TestNetnsWarmTupleFailsClosedOnCertificateOrHTTP(t *testing.T) {
	for _, trust := range []bool{false, true} {
		t.Run(fmt.Sprintf("trust=%v", trust), func(t *testing.T) {
			pid := warmNetnsSetup(t)
			cert, roots := warmTestCertificate(t)
			warmTLSServer(t, pid, cert, 503)
			if !trust {
				roots = x509.NewCertPool()
			}
			m := warmManager(t, roots)
			if c, err := m.DialContext(context.Background(), warmTestHost+":443"); err == nil {
				c.Close()
				t.Fatal("failed warm opened pass")
			} else {
				t.Log(err)
			}
			if m.Active() != 0 {
				t.Fatal("failed warm leaked source port")
			}
		})
	}
}
