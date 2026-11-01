package nfq

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/iphealth"
)

type silentHelloRow struct {
	sniSet, domain, ipSet, meta string
}

func parseSilentHelloRow(line string) (silentHelloRow, bool) {
	f := strings.Split(line, ",")
	if len(f) < 9 {
		return silentHelloRow{}, false
	}
	r := silentHelloRow{sniSet: f[2], domain: f[3], ipSet: f[5]}
	if len(f) > 9 {
		r.meta = f[9]
	}
	return r, true
}

type silentHelloRig struct {
	h    *setOrderHarness
	pool *Pool
}

func newSilentHelloRig(t *testing.T, sets ...*config.SetConfig) *silentHelloRig {
	t.Helper()
	h := newSetOrderHarness(t, setOrderBothMACs, sets)
	r := &silentHelloRig{h: h}
	h.w.bans = newTestIPBans(t, func(ctx context.Context, ip net.IP, port, mark int) iphealth.TLSResult {
		return iphealth.TLSSilent
	})
	r.pool = &Pool{Workers: []*Worker{h.w}, state: &runtimeState{bans: h.w.bans, destState: h.w.destState}}
	r.pool.wireBans()
	orig := lookupFlow
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
		a := silentFlow()
		return a.flow, a.found, a.err
	}
	t.Cleanup(func() { lookupFlow = orig })
	return r
}

func banOnSilence(set *config.SetConfig) {
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.CacheBlockedIPs = true
}

func (r *silentHelloRig) resetAfterSilence(t *testing.T, name string, sport uint16) (silentHelloRow, silentHelloRow) {
	t.Helper()
	c := newSetOrderConn(setOrderGGCAddr, sport, uint32(sport)*10)
	r.h.syn(t, c)
	hello := r.h.hello(t, c, name)
	r.h.drain()
	r.pool.sweepSilentHellos(time.Now().Add(time.Minute))
	r.h.w.bans.wg.Wait()
	var resets []silentHelloRow
	for _, line := range r.h.drain() {
		if row, ok := parseSilentHelloRow(line); ok && row.meta == "ipblock-ban" {
			resets = append(resets, row)
		}
	}
	if len(resets) != 1 || len(hello.rows) != 1 {
		t.Fatalf("want one ClientHello line and one ipblock-ban reset line, got %d and %d", len(hello.rows), len(resets))
	}
	h := hello.rows[0]
	return silentHelloRow{sniSet: h.sniSet, domain: h.domain, ipSet: h.ipSet}, resets[0]
}

func TestASilentHelloResetOfAHeldConnectionShowsTheHoldingSetAsItsIPSet(t *testing.T) {
	ggc := newSetOrderGGCSetWith(true)
	banOnSilence(ggc)
	r := newSilentHelloRig(t, ggc, newSetOrderGWSet())
	hello, reset := r.resetAfterSilence(t, setOrderGGCName, 41001)
	if hello.sniSet != "" || hello.ipSet != setOrderGGCSet {
		t.Fatalf("the held ClientHello line shows sni-set=%q ip-set=%q, want the holding set as ip-set", hello.sniSet, hello.ipSet)
	}
	if reset.sniSet != "" || reset.ipSet != setOrderGGCSet || reset.domain != setOrderGGCName {
		t.Errorf("the reset line shows sni-set=%q ip-set=%q domain=%q, want ip-set=%q with the domain and no sni-set", reset.sniSet, reset.ipSet, reset.domain, setOrderGGCSet)
	}
}

func TestASilentHelloResetOfANameMatchedConnectionKeepsItsColumns(t *testing.T) {
	gw := newSetOrderGWSet()
	banOnSilence(gw)
	r := newSilentHelloRig(t, newSetOrderGGCSetWith(false), gw)
	hello, reset := r.resetAfterSilence(t, setOrderGGCName, 41002)
	if hello.sniSet != reset.sniSet || hello.ipSet != reset.ipSet {
		t.Errorf("the reset line shows sni-set=%q ip-set=%q, the ClientHello line sni-set=%q ip-set=%q", reset.sniSet, reset.ipSet, hello.sniSet, hello.ipSet)
	}
}

func TestASilentHelloResetOfAnAddressMatchedConnectionKeepsItsColumns(t *testing.T) {
	ggc := newSetOrderGGCSetWith(false)
	banOnSilence(ggc)
	r := newSilentHelloRig(t, ggc)
	hello, reset := r.resetAfterSilence(t, setOrderUnlisted, 41003)
	if hello.sniSet != reset.sniSet || hello.ipSet != reset.ipSet {
		t.Errorf("the reset line shows sni-set=%q ip-set=%q, the ClientHello line sni-set=%q ip-set=%q", reset.sniSet, reset.ipSet, hello.sniSet, hello.ipSet)
	}
}
