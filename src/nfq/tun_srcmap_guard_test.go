package nfq

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func srcResolverWithConntrack(t *testing.T, wan, contents string) *tunSrcResolver {
	t.Helper()
	path := filepath.Join(t.TempDir(), "nf_conntrack")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	r := newTunSrcResolver(wan)
	r.path = path
	return r
}

func TestResolveRecoversTheLANSource(t *testing.T) {
	r := srcResolverWithConntrack(t, "62.78.37.161",
		"udp      17 29 src=192.168.31.140 dst=1.1.1.1 sport=25954 dport=53 src=1.1.1.1 dst=62.78.37.161 sport=53 dport=25954 mark=0 use=2\n")

	ip, ok := r.resolve(17, net.ParseIP("62.78.37.161"), 25954, net.ParseIP("1.1.1.1"), 53)
	if !ok || ip.String() != "192.168.31.140" {
		t.Fatalf("resolve = %v,%v; want 192.168.31.140,true", ip, ok)
	}
}

func TestResolveRejectsASourceEqualToTheDestination(t *testing.T) {
	r := srcResolverWithConntrack(t, "62.78.37.161",
		"udp      17 29 src=1.1.1.1 dst=1.1.1.1 sport=25954 dport=53 src=1.1.1.1 dst=62.78.37.161 sport=53 dport=25954 mark=0 use=2\n")

	ip, ok := r.resolve(17, net.ParseIP("62.78.37.161"), 25954, net.ParseIP("1.1.1.1"), 53)
	if ok {
		t.Fatalf("resolve returned %v; a conntrack row naming the destination as the source must be ignored", ip)
	}
}

func TestResolveIgnoresTheRoutersOwnFlow(t *testing.T) {
	r := srcResolverWithConntrack(t, "62.78.37.161",
		"udp      17 29 src=62.78.37.161 dst=1.1.1.1 sport=25954 dport=53 [UNREPLIED] src=1.1.1.1 dst=62.78.37.161 sport=53 dport=25954 mark=0 use=2\n")

	if ip, ok := r.resolve(17, net.ParseIP("62.78.37.161"), 25954, net.ParseIP("1.1.1.1"), 53); ok {
		t.Fatalf("resolve returned %v; a flow the router opened itself has no LAN source to recover", ip)
	}
}
