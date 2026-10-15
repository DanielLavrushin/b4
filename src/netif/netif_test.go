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

	if err := os.Remove(filepath.Join(root, "tun9", "tun_flags")); err != nil {
		t.Fatal(err)
	}
	if got := Of("tun9"); got != KindUserspaceTunnel {
		t.Errorf("a lookup within the TTL must come from the cache, got %v", got)
	}

	Forget()
	if got := Of("tun9"); got != KindOther {
		t.Errorf("after Forget the interface is read from disk again, got %v", got)
	}
}

func TestUpIsReadFromFlags(t *testing.T) {
	root := fakeSysfs(t)
	mkIface(t, root, "up0", map[string]string{"tun_flags": "0x1001\n", "flags": "0x1003\n"})
	mkIface(t, root, "down0", map[string]string{"tun_flags": "0x1001\n", "flags": "0x1002\n"})
	mkIface(t, root, "noflags0", map[string]string{"tun_flags": "0x1001\n"})

	if !IsUp("up0") {
		t.Error("IFF_UP is set, so the device can carry the set's default route")
	}
	if IsUp("down0") {
		t.Error("without IFF_UP the kernel refuses the default route, so the set leaks and must keep its bypass")
	}
	if !IsUp("noflags0") {
		t.Error("a kernel that does not publish flags must not be read as down")
	}

	if !EncapsulatedAndUp("up0") {
		t.Error("a live tunnel wraps the packet, so b4 hands it off")
	}
	if EncapsulatedAndUp("down0") {
		t.Error("a down tunnel carries nothing, so the packet still needs its bypass")
	}
	if Of("down0") != KindUserspaceTunnel {
		t.Error("bringing a device down does not change what kind of device it is")
	}
}

func TestVanishedInterfaceKeepsItsKind(t *testing.T) {
	root := fakeSysfs(t)
	mkIface(t, root, "tun7", map[string]string{"tun_flags": "0x1001\n"})

	if got := Of("tun7"); got != KindUserspaceTunnel {
		t.Fatalf("Of = %v", got)
	}

	if err := os.RemoveAll(filepath.Join(root, "tun7")); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	e := cache["tun7"]
	e.at = e.at.Add(-2 * cacheTTL)
	cache["tun7"] = e
	mu.Unlock()

	if got := Of("tun7"); got != KindUserspaceTunnel {
		t.Errorf("a tunnel that dropped out is still a tunnel, got %v; letting it read as missing flips routing decisions and tears the set down", got)
	}
	if IsUp("tun7") {
		t.Error("a device that is gone cannot be up")
	}

	ForgetIface("tun7")
	if got := Of("tun7"); got != KindMissing {
		t.Errorf("ForgetIface drops what b4 remembered, so the next read comes from disk, got %v", got)
	}
}

func TestMarkDownKeepsTheKindAndDropsTheCarrier(t *testing.T) {
	root := fakeSysfs(t)
	mkIface(t, root, "tun5", map[string]string{"tun_flags": "0x1001\n", "flags": "0x1003\n"})

	if !EncapsulatedAndUp("tun5") {
		t.Fatal("a live tunnel wraps the packet")
	}

	MarkDown("tun5")

	if Of("tun5") != KindUserspaceTunnel {
		t.Error("a link-down event does not change what kind of device it is")
	}
	if IsUp("tun5") || EncapsulatedAndUp("tun5") {
		t.Error("the link watcher sees the device go before the cache would expire, and until then b4 would hand off packets to a tunnel that carries nothing")
	}

	MarkDown("never-seen")
	if Of("never-seen") != KindMissing {
		t.Error("marking an interface b4 never classified must not invent a cache entry")
	}
}
