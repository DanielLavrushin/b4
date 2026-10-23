package metrics

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"
)

var (
	phone  = Device{MAC: "AA:BB:CC:00:00:01", Addr: netip.MustParseAddr("192.168.1.23")}
	laptop = Device{MAC: "AA:BB:CC:00:00:02", Addr: netip.MustParseAddr("192.168.1.24")}
	router = Device{Addr: netip.MustParseAddr("192.168.1.1")}
)

func deviceEntry(t testing.TB, fr Frame, key string) DeviceEntry {
	t.Helper()
	if fr.Devices == nil {
		t.Fatal("frame has no devices")
	}
	for _, d := range fr.Devices.Items {
		if d.Key == key {
			return d
		}
	}
	t.Fatalf("%q missing from devices: %+v", key, fr.Devices.Items)
	return DeviceEntry{}
}

func TestDevicesCountEachFlowOnce(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m

	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "", phone)
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "video", "video.example", phone)
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "video", "", phone)
	m.ObserveFlowTo(flowKey(2), netip.Addr{}, "", "", router)
	m.ObserveFlowTo(flowKey(3), netip.Addr{}, "", "", Device{})
	r.step(5 * time.Second)
	m.ObserveFlowTo(flowKey(4), netip.Addr{}, "", "", phone)

	fr := m.Hello()
	if d := deviceEntry(t, fr, phone.MAC); d.Count != 2 || d.Last != ms(rigStart.Add(5*time.Second)) || !slices.Equal(d.Domains, []string{"video.example"}) {
		t.Fatalf("a device counts each of its flows once, not each packet, and keeps the time of the latest: %+v", d)
	}
	if d := deviceEntry(t, fr, "192.168.1.1"); d.Count != 1 || d.Domains != nil {
		t.Fatalf("a source without a MAC is listed by its address: %+v", d)
	}
	if n := len(fr.Devices.Items); n != 2 || fr.Totals.Conns != 4 {
		t.Fatalf("a flow without a source is counted but belongs to no device: %d devices, %+v", n, fr.Totals)
	}
}

func TestDevicesListTheirMostConnectedDomains(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	n := 0
	flows := func(host string, times int) {
		for i := 0; i < times; i++ {
			n++
			m.ObserveFlowTo(flowKey(n), netip.Addr{}, "", host, phone)
		}
	}
	flows("video.example", 3)
	flows("chat.example", 2)
	flows("news.example", 1)
	flows("ads.example", 1)
	n++
	m.ObserveFlowTo(flowKey(n), netip.Addr{}, "", "", phone)
	m.ObserveFlowTo(flowKey(n), netip.Addr{}, "", "Late.Example.", phone)
	m.ObserveFlowTo(flowKey(n), netip.Addr{}, "", "late.example", phone)

	d := deviceEntry(t, m.Hello(), phone.MAC)
	if d.Count != 8 || !slices.Equal(d.Domains, []string{"video.example", "chat.example", "late.example"}) {
		t.Fatalf("the names with the most connections, the most recent first among equals; a flow named late is named once: %+v", d)
	}
}

func TestDevicesFoldTheirAddressIntoTheMACOnceItIsKnown(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	early := Device{Addr: phone.Addr}

	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "video.example", early)
	m.ObserveFlowTo(flowKey(2), netip.Addr{}, "", "", early)
	m.ObserveFlowTo(flowKey(3), netip.Addr{}, "", "chat.example", phone)

	fr := m.Hello()
	if d := deviceEntry(t, fr, phone.MAC); len(fr.Devices.Items) != 1 || d.Count != 3 || !slices.Equal(d.Domains, []string{"chat.example", "video.example"}) {
		t.Fatalf("flows counted under the address before the MAC table knew it move to the device: %+v", fr.Devices.Items)
	}

	m.ObserveFlowTo(flowKey(4), netip.Addr{}, "", "news.example", early)
	m.ObserveFlowTo(flowKey(5), netip.Addr{}, "", "video.example", phone)
	fr = m.Hello()
	if d := deviceEntry(t, fr, phone.MAC); len(fr.Devices.Items) != 1 || d.Count != 5 || !slices.Equal(d.Domains, []string{"video.example", "news.example", "chat.example"}) {
		t.Fatalf("a later gap in the MAC table folds into the known device too: %+v", fr.Devices.Items)
	}
}

