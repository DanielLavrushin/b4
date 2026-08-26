package netif

import (
	"os"
	"path/filepath"
	"testing"
)

func fakeSysfs(t *testing.T) string {
	t.Helper()
	root := t.TempDir() + string(os.PathSeparator)
	prev := Root
	Root = root
	Forget()
	t.Cleanup(func() {
		Root = prev
		Forget()
	})
	return root
}

func mkIface(t *testing.T, root, name string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, body := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOfClassifiesInterfaces(t *testing.T) {
	root := fakeSysfs(t)
	mkIface(t, root, "xray0", map[string]string{"tun_flags": "0x1001\n"})
	mkIface(t, root, "wg0", map[string]string{"uevent": "DEVTYPE=wireguard\nINTERFACE=wg0\n"})
	mkIface(t, root, "eth0", map[string]string{"uevent": "INTERFACE=eth0\n"})

	for _, c := range []struct {
		iface string
		want  Kind
	}{
		{"xray0", KindUserspaceTunnel},
		{"wg0", KindWireGuard},
		{"eth0", KindOther},
		{"nope0", KindMissing},
		{"", KindUnknown},
	} {
		if got := Of(c.iface); got != c.want {
			t.Errorf("Of(%q) = %v, want %v", c.iface, got, c.want)
		}
	}

	if !IsUserspaceTunnel("xray0") {
		t.Error("a device with tun_flags is read by a userspace program and must be reported as one")
	}
	if IsUserspaceTunnel("wg0") {
		t.Error("WireGuard is a kernel device: it never re-dials a connection, so it must not be treated as a userspace tunnel")
	}
	if !IsEncapsulated("wg0") || !IsEncapsulated("xray0") {
		t.Error("both a userspace tunnel and WireGuard carry the packet inside something else")
	}
	if IsEncapsulated("eth0") {
		t.Error("a plain interface puts the packet on the wire as it stands")
	}
}

func TestOfCachesAndForgetClears(t *testing.T) {
	root := fakeSysfs(t)
	mkIface(t, root, "tun9", map[string]string{"tun_flags": "0x1001\n"})

	if got := Of("tun9"); got != KindUserspaceTunnel {
		t.Fatalf("Of = %v", got)
	}

	if err := os.RemoveAll(filepath.Join(root, "tun9")); err != nil {
		t.Fatal(err)
	}
	if got := Of("tun9"); got != KindUserspaceTunnel {
		t.Errorf("a lookup within the TTL must come from the cache, got %v", got)
	}

	Forget()
	if got := Of("tun9"); got != KindMissing {
		t.Errorf("after Forget the interface is read from disk again, got %v", got)
	}
}
