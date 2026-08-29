package mtproto

import (
	"fmt"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func limitTestServer(t *testing.T, limit int) (*Server, func(mut func(*config.MTProtoConfig)) *config.Config, *Secret, *Secret) {
	t.Helper()
	srv, mkCfg, secA, secB := revocationTestServer(t)
	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets[0].MaxNetworks = limit
	}))
	return srv, mkCfg, secA, secB
}

func conn(ip string, port int) *closeRecordConn {
	return &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP(ip), Port: port}}
}

func TestLimitAdmitsManyConnsFromOneNetwork(t *testing.T) {
	srv, _, secA, _ := limitTestServer(t, 2)

	for i := 0; i < 12; i++ {
		_, _, deny := srv.trackConn(secA, conn("85.233.150.240", 1000+i))
		if deny.limit != 0 {
			t.Fatalf("conn %d from the same network was refused", i)
		}
	}
	if got := statForSecret(t, srv, "Max").Networks; got != 1 {
		t.Fatalf("Networks = %d, want 1", got)
	}
}

func TestLimitRefusesTheNextNetwork(t *testing.T) {
	srv, _, secA, secB := limitTestServer(t, 2)

	for i, ip := range []string{"85.233.150.240", "178.130.140.98"} {
		if _, _, deny := srv.trackConn(secA, conn(ip, 2000+i)); deny.limit != 0 {
			t.Fatalf("network %s was refused below the limit", ip)
		}
	}

	_, untrack, deny := srv.trackConn(secA, conn("203.0.113.9", 2100))
	if deny.limit != 2 {
		t.Fatalf("third network must be refused with limit 2, got %+v", deny)
	}
	if deny.total != 1 || !deny.log {
		t.Fatalf("first refusal must be counted and logged, got %+v", deny)
	}
	untrack()

	if got := statForSecret(t, srv, "Max").Networks; got != 2 {
		t.Fatalf("a refused conn must not be tracked: Networks = %d, want 2", got)
	}

	if _, _, deny := srv.trackConn(secB, conn("203.0.113.9", 2101)); deny.limit != 0 {
		t.Fatalf("an unlimited secret must not inherit another secret's limit")
	}
}

func TestLimitRefusalIsRateLimited(t *testing.T) {
	srv, _, secA, _ := limitTestServer(t, 1)
	srv.trackConn(secA, conn("85.233.150.240", 3000))

	logged := 0
	for i := 0; i < 5; i++ {
		_, _, deny := srv.trackConn(secA, conn(fmt.Sprintf("203.0.113.%d", i+1), 3100+i))
		if deny.limit == 0 {
			t.Fatalf("refusal %d did not happen", i)
		}
		if deny.log {
			logged++
		}
		if deny.total != int64(i+1) {
			t.Fatalf("refused total = %d, want %d", deny.total, i+1)
		}
	}
	if logged != 1 {
		t.Fatalf("only the first refusal in the window may log, got %d", logged)
	}
}

func TestLimitFreesSlotWhenNetworkLeaves(t *testing.T) {
	srv, _, secA, _ := limitTestServer(t, 1)

	_, untrack, _ := srv.trackConn(secA, conn("85.233.150.240", 4000))
	if _, _, deny := srv.trackConn(secA, conn("178.130.140.98", 4001)); deny.limit == 0 {
		t.Fatal("second network must be refused at limit 1")
	}
	untrack()
	if _, _, deny := srv.trackConn(secA, conn("178.130.140.98", 4002)); deny.limit != 0 {
		t.Fatal("slot must be free once the first network is gone")
	}
}

func TestLimitCountsIPv6By64(t *testing.T) {
	srv, _, secA, _ := limitTestServer(t, 1)

	srv.trackConn(secA, conn("2a00:1370:8190:1234::1", 5000))
	if _, _, deny := srv.trackConn(secA, conn("2a00:1370:8190:1234:aaaa:bbbb:cccc:dddd", 5001)); deny.limit != 0 {
		t.Fatal("a rotated address inside the same /64 must reuse the slot")
	}
	if _, _, deny := srv.trackConn(secA, conn("2a00:1370:8190:9999::1", 5002)); deny.limit == 0 {
		t.Fatal("a different /64 must be refused at limit 1")
	}
}

func TestZeroLimitIsUnlimited(t *testing.T) {
	srv, _, secA, _ := limitTestServer(t, 0)

	for i := 0; i < 40; i++ {
		ip := fmt.Sprintf("10.%d.%d.1", i/256, i%256)
		if _, _, deny := srv.trackConn(secA, conn(ip, 6000+i)); deny.limit != 0 {
			t.Fatalf("network %d refused although the secret is unlimited", i)
		}
	}
	if got := statForSecret(t, srv, "Max").Networks; got != 40 {
		t.Fatalf("Networks = %d, want 40", got)
	}
}

