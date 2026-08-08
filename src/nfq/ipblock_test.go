package nfq

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/iphealth"
)

func encodeTestName(name string) []byte {
	buf := make([]byte, 0, len(name)+2)
	for _, label := range splitLabels(name) {
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}
	return append(buf, 0)
}

func splitLabels(name string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if i > start {
				out = append(out, name[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func buildTestAResponse(domain string, ips ...string) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], 0x4242)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	binary.BigEndian.PutUint16(msg[6:8], uint16(len(ips)))

	msg = append(msg, encodeTestName(domain)...)
	var q [4]byte
	binary.BigEndian.PutUint16(q[0:2], 1)
	binary.BigEndian.PutUint16(q[2:4], 1)
	msg = append(msg, q[:]...)

	for _, ip := range ips {
		msg = append(msg, 0xC0, 0x0C)
		var fixed [10]byte
		binary.BigEndian.PutUint16(fixed[0:2], 1)
		binary.BigEndian.PutUint16(fixed[2:4], 1)
		binary.BigEndian.PutUint32(fixed[4:8], 3600)
		binary.BigEndian.PutUint16(fixed[8:10], 4)
		msg = append(msg, fixed[:]...)
		msg = append(msg, net.ParseIP(ip).To4()...)
	}
	return msg
}

func waitFor(t *testing.T, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

func healWorker(t *testing.T, dead ...string) *Worker {
	t.Helper()
	health := iphealth.NewTracker(func(string, uint16, int) bool { return false })
	t.Cleanup(health.Stop)

	for _, ip := range dead {
		for i := 0; i < 3; i++ {
			health.RecordSyn(ip, 443, 0, 3, time.Hour)
		}
	}
	if len(dead) > 0 && !waitFor(t, func() bool { return health.IsDead(dead[len(dead)-1]) }) {
		t.Fatal("seed addresses never became dead")
	}

	return &Worker{ipHealth: health, goodIPs: iphealth.NewKnownGood(), hostHints: newHostHintCache()}
}

func healSetWith(healDNS bool) *config.SetConfig {
	set := config.NewSetConfig()
	set.Name = "github"
	set.Enabled = true
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.HealDNS = healDNS
	return &set
}

func TestHealDNSResponseStripsUnreachableAddress(t *testing.T) {
	w := healWorker(t, "185.199.110.133")
	set := healSetWith(true)
	resp := buildTestAResponse("raw.githubusercontent.com",
		"185.199.108.133", "185.199.109.133", "185.199.110.133", "185.199.111.133")

	healed := w.healDNSResponse(&config.Config{}, set, "raw.githubusercontent.com", resp)
	if healed == nil {
		t.Fatal("expected a curated answer")
	}

	ips := dns.ParseResponseIPs(healed)
	if len(ips) != 3 {
		t.Fatalf("kept %d addresses, want 3", len(ips))
	}
	for _, ip := range ips {
		if ip.String() == "185.199.110.133" {
			t.Errorf("the unreachable address survived")
		}
	}
}

func TestHealDNSResponseIgnoredWhenHealingOff(t *testing.T) {
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	w := healWorker(t, "2.2.2.2")
	if healed := w.healDNSResponse(&config.Config{}, healSetWith(false), "example.com", resp); healed != nil {
		t.Errorf("DNS answers must be left alone while heal_dns is off")
	}
}

func TestHealDNSResponseSkippedWhenDetectionOff(t *testing.T) {
	w := healWorker(t, "2.2.2.2")
	set := healSetWith(true)
	set.TCP.IPBlockDetect.Enabled = false
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	if healed := w.healDNSResponse(&config.Config{}, set, "example.com", resp); healed != nil {
		t.Errorf("expected no rewrite while IP block detection is off")
	}
}

func TestHealDNSResponseSkippedInDiscovery(t *testing.T) {
	w := healWorker(t, "2.2.2.2")
	cfg := &config.Config{}
	cfg.Queue.IsDiscovery = true
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	if healed := w.healDNSResponse(cfg, healSetWith(true), "example.com", resp); healed != nil {
		t.Errorf("a discovery run must not rewrite DNS answers")
	}
}

func TestHealDNSResponseFallsBackToRememberedAddress(t *testing.T) {
	w := healWorker(t, "1.1.1.1", "2.2.2.2")
	w.goodIPs.Remember("example.com", net.ParseIP("3.3.3.3"))
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	healed := w.healDNSResponse(&config.Config{}, healSetWith(true), "example.com", resp)
	if healed == nil {
		t.Fatal("expected an answer built from the remembered address")
	}
	ips := dns.ParseResponseIPs(healed)
	if len(ips) != 1 || ips[0].String() != "3.3.3.3" {
		t.Errorf("addresses = %v, want [3.3.3.3]", ips)
	}
}

func TestHealDNSResponsePassesThroughWhenNothingRemembered(t *testing.T) {
	w := healWorker(t, "1.1.1.1", "2.2.2.2")
	resp := buildTestAResponse("example.com", "1.1.1.1", "2.2.2.2")

	if healed := w.healDNSResponse(&config.Config{}, healSetWith(true), "example.com", resp); healed != nil {
		t.Errorf("expected the original answer to pass through rather than an empty one")
	}
}

func TestSynDetectEnabled(t *testing.T) {
	set := healSetWith(false)
	if !synDetectEnabled(set) {
		t.Errorf("a new set with detection on should watch SYNs by default")
	}
	set.TCP.IPBlockDetect.SynDetect = false
	if synDetectEnabled(set) {
		t.Errorf("SYN watching should follow its own switch")
	}
	if synDetectEnabled(nil) {
		t.Errorf("a nil set must not enable SYN watching")
	}
}