func TestSlowTickFoldsAddressesWhoseMACTheTableLearned(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	known := map[string]string{}
	SetMACProvider(func(ip string) string { return known[ip] })
	t.Cleanup(func() { SetMACProvider(nil) })

	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "video.example", Device{Addr: phone.Addr})
	m.ObserveFlowTo(flowKey(2), netip.Addr{}, "", "", laptop)
	m.ObserveFlowTo(flowKey(3), netip.Addr{}, "", "chat.example", Device{Addr: laptop.Addr})
	m.ObserveFlowTo(flowKey(4), netip.Addr{}, "", "", router)
	r.step(10 * time.Second)
	if n := len(m.Hello().Devices.Items); n != 4 {
		t.Fatalf("addresses the MAC table does not know stay listed by address, got %d devices", n)
	}

	known[phone.Addr.String()] = phone.MAC
	known[laptop.Addr.String()] = laptop.MAC
	r.step(time.Second)
	if n := len(m.Hello().Devices.Items); n != 4 {
		t.Fatalf("the lookup runs on the slow tick only, got %d devices", n)
	}
	r.step(10 * time.Second)

	fr := m.Hello()
	if d := deviceEntry(t, fr, phone.MAC); d.Count != 1 || !slices.Equal(d.Domains, []string{"video.example"}) {
		t.Fatalf("an address row whose device went quiet before its MAC was known turns into the device: %+v", d)
	}
	if d := deviceEntry(t, fr, laptop.MAC); d.Count != 2 || !slices.Equal(d.Domains, []string{"chat.example"}) {
		t.Fatalf("an address row joins the device already listed by MAC: %+v", d)
	}
	if d := deviceEntry(t, fr, router.Addr.String()); len(fr.Devices.Items) != 3 || d.Count != 1 {
		t.Fatalf("the router has no MAC and keeps its address row: %+v", fr.Devices.Items)
	}
}

func TestANameWithTheMACReachesTheRowCountedByAddress(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m

	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "", Device{Addr: phone.Addr})
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "video.example", phone)

	fr := m.Hello()
	if d := deviceEntry(t, fr, phone.MAC); len(fr.Devices.Items) != 1 || d.Count != 1 || !slices.Equal(d.Domains, []string{"video.example"}) {
		t.Fatalf("the MAC table learned the phone between a flow's first packet and its name: %+v", fr.Devices.Items)
	}
}

func TestANameByAddressReachesTheRowMovedToItsMAC(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	known := map[string]string{}
	SetMACProvider(func(ip string) string { return known[ip] })
	t.Cleanup(func() { SetMACProvider(nil) })

	proxied := Device{Addr: laptop.Addr}
	m.CountConnection("work", proxied)
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "", proxied)
	known[laptop.Addr.String()] = laptop.MAC
	r.step(10 * time.Second)
	m.RecordDomain("mail.example", "work", proxied)
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "chat.example", proxied)

	fr := m.Hello()
	if d := deviceEntry(t, fr, laptop.MAC); len(fr.Devices.Items) != 1 || d.Count != 2 || !slices.Equal(d.Domains, []string{"chat.example", "mail.example"}) {
		t.Fatalf("the slow tick moved the laptop's row to its MAC while its connections still named it by address: %+v", fr.Devices.Items)
	}

	known["192.168.1.201"] = "AA:BB:CC:00:00:C9"
	m.RecordDomain("ads.example", "", Device{Addr: netip.MustParseAddr("192.168.1.200")})
	m.RecordDomain("ads.example", "", Device{Addr: netip.MustParseAddr("192.168.1.201")})
	m.RecordDomain("ads.example", "", Device{MAC: "AA:BB:CC:00:00:CA", Addr: netip.MustParseAddr("192.168.1.202")})
	if n := len(m.Hello().Devices.Items); n != 1 {
		t.Fatalf("a name never lists a device with no counted connection: %d devices", n)
	}
}