func TestLoweringLimitTrimsNewestNetworks(t *testing.T) {
	srv, mkCfg, secA, _ := limitTestServer(t, 0)

	oldest := conn("85.233.150.240", 7000)
	middle := conn("178.130.140.98", 7001)
	newest := conn("203.0.113.9", 7002)
	for _, c := range []*closeRecordConn{oldest, middle, newest} {
		srv.trackConn(secA, c)
	}

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets[0].MaxNetworks = 1
	}))

	if oldest.closed.Load() {
		t.Fatal("the longest-established network must survive a lowered limit")
	}
	if !middle.closed.Load() || !newest.closed.Load() {
		t.Fatalf("newer networks must be dropped: middle=%v newest=%v",
			middle.closed.Load(), newest.closed.Load())
	}
}

func TestRaisingLimitClosesNothing(t *testing.T) {
	srv, mkCfg, secA, _ := limitTestServer(t, 1)

	c := conn("85.233.150.240", 8000)
	srv.trackConn(secA, c)

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets[0].MaxNetworks = 9
	}))

	if c.closed.Load() {
		t.Fatal("raising the limit must not close anything")
	}
	if _, _, deny := srv.trackConn(secA, conn("178.130.140.98", 8001)); deny.limit != 0 {
		t.Fatal("the raised limit must take effect for new networks")
	}
}

func TestLimitReadFromLiveSecretNotStalePointer(t *testing.T) {
	srv, mkCfg, secA, _ := limitTestServer(t, 0)

	srv.trackConn(secA, conn("85.233.150.240", 9000))

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets[0].MaxNetworks = 1
	}))

	if _, _, deny := srv.trackConn(secA, conn("178.130.140.98", 9001)); deny.limit != 1 {
		t.Fatalf("the limit must come from the live secrets, not the stale *Secret held by the caller: %+v", deny)
	}
}

func TestSlotHeldWhileAnyConnFromNetworkRemains(t *testing.T) {
	srv, _, secA, _ := limitTestServer(t, 1)

	first := conn("85.233.150.240", 10000)
	second := conn("85.233.150.240", 10001)
	_, releaseFirst, _ := srv.trackConn(secA, first)
	srv.trackConn(secA, second)

	releaseFirst()

	if got := statForSecret(t, srv, "Max").Networks; got != 1 {
		t.Fatalf("the network must still be held by the surviving conn: Networks = %d, want 1", got)
	}
	if _, _, deny := srv.trackConn(secA, conn("178.130.140.98", 10002)); deny.limit != 1 {
		t.Fatal("the slot must still be occupied, so another network must be refused")
	}
	if _, _, deny := srv.trackConn(secA, conn("85.233.150.240", 10003)); deny.limit != 0 {
		t.Fatal("the still-held network must keep being admitted")
	}
}

func TestRefusalCountSurvivesTheConnSetChurning(t *testing.T) {
	srv, _, secA, _ := limitTestServer(t, 1)

	_, release, _ := srv.trackConn(secA, conn("85.233.150.240", 11000))
	if _, _, deny := srv.trackConn(secA, conn("178.130.140.98", 11001)); deny.total != 1 {
		t.Fatalf("first refusal total = %d, want 1", deny.total)
	}
	release()

	_, _, _ = srv.trackConn(secA, conn("85.233.150.240", 11002))
	_, _, deny := srv.trackConn(secA, conn("178.130.140.98", 11003))
	if deny.total != 2 {
		t.Fatalf("the refusal count must survive the set being dropped and rebuilt: total = %d, want 2", deny.total)
	}
	if deny.log {
		t.Fatal("the log rate limit must survive the set being dropped and rebuilt")
	}
}

func TestRefusalStateDroppedWithTheSecret(t *testing.T) {
	srv, mkCfg, secA, _ := limitTestServer(t, 1)

	srv.trackConn(secA, conn("85.233.150.240", 12000))
	srv.trackConn(secA, conn("178.130.140.98", 12001))

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets = m.Secrets[1:]
	}))

	srv.connsMu.Lock()
	left := len(srv.refusals)
	srv.connsMu.Unlock()
	if left != 0 {
		t.Fatalf("a removed secret must not leave refusal state behind, got %d entries", left)
	}
}

func TestTrimmedNetworkCannotReclaimItsSlot(t *testing.T) {
	srv, mkCfg, secA, _ := limitTestServer(t, 0)

	oldest := conn("85.233.150.240", 13000)
	doomed := conn("178.130.140.98", 13001)
	srv.trackConn(secA, oldest)
	srv.trackConn(secA, doomed)

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets[0].MaxNetworks = 1
	}))
	if !doomed.closed.Load() {
		t.Fatal("the newer network must have been closed by the trim")
	}

	_, _, deny := srv.trackConn(secA, conn("178.130.140.98", 13002))
	if deny.limit != 1 {
		t.Fatalf("a trimmed network reconnecting before its old conns untrack must be refused, got %+v", deny)
	}
	if got := statForSecret(t, srv, "Max").Networks; got != 1 {
		t.Fatalf("the secret must not be left above its limit: Networks = %d, want 1", got)
	}
}
