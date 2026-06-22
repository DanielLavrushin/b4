package nfq

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func writeConntrack(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "nf_conntrack")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func newResolverWith(t *testing.T, wan, content string) *tunSrcResolver {
	r := newTunSrcResolver(wan)
	r.path = writeConntrack(t, content)
	return r
}

func TestTunSrcResolveBasic(t *testing.T) {
	line := "ipv4     2 tcp      6 431999 ESTABLISHED src=192.168.1.100 dst=104.18.8.37 sport=49874 dport=443 src=104.18.8.37 dst=94.189.76.227 sport=443 dport=49874 [ASSURED] mark=0 use=1\n"
	r := newResolverWith(t, "94.189.76.227", line)

	got, ok := r.resolve(6, net.ParseIP("94.189.76.227"), 49874, net.ParseIP("104.18.8.37"), 443)
	if !ok || got.String() != "192.168.1.100" {
		t.Fatalf("want 192.168.1.100, got %v ok=%v", got, ok)
	}
}

func TestTunSrcResolvePortRemap(t *testing.T) {
	// SNAT remapped the source port 49874 -> 44092; b4 sees sport=44092.
	line := "ipv4     2 tcp      6 431999 ESTABLISHED src=192.168.1.50 dst=104.18.8.37 sport=49874 dport=443 src=104.18.8.37 dst=94.189.76.227 sport=443 dport=44092 [ASSURED] use=1\n"
	r := newResolverWith(t, "94.189.76.227", line)

	got, ok := r.resolve(6, net.ParseIP("94.189.76.227"), 44092, net.ParseIP("104.18.8.37"), 443)
	if !ok || got.String() != "192.168.1.50" {
		t.Fatalf("want 192.168.1.50, got %v ok=%v", got, ok)
	}
}

func TestTunSrcResolveGateAndMiss(t *testing.T) {
	line := "ipv4 2 tcp 6 100 ESTABLISHED src=192.168.1.100 dst=104.18.8.37 sport=49874 dport=443 src=104.18.8.37 dst=94.189.76.227 sport=443 dport=49874 use=1\n"
	r := newResolverWith(t, "94.189.76.227", line)

	if _, ok := r.resolve(6, net.ParseIP("192.168.1.5"), 49874, net.ParseIP("104.18.8.37"), 443); ok {
		t.Fatal("non-WAN source must not be resolved")
	}
	if _, ok := r.resolve(6, net.ParseIP("94.189.76.227"), 49874, net.ParseIP("8.8.8.8"), 443); ok {
		t.Fatal("unknown flow must not resolve")
	}
}

func TestTunSrcResolveNoWAN(t *testing.T) {
	r := newTunSrcResolver("")
	if _, ok := r.resolve(6, net.ParseIP("94.189.76.227"), 49874, net.ParseIP("104.18.8.37"), 443); ok {
		t.Fatal("empty WAN must disable resolution")
	}
}

func TestTunSrcSetWANClearsCache(t *testing.T) {
	line := "tcp 6 100 ESTABLISHED src=192.168.1.100 dst=104.18.8.37 sport=49874 dport=443 src=104.18.8.37 dst=94.189.76.227 sport=443 dport=49874 use=1\n"
	r := newResolverWith(t, "94.189.76.227", line)
	if _, ok := r.resolve(6, net.ParseIP("94.189.76.227"), 49874, net.ParseIP("104.18.8.37"), 443); !ok {
		t.Fatal("expected resolve before WAN change")
	}
	r.setWAN("203.0.113.9")
	r.mu.Lock()
	n := len(r.cache)
	r.mu.Unlock()
	if n != 0 {
		t.Fatalf("setWAN should clear cache, have %d entries", n)
	}
}