func TestDevicesKeepTheirBusiestDomainsInBoundedSlots(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	n := 0
	flow := func(host string) {
		n++
		m.ObserveFlowTo(flowKey(n), netip.Addr{}, "", host, phone)
	}
	flow("busy.example")
	flow("busy.example")
	for i := 0; i < 3*DeviceDomainsKept; i++ {
		flow(fmt.Sprintf("once%02d.example", i))
	}
	flow("busy.example")

	d := deviceEntry(t, m.Hello(), phone.MAC)
	last := 3*DeviceDomainsKept - 1
	want := []string{"busy.example", fmt.Sprintf("once%02d.example", last), fmt.Sprintf("once%02d.example", last-1)}
	if d.Count != uint64(n) || !slices.Equal(d.Domains, want) {
		t.Fatalf("one-off names take turns in the lowest slot, the busy name stays on top: %+v", d)
	}
	m.dev.mu.Lock()
	filled := 0
	for _, slot := range m.dev.items[deviceKey{mac: phone.MAC}].domains {
		if slot.name != "" {
			filled++
		}
	}
	m.dev.mu.Unlock()
	if filled != DeviceDomainsKept {
		t.Fatalf("a device keeps %d names, kept %d", DeviceDomainsKept, filled)
	}
}

func TestDevicesEvictTheLowestCountSeenLongestAgo(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	busy := Device{MAC: "AA:BB:CC:00:00:FF"}
	n := 0
	flowFrom := func(d Device) {
		n++
		m.ObserveFlowTo(flowKey(n), netip.Addr{}, "", "", d)
	}
	flowFrom(busy)
	flowFrom(busy)
	for i := 1; i < DevicesKept; i++ {
		flowFrom(Device{Addr: netip.AddrFrom4([4]byte{192, 168, 2, byte(i)})})
	}
	flowFrom(busy)
	fresh := Device{Addr: netip.MustParseAddr("192.168.3.1")}
	flowFrom(fresh)

	m.dev.mu.Lock()
	kept := len(m.dev.items)
	_, hasBusy := m.dev.items[deviceKey{mac: busy.MAC}]
	_, oldest := m.dev.items[deviceKey{addr: netip.MustParseAddr("192.168.2.1")}]
	_, next := m.dev.items[deviceKey{addr: netip.MustParseAddr("192.168.2.2")}]
	_, hasFresh := m.dev.items[deviceKey{addr: fresh.Addr}]
	m.dev.mu.Unlock()
	if kept != DevicesKept || !hasBusy || oldest || !next || !hasFresh {
		t.Fatalf("kept=%d busy=%v 192.168.2.1=%v 192.168.2.2=%v fresh=%v", kept, hasBusy, oldest, next, hasFresh)
	}
}

func TestDevicesSendTheMostConnectedFirst(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	n := 0
	for i := 0; i < DevicesSent+5; i++ {
		d := Device{Addr: netip.AddrFrom4([4]byte{192, 168, 2, byte(i + 1)})}
		for c := 0; c <= i%4; c++ {
			n++
			m.ObserveFlowTo(flowKey(n), netip.Addr{}, "", "", d)
		}
	}

	items := m.Hello().Devices.Items
	if len(items) != DevicesSent {
		t.Fatalf("sent %d devices, want %d", len(items), DevicesSent)
	}
	if items[0].Key != "192.168.2.52" || items[0].Count != 4 || items[1].Key != "192.168.2.48" {
		t.Fatalf("highest count first, the most recent first among equals: %+v %+v", items[0], items[1])
	}
	for i := 1; i < len(items); i++ {
		if items[i].Count > items[i-1].Count {
			t.Fatalf("not sorted by count at %d: %+v", i, items)
		}
	}
}

func TestBlockedFlowsCountForTheirDevice(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m

	m.RecordBlockedFlow(flowKey(1), "ads", "ads.example", phone)
	m.RecordBlockedFlow(flowKey(1), "ads", "ads.example", phone)
	m.ObserveFlowTo(flowKey(2), netip.Addr{}, "", "", phone)
	m.RecordBlockedFlow(flowKey(2), "ads", "ads.example", phone)
	m.RecordBlockedFlow(flowKey(3), "ads", "ads.example", router)

	fr := m.Hello()
	if d := deviceEntry(t, fr, phone.MAC); d.Count != 2 {
		t.Fatalf("a blocked connection is one of the device's connections, counted once: %+v", d)
	}
	if d := deviceEntry(t, fr, "192.168.1.1"); d.Count != 1 {
		t.Fatalf("a blocked connection from a source without a MAC counts for its address: %+v", d)
	}
	if devs := fr.Blocked.Devices; len(devs) != 1 || devs[0].Key != phone.MAC || devs[0].Count != 2 {
		t.Fatalf("the Blocked panel still lists devices by MAC only: %+v", devs)
	}
}

