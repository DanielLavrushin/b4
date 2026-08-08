package nfq

import (
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func buildDNSQuery(domain string, txid uint16) []byte {
	var q []byte
	for _, lab := range strings.Split(domain, ".") {
		q = append(q, byte(len(lab)))
		q = append(q, []byte(lab)...)
	}
	q = append(q, 0x00, 0x00, 0x01, 0x00, 0x01)

	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], txid)
	binary.BigEndian.PutUint16(hdr[2:4], 0x0100)
	binary.BigEndian.PutUint16(hdr[4:6], 1)
	return append(hdr, q...)
}

func newBlockingDNSTCPServer(t *testing.T) *dnsTCPServer {
	t.Helper()

	set := &config.SetConfig{
		Id:      "test-block",
		Name:    "testblock",
		Enabled: true,
	}
	set.Targets.DomainsToMatch = []string{"blocked.test"}
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock

	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{set}
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = true

	w := NewWorkerWithQueue(&cfg, 0)
	w.matcher.Store(sni.NewSuffixSet(cfg.Sets))
	w.ipToMac.Store(make(map[string]string))

	var srv *dnsTCPServer
	for port := 45353; port < 45383; port++ {
		srv = newDNSTCPServer(w, port)
		if err := srv.Start(); err == nil {
			break
		}
		srv = nil
	}
	if srv == nil {
		t.Skip("no free port for dns tcp listener")
	}
	t.Cleanup(srv.Stop)
	return srv
}

func queryOverTCP(t *testing.T, addr string, domain string) []byte {
	t.Helper()

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()

	if err := writeDNSTCPMessage(conn, buildDNSQuery(domain, 0x1234), 5*time.Second); err != nil {
		t.Fatalf("write query to %s: %v", addr, err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := readDNSTCPMessage(conn)
	if err != nil {
		t.Fatalf("read response from %s: %v", addr, err)
	}
	return resp
}

func assertNXDomain(t *testing.T, resp []byte, addr string) {
	t.Helper()
	if len(resp) < 12 {
		t.Fatalf("%s: response too short: %d bytes", addr, len(resp))
	}
	if got := binary.BigEndian.Uint16(resp[0:2]); got != 0x1234 {
		t.Fatalf("%s: transaction id mismatch: got %#x", addr, got)
	}
	if rcode := resp[3] & 0x0F; rcode != 3 {
		t.Fatalf("%s: expected NXDOMAIN (rcode 3), got rcode %d", addr, rcode)
	}
}

func newDNSTCPServerWithFamilies(t *testing.T, v4, v6 bool) (*dnsTCPServer, error) {
	t.Helper()

	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = v4
	cfg.Queue.IPv6Enabled = v6

	w := NewWorkerWithQueue(&cfg, 0)
	w.matcher.Store(sni.NewSuffixSet(cfg.Sets))
	w.ipToMac.Store(make(map[string]string))

	srv := newDNSTCPServer(w, 45400)
	err := srv.Start()
	if err == nil {
		t.Cleanup(srv.Stop)
	}
	return srv, err
}

func TestDNSTCPServerSkipsDisabledFamilies(t *testing.T) {
	t.Run("v4 only", func(t *testing.T) {
		srv, err := newDNSTCPServerWithFamilies(t, true, false)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		if !srv.ReadyV4() {
			t.Error("expected v4 listener")
		}
		if srv.ReadyV6() {
			t.Error("v6 listener bound while IPv6 is disabled")
		}
	})

	t.Run("v6 only", func(t *testing.T) {
		srv, err := newDNSTCPServerWithFamilies(t, false, true)
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		if srv.ReadyV4() {
			t.Error("v4 listener bound while IPv4 is disabled")
		}
		if !srv.ReadyV6() {
			t.Error("expected v6 listener")
		}
	})

	t.Run("both disabled", func(t *testing.T) {
		if _, err := newDNSTCPServerWithFamilies(t, false, false); err == nil {
			t.Error("expected an error when both families are disabled")
		}
	})
}

func TestDNSTCPServerBindsBothFamilies(t *testing.T) {
	srv := newBlockingDNSTCPServer(t)
	if !srv.ReadyV4() {
		t.Error("expected IPv4 listener to be ready")
	}
	if !srv.ReadyV6() {
		t.Error("expected IPv6 listener to be ready")
	}
}

func TestDNSTCPServerBlocksOverIPv4(t *testing.T) {
	srv := newBlockingDNSTCPServer(t)
	if !srv.ReadyV4() {
		t.Skip("no IPv4 listener")
	}
	addr := net.JoinHostPort("127.0.0.1", itoa(srv.port))
	assertNXDomain(t, queryOverTCP(t, addr, "blocked.test"), addr)
}

func TestDNSTCPServerBlocksOverIPv6(t *testing.T) {
	srv := newBlockingDNSTCPServer(t)
	if !srv.ReadyV6() {
		t.Skip("no IPv6 listener on this host")
	}
	addr := net.JoinHostPort("::1", itoa(srv.port))
	assertNXDomain(t, queryOverTCP(t, addr, "blocked.test"), addr)
}

func TestDNSTCPServerHandlesMultipleQueriesOnOneConnection(t *testing.T) {
	srv := newBlockingDNSTCPServer(t)
	addr := net.JoinHostPort("127.0.0.1", itoa(srv.port))

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for i := 0; i < 3; i++ {
		if err := writeDNSTCPMessage(conn, buildDNSQuery("blocked.test", uint16(0x1234)), 5*time.Second); err != nil {
			t.Fatalf("write query %d: %v", i, err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		resp, err := readDNSTCPMessage(conn)
		if err != nil {
			t.Fatalf("read response %d: %v", i, err)
		}
		assertNXDomain(t, resp, addr)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	return string(b[i:])
}

func TestDNSTCPServerStopIsPromptWithIdleConnection(t *testing.T) {
	srv := newBlockingDNSTCPServer(t)
	addr := net.JoinHostPort("127.0.0.1", itoa(srv.port))

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if err := writeDNSTCPMessage(conn, buildDNSQuery("blocked.test", 0x1234), 5*time.Second); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := readDNSTCPMessage(conn); err != nil {
		t.Fatalf("read: %v", err)
	}

	done := make(chan time.Duration, 1)
	go func() {
		start := time.Now()
		srv.Stop()
		done <- time.Since(start)
	}()

	select {
	case took := <-done:
		if took > 3*time.Second {
			t.Errorf("Stop took %v with an idle connection open; it must not wait out the idle timeout", took)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Stop blocked on an idle connection")
	}
}

func TestDNSTCPServerStopRaceWithAccepts(t *testing.T) {
	for round := 0; round < 20; round++ {
		srv := newBlockingDNSTCPServer(t)
		addr := net.JoinHostPort("127.0.0.1", itoa(srv.port))

		var dialers sync.WaitGroup
		for i := 0; i < 8; i++ {
			dialers.Add(1)
			go func() {
				defer dialers.Done()
				c, err := net.DialTimeout("tcp", addr, time.Second)
				if err == nil {
					_ = writeDNSTCPMessage(c, buildDNSQuery("blocked.test", 0x1234), time.Second)
					_ = c.Close()
				}
			}()
		}
		srv.Stop()
		dialers.Wait()
	}
}
