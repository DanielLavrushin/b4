package nfq

import (
	"bufio"
	"bytes"
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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
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

func warmTLSServer(t *testing.T, pid int, cert tls.Certificate, status int, network ...string) <-chan warmTestRead {
	t.Helper()
	var ln net.Listener
	netnsHoldInside(t, pid, func() error {
		var err error
		transport, address := "tcp4", "0.0.0.0:443"
		if len(network) > 0 && network[0] == "tcp6" {
			transport, address = "tcp6", "[::]:443"
		}
		ln, err = net.Listen(transport, address)
		return err
	})
	done := make(chan warmTestRead, 16)
	var wg sync.WaitGroup
	collectMode := len(network) > 1 && network[1] == "collect"
	accepted := 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted++
			stall := collectMode && accepted == 2
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(12 * time.Second))
				raw := &warmRecordConn{Conn: conn}
				if stall {
					header := make([]byte, 5)
					if _, err := io.ReadFull(raw, header); err == nil {
						_, _ = io.CopyN(io.Discard, raw, int64(binary.BigEndian.Uint16(header[3:5])))
						_, _ = io.Copy(io.Discard, raw)
					}
					return
				}
				c := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}})
				defer c.Close()
				req, err := http.ReadRequest(bufio.NewReader(c))
				r := warmTestRead{peer: conn.RemoteAddr().String(), hello: warmFirstRecord(raw.read.Bytes()), err: err}
				if err == nil {
					r.method = req.Method
					if req.Host != warmTestHost {
						r.err = fmt.Errorf("unexpected Host %q", req.Host)
					}
					_, err = fmt.Fprintf(c, "HTTP/1.1 %d test\r\nContent-Length: 0\r\n\r\n", status)
					if err == nil && req.URL.Path == "/keep" {
						req, err = http.ReadRequest(bufio.NewReader(c))
						if err == nil {
							_, err = fmt.Fprintf(c, "HTTP/1.1 %d test\r\nContent-Length: 0\r\n\r\n", status)
						}
					}

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
	proto := binary.NativeEndian.Uint16(binary.BigEndian.AppendUint16(nil, unix.ETH_P_ALL))
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
			n, _, err := unix.Recvfrom(fd, buf, 0)
			if err == nil && n > 0 && (buf[0]>>4 == 4 || buf[0]>>4 == 6) {
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
		warming := false
		var pcap bytes.Buffer
		for _, v := range []uint32{0xa1b2c3d4, 0x00040002, 0, 0, 65535, 101} {
			binary.Write(&pcap, binary.LittleEndian, v)
		}
		for _, received := range packets {
			pkt := received.packet
			if s, ok := reuseWireSegment(pkt); ok {
				if s.flags&0x12 == 2 && (len(openings) == 0 || s.seq != openings[len(openings)-1]) {
					openings = append(openings, s.seq)
					warming = s.window == 1
				}
				if !reuseTCPChecksumValid(pkt) {
					ihl := reuseTCPHeaderAt(pkt)
					payload := pkt[ihl+int(pkt[ihl+12]>>4)*4:]
					probe := bytes.Clone(pkt)
					if pkt[0]>>4 == 6 {
						sock.FixTCPChecksumV6(probe)
					} else {
						sock.FixTCPChecksum(probe)
					}
					corruptTCPChecksum(probe, ihl)
					if !warming || s.ttl >= 30 || len(payload) == 0 || !bytes.Equal(payload, make([]byte, len(payload))) || !bytes.Equal(probe, pkt) {
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
		if dir := os.Getenv("B4_WSREUSE_ARTIFACTS"); dir != "" {
			path := filepath.Join(dir, strings.ReplaceAll(t.Name(), "/", "-")+".pcap")
			if err := os.WriteFile(path, pcap.Bytes(), 0600); err != nil {
				t.Error(err)
			}
		}
		return seen
	}
	t.Cleanup(func() { collect() })
	return collect
}

func wsReuseBinary(t *testing.T, unset string) string {
	t.Helper()
	binary := os.Getenv("B4_WSREUSE_BINARY")
	if binary == "" {
		t.Skip(unset)
	}
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("B4_WSREUSE_BINARY: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		t.Fatalf("B4_WSREUSE_BINARY=%s is not an executable file", binary)
	}
	return binary
}

func TestNetnsWSReuseTransparentBinary(t *testing.T) {
	binary := wsReuseBinary(t, "set B4_WSREUSE_BINARY to the candidate b4 binary")
	for _, family := range []string{"IPv4", "IPv6"} {
		for _, backend := range []string{"iptables", "nftables"} {
			for _, version := range []uint16{tls.VersionTLS12, tls.VersionTLS13} {
				t.Run(fmt.Sprintf("%s/%s/TLS%x", family, backend, version), func(t *testing.T) { warmTransparentBinary(t, binary, backend, family == "IPv6", version) })
			}
		}
	}
}

func warmTransparentBinary(t *testing.T, binary, backend string, ipv6 bool, version uint16, collecting ...bool) {
	collectMode := len(collecting) > 0 && collecting[0]
	t.Helper()
	netnsHoldRequire(t)
	if _, err := exec.LookPath("ethtool"); err != nil {
		t.Skip("ethtool is not installed")
	}
	if backend == "nftables" {
		if _, err := exec.LookPath("nft"); err != nil {
			t.Skip("nft is not installed")
		}
	}
	serverPID := netnsHoldStartPeer(t)
	target, outsideIP, network, prefix := netnsHoldServer, "203.0.113.3", "tcp4", uint32(32)
	clientPID := netnsWSStartLANClient(t)
	netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(serverPID), "-n", "ip", "addr", "add", "203.0.113.3/24", "dev", netnsHoldPeer)
	netnsHoldRun(t, "ethtool", "-K", netnsHoldLink, "tx", "off", "tso", "off", "gso", "off", "gro", "off")
	netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(serverPID), "-n", "ethtool", "-K", netnsHoldPeer, "tx", "off", "tso", "off", "gso", "off", "gro", "off")
	if ipv6 {
		target, outsideIP, network, prefix = "2001:db8::2", "2001:db8::3", "tcp6", 128
		netnsHoldRun(t, "ip", "-6", "addr", "add", "2001:db8::1/64", "dev", netnsHoldLink, "nodad")
		netnsHoldRun(t, "ip", "-6", "addr", "add", "fd00:88::1/64", "dev", netnsWSLANLink, "nodad")
		for _, address := range []string{target, outsideIP} {
			netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(serverPID), "-n", "ip", "-6", "addr", "add", address+"/64", "dev", netnsHoldPeer, "nodad")
		}
		netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(clientPID), "-n", "ip", "-6", "addr", "add", "fd00:88::2/64", "dev", netnsWSLANPeer, "nodad")
		netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(clientPID), "-n", "ip", "-6", "route", "add", "default", "via", "fd00:88::1")
		netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(serverPID), "-n", "ip", "-6", "route", "add", "fd00:88::/64", "via", "2001:db8::1")
		if err := os.WriteFile("/proc/sys/net/ipv6/conf/all/forwarding", []byte("1"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	cert, roots := warmTestCertificate(t)
	serverMode := ""
	if collectMode {
		serverMode = "collect"
	}
	reads := warmTLSServer(t, serverPID, cert, 204, network, serverMode)
	wire := warmWire(t, serverPID)
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	reserved, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	apiPort := reserved.Addr().(*net.TCPAddr).Port
	reserved.Close()
	cfg := config.NewConfig()
	cfg.Version = config.CurrentConfigVersion
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	cfg.Queue.IPv6Enabled = ipv6
	cfg.System.Tables.Engine = backend
	cfg.System.DNS.TCPDisabled = true
	cfg.System.Logging.Directory = ""
	cfg.System.Logging.Level = log.LevelTrace
	cfg.System.WebServer.Port, cfg.System.WebServer.BindAddress = apiPort, "127.0.0.1"
	cfg.System.Geo.GeoSitePath = ""
	cfg.System.Geo.GeoIpPath = filepath.Join(dir, "googlevideo.dat")
	geoTarget := target
	if collectMode {
		geoTarget = "203.0.113.99"
		if ipv6 {
			geoTarget = "2001:db8::99"
		}
	}
	addressBytes := net.ParseIP(geoTarget).To4()
	if ipv6 {
		addressBytes = net.ParseIP(geoTarget).To16()
	}
	geo, err := proto.Marshal(&v2data.GeoIPList{Entry: []*v2data.GeoIP{{CountryCode: "BLOCKED_TLS_RU", Cidr: []*v2data.CIDR{{Ip: addressBytes, Prefix: prefix}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.System.Geo.GeoIpPath, geo, 0600); err != nil {
		t.Fatal(err)
	}
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "reuse-netns", "reuse-netns", true
	set.Targets.GeoIpCategories = []string{"BLOCKED_TLS_RU"}
	set.Targets.UseCollectedIPs = collectMode
	set.Targets.IPPriority = collectMode
	netnsHoldInside(t, clientPID, func() error {
		iface, err := net.InterfaceByName(netnsWSLANPeer)
		if err == nil {
			set.Targets.SourceDevices = []string{iface.HardwareAddr.String()}
		}
		return err
	})
	if collectMode {
		clientAddress := netnsWSLANClient
		if ipv6 {
			clientAddress = "fd00:88::2"
		}
		cfg.Queue.Devices.Devices = []config.Device{{MAC: set.Targets.SourceDevices[0], IP: clientAddress, Name: "Test device", IsManual: true}}
	}
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse.Enabled = true
	exerciseCache := !collectMode && !ipv6 && version == tls.VersionTLS13
	if exerciseCache {
		set.TCP.WSSizeReuse.MaxAgeSeconds = 4
	}
	set.TCP.RSTProtection.Enabled = false
	set.Faking.SNI = false
	set.Fragmentation.Strategy = "combo"
	set.Fragmentation.MiddleSNI = true
	set.Fragmentation.SeqOverlapLength = 1
	set.Fragmentation.SeqOverlapPattern = []string{"0x00"}
	set.Fragmentation.Combo.FirstByteSplit = true
	set.Fragmentation.Combo.ExtensionSplit = false
	set.Fragmentation.Combo.ShuffleMode = "reverse"
	set.Fragmentation.Combo.DecoyEnabled = false
	set.Fragmentation.Combo.FakePerSegment = false
	ordinary := config.NewSetConfig()
	ordinary.Id, ordinary.Name, ordinary.Enabled = "ordinary", "ordinary", true
	ordinary.Targets.SNIDomains = []string{warmTestHost}
	ordinary.Faking.SNI = false
	ordinary.Fragmentation.Strategy = "none"
	if collectMode {
		ordinary.TCP.IPBlockDetect.Enabled = true
		ordinary.TCP.IPBlockDetect.Action = "collect"
		ordinary.TCP.IPBlockDetect.TimeoutMs = 1000
		ordinary.TCP.RSTProtection.Enabled = false
	}
	cfg.Sets = []*config.SetConfig{&set, &ordinary}
	raw, err := json.Marshal(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if destination := os.Getenv("B4_WSREUSE_ARTIFACTS"); destination != "" {
		_ = os.WriteFile(filepath.Join(destination, "fixture-config.json"), raw, 0600)
	}
	path := filepath.Join(dir, "b4.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(dir, "b4.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	cmd := exec.Command(binary, "--config", path)
	cmd.Env = append(os.Environ(), "SSL_CERT_FILE="+ca, "SSL_CERT_DIR="+dir)
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	t.Cleanup(func() {
		cmd.Process.Signal(unix.SIGTERM)
		select {
		case err := <-exited:
			if err != nil {
				t.Errorf("b4 shutdown: %v", err)
			}
		case <-time.After(8 * time.Second):
			cmd.Process.Kill()
			<-exited
			t.Error("b4 did not stop")
		}
		data, _ := os.ReadFile(filepath.Join(dir, "b4.log"))
		if destination := os.Getenv("B4_WSREUSE_ARTIFACTS"); destination != "" {
			_ = os.WriteFile(filepath.Join(destination, strings.ReplaceAll(t.Name(), "/", "-")+".log"), data, 0600)
		}
		if t.Failed() {
			t.Log(string(data))
		}
	})
	url := fmt.Sprintf("http://127.0.0.1:%d/api/version", apiPort)
	deadline := time.Now().Add(8 * time.Second)
	for {
		if response, err := http.Get(url); err == nil {
			response.Body.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("candidate API did not start")
		}
		time.Sleep(25 * time.Millisecond)
	}
	if collectMode {
		dial := func() net.Conn {
			t.Helper()
			var raw net.Conn
			netnsHoldInside(t, clientPID, func() error {
				var e error
				raw, e = net.DialTimeout(network, net.JoinHostPort(target, "443"), 3*time.Second)
				return e
			})
			raw.SetDeadline(time.Now().Add(15 * time.Second))
			return raw
		}
		activeRaw := dial()
		active := tls.Client(activeRaw, &tls.Config{ServerName: warmTestHost, RootCAs: roots, MinVersion: version, MaxVersion: version, CurvePreferences: []tls.CurveID{tls.X25519}})
		defer activeRaw.Close()
		if _, e := fmt.Fprintf(active, "GET /keep HTTP/1.1\r\nHost: %s\r\n\r\n", warmTestHost); e != nil {
			t.Fatal(e)
		}
		first, e := http.ReadResponse(bufio.NewReader(active), nil)
		if e != nil || first.StatusCode != 204 {
			t.Fatalf("preexisting TLS connection: %v", e)
		}
		first.Body.Close()
		stalledRaw := dial()
		stalled := tls.Client(stalledRaw, &tls.Config{ServerName: warmTestHost, RootCAs: roots, MinVersion: version, MaxVersion: version, CurvePreferences: []tls.CurveID{tls.X25519}})
		started := time.Now()
		e = stalled.Handshake()
		stalledRaw.Close()
		if e == nil || !strings.Contains(e.Error(), "reset") || time.Since(started) > 10*time.Second {
			t.Fatalf("collected stalled connection did not receive an exact reset: %v after %s", e, time.Since(started))
		}
		if _, e := fmt.Fprintf(active, "GET /keep HTTP/1.1\r\nHost: %s\r\n\r\n", warmTestHost); e != nil {
			t.Fatalf("active flow hijacked: %v", e)
		}
		second, e := http.ReadResponse(bufio.NewReader(active), nil)
		if e != nil || second.StatusCode != 204 {
			t.Fatalf("active flow changed after collection: %v", e)
		}
		second.Body.Close()
		closeReuseTestTLS(t, active, activeRaw)
		record := nextReuseRecord(t, reads)
		if record.err != nil || record.method != "GET" {
			t.Fatalf("preexisting flow: %+v", record)
		}
	}
	var client net.Conn
	netnsHoldInside(t, clientPID, func() error {
		var err error
		client, err = net.DialTimeout(network, net.JoinHostPort(target, "443"), 3*time.Second)
		return err
	})
	client.SetDeadline(time.Now().Add(20 * time.Second))
	clientWire := &warmRecordConn{Conn: client}
	secure := tls.Client(clientWire, &tls.Config{ServerName: warmTestHost, RootCAs: roots, MinVersion: version, MaxVersion: version, CurvePreferences: []tls.CurveID{tls.X25519}, NextProtos: []string{"http/1.1"}})
	if _, err := fmt.Fprintf(secure, "GET /generate_204 HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", warmTestHost); err != nil {
		client.Close()
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(secure), nil)
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	if response.StatusCode != 204 {
		t.Fatal("client HTTP status", response.StatusCode)
	}
	response.Body.Close()
	closeReuseTestTLS(t, secure, client)
	var warm, pass warmTestRead
	select {
	case warm = <-reads:
	case <-time.After(3 * time.Second):
		t.Fatal("missing warm record")
	}
	select {
	case pass = <-reads:
	case <-time.After(3 * time.Second):
		t.Fatal("missing pass record")
	}
	if warm.err != nil || pass.err != nil || warm.method != "HEAD" || pass.method != "GET" || warm.peer != pass.peer {
		t.Fatalf("warm/pass records: %+v %+v", warm, pass)
	}
	var outside net.Conn
	netnsHoldInside(t, clientPID, func() error {
		var err error
		outside, err = net.DialTimeout(network, net.JoinHostPort(outsideIP, "443"), 3*time.Second)
		return err
	})
	outside.SetDeadline(time.Now().Add(5 * time.Second))
	other := tls.Client(outside, &tls.Config{ServerName: warmTestHost, RootCAs: roots, MinVersion: version, MaxVersion: version, CurvePreferences: []tls.CurveID{tls.X25519}})
	fmt.Fprintf(other, "GET /generate_204 HTTP/1.1\r\nHost: %s\r\n\r\n", warmTestHost)
	outResponse, err := http.ReadResponse(bufio.NewReader(other), nil)
	if err != nil {
		other.Close()
		t.Fatal("unselected flow", err)
	}
	outResponse.Body.Close()
	closeReuseTestTLS(t, other, outside)
	select {
	case record := <-reads:
		if record.method != "GET" {
			t.Fatal("unselected address warmed")
		}
	case <-time.After(time.Second):
		t.Fatal("missing unselected request")
	}
	if !bytes.Equal(pass.hello, warmFirstRecord(clientWire.written.Bytes())) {
		t.Fatal("original ClientHello changed")
	}
	var plain net.Listener
	netnsHoldInside(t, serverPID, func() error {
		var err error
		plain, err = net.Listen(network, net.JoinHostPort(target, "80"))
		return err
	})
	defer plain.Close()
	echoed := make(chan struct{})
	go func() {
		defer close(echoed)
		c, err := plain.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		c.SetDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 4)
		if _, err := io.ReadFull(c, buf); err == nil {
			c.Write(buf)
		}
	}()
	var bypass net.Conn
	netnsHoldInside(t, clientPID, func() error {
		var err error
		bypass, err = net.DialTimeout(network, net.JoinHostPort(target, "80"), 2*time.Second)
		return err
	})
	bypass.SetDeadline(time.Now().Add(3 * time.Second))
	bypass.Write([]byte("port"))
	buf := make([]byte, 4)
	if _, err := io.ReadFull(bypass, buf); err != nil || string(buf) != "port" {
		t.Fatal("unselected port was intercepted", err)
	}
	bypass.Close()
	<-echoed
	time.Sleep(100 * time.Millisecond)
	expectedSYNs := 2
	if exerciseCache {
		request := func() {
			t.Helper()
			time.Sleep(100 * time.Millisecond)
			var raw net.Conn
			netnsHoldInside(t, clientPID, func() error {
				var e error
				raw, e = net.DialTimeout(network, net.JoinHostPort(target, "443"), 3*time.Second)
				return e
			})
			raw.SetDeadline(time.Now().Add(20 * time.Second))
			c := tls.Client(raw, &tls.Config{ServerName: warmTestHost, RootCAs: roots, MinVersion: version, MaxVersion: version, CurvePreferences: []tls.CurveID{tls.X25519}, NextProtos: []string{"http/1.1"}})
			if _, err := fmt.Fprintf(c, "GET /generate_204 HTTP/1.1\r\nHost: %s\r\n\r\n", warmTestHost); err != nil {
				raw.Close()
				t.Fatal(err)
			}
			response, err := http.ReadResponse(bufio.NewReader(c), nil)
			if err != nil {
				raw.Close()
				t.Fatal(err)
			}
			response.Body.Close()
			closeReuseTestTLS(t, c, raw)
		}
		request()
		record := nextReuseRecord(t, reads)
		if record.err != nil || record.method != "GET" || record.peer != warm.peer {
			t.Fatalf("fresh cache: %+v", record)
		}
		time.Sleep(4 * time.Second)
		select {
		case record := <-reads:
			t.Fatalf("background warm after client close: %+v", record)
		default:
		}
		request()
		rewarm, pass := nextReuseRecord(t, reads), nextReuseRecord(t, reads)
		if rewarm.err != nil || pass.err != nil || rewarm.method != "HEAD" || pass.method != "GET" || rewarm.peer != warm.peer || pass.peer != warm.peer {
			t.Fatalf("stale cache: %+v %+v", rewarm, pass)
		}
		expectedSYNs = 5
	}
	time.Sleep(100 * time.Millisecond)
	packets := wire()
	var syns []netnsWireSegment
	for _, packet := range packets {
		if packet.flags&2 != 0 && packet.sport >= 63000 && packet.sport <= 63999 {
			if len(syns) == 0 || packet.seq != syns[len(syns)-1].seq {
				syns = append(syns, packet)
			}
		}
	}
	if len(syns) != expectedSYNs || syns[0].seq == syns[1].seq || syns[0].window != 1 || syns[1].window == 1 {
		t.Fatalf("warm/pass SYNs: %+v", syns)
	}
	if syns[0].scale != 1 || syns[1].scale <= 1 {
		t.Fatalf("warm/pass Window Scale: %d/%d", syns[0].scale, syns[1].scale)
	}
	if syns[0].sport != syns[1].sport {
		t.Fatal("wire tuple changed")
	}
	if exerciseCache {
		if syns[2].window == 1 || syns[3].window != 1 || syns[4].window == 1 || syns[2].sport != syns[0].sport || syns[3].sport != syns[0].sport || syns[4].sport != syns[0].sport {
			t.Fatalf("cached/rewarm SYN sequence: %+v", syns)
		}
	}
	if strings.Contains(string(raw), "wssize-reuse") {
		t.Fatal("internal route leaked into saved config")
	}
	t.Logf("%s: verified client TLS/HTTP204, same tuple %s, new ISN and normal pass window; unselected GeoIP address not warmed", backend, warm.peer)
}

func closeReuseTestTLS(t *testing.T, c *tls.Conn, raw net.Conn) {
	t.Helper()
	defer c.Close()
	if err := c.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if err := raw.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if n, err := io.Copy(io.Discard, c); err != nil || n != 0 {
		t.Fatalf("TLS close: %d %v", n, err)
	}
	if _, err := io.Copy(io.Discard, raw); err != nil {
		t.Fatal(err)
	}
}

func reuseTCPHeaderAt(pkt []byte) int {
	if len(pkt) >= 20 && pkt[0]>>4 == 4 && pkt[9] == 6 {
		return int(pkt[0]&15) * 4
	}
	if len(pkt) >= 40 && pkt[0]>>4 == 6 && pkt[6] == 6 {
		return 40
	}
	return 0
}

func reuseWireSegment(pkt []byte) (netnsWireSegment, bool) {
	at := reuseTCPHeaderAt(pkt)
	if at < 20 || len(pkt) < at+20 {
		return netnsWireSegment{}, false
	}
	tcp := pkt[at:]
	size := int(tcp[12]>>4) * 4
	if size < 20 || size > len(tcp) {
		return netnsWireSegment{}, false
	}
	s := netnsWireSegment{sport: binary.BigEndian.Uint16(tcp[:2]), dport: binary.BigEndian.Uint16(tcp[2:4]), seq: binary.BigEndian.Uint32(tcp[4:8]), window: binary.BigEndian.Uint16(tcp[14:16]), flags: tcp[13], payload: len(tcp) - size, scale: -1, ttl: pkt[8]}
	if pkt[0]>>4 == 6 {
		s.ttl = pkt[7]
	}
	for i := 20; i < size; {
		if tcp[i] == 0 {
			break
		}
		if tcp[i] == 1 {
			i++
			continue
		}
		if i+1 >= size || tcp[i+1] < 2 || i+int(tcp[i+1]) > size {
			break
		}
		if tcp[i] == 3 && tcp[i+1] == 3 {
			s.scale = int(tcp[i+2])
			break
		}
		i += int(tcp[i+1])
	}
	return s, true
}

func reuseTCPChecksumValid(pkt []byte) bool {
	return rfc1071TCPChecksumValid(pkt)
}

func nextReuseRecord(t *testing.T, records <-chan warmTestRead) warmTestRead {
	t.Helper()
	select {
	case record := <-records:
		return record
	case <-time.After(3 * time.Second):
		t.Fatal("missing warm/pass server record")
		return warmTestRead{}
	}
}

func TestNetnsCollectedWSReuseTransparentBinary(t *testing.T) {
	binary := wsReuseBinary(t, "set B4_WSREUSE_BINARY to the collected-IP candidate")
	for _, family := range []string{"IPv4", "IPv6"} {
		for _, backend := range []string{"iptables", "nftables"} {
			t.Run(family+"/"+backend, func(t *testing.T) {
				warmTransparentBinary(t, binary, backend, family == "IPv6", tls.VersionTLS13, true)
			})
		}
	}
}