func TestProxiedConnectionsCountForTheirDevice(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m

	m.CountConnection("work", laptop)
	m.RecordDomain("mail.example", "work", laptop)
	m.CountConnection("work", laptop)
	m.RecordDomain("mail.example", "work", phone)

	fr := m.Hello()
	if d := deviceEntry(t, fr, laptop.MAC); d.Count != 2 || !slices.Equal(d.Domains, []string{"mail.example"}) {
		t.Fatalf("a proxied connection counts for the client that opened it: %+v", d)
	}
	if len(fr.Devices.Items) != 1 {
		t.Fatalf("a name for a device with no counted connection lists no device: %+v", fr.Devices.Items)
	}
	if d := topDomain(t, fr, "mail.example"); d.Count != 2 {
		t.Fatalf("Top domains counts every proxied name: %+v", d)
	}
}

func TestDevicesResetWithTheCounters(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "", phone)
	before := m.Hello().Devices

	m.ResetCounters()
	after := m.Hello().Devices
	if len(after.Items) != 0 || after.Rev <= before.Rev {
		t.Fatalf("cleared with a new rev: %+v (was %+v)", after, before)
	}

	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "late.example", phone)
	if items := m.Hello().Devices.Items; len(items) != 0 {
		t.Fatalf("a flow counted before the reset neither counts nor names its device after it: %+v", items)
	}
}

func TestTickCarriesDevicesOnlyWhenTheyMoved(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	var sent SentRevs

	if fr := m.Tick(&sent); fr.Devices != nil {
		t.Fatal("no device counted since rev 0")
	}
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "", phone)
	fr := m.Tick(&sent)
	if fr.Devices == nil || sent.Devices != fr.Devices.Rev {
		t.Fatalf("a new connection moves the list: %+v %+v", fr.Devices, sent)
	}
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "", phone)
	r.step(time.Second)
	if fr = m.Tick(&sent); fr.Devices != nil {
		t.Fatal("another packet of a counted flow does not move the list")
	}
}

func TestCountingAKnownDeviceDoesNotAllocate(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.m.dev.count(phone, 0)
	r.m.dev.name(phone, "video.example")
	n := testing.AllocsPerRun(200, func() {
		r.m.dev.count(phone, 0)
		r.m.dev.name(phone, "video.example")
	})
	if n != 0 {
		t.Fatalf("a known device and name cost %v allocations", n)
	}
}

func TestDeviceOfLooksUpTheClientMAC(t *testing.T) {
	SetMACProvider(func(ip string) string {
		if ip == "192.168.1.23" {
			return phone.MAC
		}
		return ""
	})
	t.Cleanup(func() { SetMACProvider(nil) })

	cases := []struct {
		name string
		addr net.Addr
		want Device
	}{
		{"tcp client", &net.TCPAddr{IP: net.ParseIP("192.168.1.23"), Port: 50000}, phone},
		{"udp client", &net.UDPAddr{IP: net.IPv4(192, 168, 1, 23), Port: 50000}, phone},
		{"v4-mapped on a dual-stack socket", &net.TCPAddr{IP: net.ParseIP("::ffff:192.168.1.23"), Port: 1}, phone},
		{"no MAC known", &net.TCPAddr{IP: net.ParseIP("192.168.1.99"), Port: 1}, Device{Addr: netip.MustParseAddr("192.168.1.99")}},
		{"link-local zone dropped", &net.UDPAddr{IP: net.ParseIP("fe80::1"), Port: 1, Zone: "br0"}, Device{Addr: netip.MustParseAddr("fe80::1")}},
		{"nil pointer", (*net.TCPAddr)(nil), Device{}},
		{"no address", nil, Device{}},
		{"unix socket", &net.UnixAddr{Name: "/tmp/b4.sock", Net: "unix"}, Device{}},
	}
	for _, c := range cases {
		if got := DeviceOf(c.addr); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}
