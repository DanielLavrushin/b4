package socks5

import (
	"net"
	"testing"
)

func tcpAddr(t *testing.T, s string) *net.TCPAddr {
	t.Helper()
	a, err := net.ResolveTCPAddr("tcp", s)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSourceACLEmptyAllowsEverything(t *testing.T) {
	acl, err := buildSourceACL(nil)
	if err != nil {
		t.Fatal(err)
	}
	if acl.active() {
		t.Fatal("an empty list must not be an active restriction")
	}
	if !acl.allows(tcpAddr(t, "203.0.113.9:1234")) {
		t.Error("an inactive ACL must allow every source")
	}
	if !acl.allows(nil) {
		t.Error("an inactive ACL must not care about the address type")
	}
}

func TestSourceACLBlankEntriesAreNotARestriction(t *testing.T) {
	acl, err := buildSourceACL([]string{"", "   "})
	if err != nil {
		t.Fatal(err)
	}
	if acl.active() {
		t.Fatal("whitespace-only entries must not switch the restriction on")
	}
}

func TestSourceACLMatches(t *testing.T) {
	acl, err := buildSourceACL([]string{"192.168.1.0/24", "10.0.0.7", "fd00::/8"})
	if err != nil {
		t.Fatal(err)
	}
	if !acl.active() {
		t.Fatal("a non-empty list must be an active restriction")
	}
	allowed := []string{"192.168.1.1:5000", "192.168.1.254:1", "10.0.0.7:80", "[fd00::1]:9"}
	for _, a := range allowed {
		if !acl.allows(tcpAddr(t, a)) {
			t.Errorf("%s should be allowed", a)
		}
	}
	denied := []string{"192.168.2.1:5000", "10.0.0.8:80", "203.0.113.9:443", "[2001:db8::1]:9"}
	for _, a := range denied {
		if acl.allows(tcpAddr(t, a)) {
			t.Errorf("%s should be denied", a)
		}
	}
}

func TestSourceACLMatchesMappedV4Peer(t *testing.T) {
	acl, err := buildSourceACL([]string{"192.168.1.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	peer := &net.TCPAddr{IP: net.ParseIP("::ffff:192.168.1.20"), Port: 4444}
	if !acl.allows(peer) {
		t.Fatal("a v4-mapped peer on a dual-stack listener must match a plain IPv4 prefix")
	}
}

func TestSourceACLDeniesUnusableAddress(t *testing.T) {
	acl, err := buildSourceACL([]string{"192.168.1.0/24"})
	if err != nil {
		t.Fatal(err)
	}
	if acl.allows(pipeAddr{}) {
		t.Fatal("an address with no IP must be denied while a restriction is active")
	}
}

func TestSourceACLDeniesEverythingWhenUnparseable(t *testing.T) {
	acl, err := buildSourceACL([]string{"192.168.1.0/24", "garbage"})
	if err == nil {
		t.Fatal("a malformed entry must surface an error")
	}
	if !acl.denyAll {
		t.Fatal("an unusable list must fail closed, not open")
	}
	if acl.allows(tcpAddr(t, "192.168.1.5:1080")) {
		t.Error("no source may be allowed while the list is unusable")
	}
}

func TestSourceACLEqual(t *testing.T) {
	a, _ := buildSourceACL([]string{"192.168.1.0/24"})
	b, _ := buildSourceACL([]string{"192.168.1.0/24"})
	c, _ := buildSourceACL([]string{"192.168.2.0/24"})
	if !a.equal(&b) {
		t.Error("identical lists must compare equal")
	}
	if a.equal(&c) {
		t.Error("different lists must not compare equal")
	}
	var nilACL *sourceACL
	if nilACL.equal(&a) {
		t.Error("a nil ACL must not equal a populated one")
	}
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }
