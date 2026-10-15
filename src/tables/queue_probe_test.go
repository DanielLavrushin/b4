package tables

import (
	"strings"
	"testing"
)

func TestQueueMatchWithLimit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		match       string
		dir         string
		ctPackets   bool
		ctDirection bool
		want        string
	}{
		{"original counter", "tcp dport 443", "original", true, false, "tcp dport 443 ct original packets < 20 counter"},
		{"original counter and direction", "tcp dport 443", "original", true, true, "tcp dport 443 ct original packets < 20 ct direction original counter"},
		{"reply counter", "tcp sport 443", "reply", true, false, "tcp sport 443 ct reply packets < 20 counter"},
		{"reply counter and direction", "tcp sport 443", "reply", true, true, "tcp sport 443 ct reply packets < 20 ct direction reply counter"},
		{"ct counters unsupported", "tcp dport 443", "original", false, false, "tcp dport 443 counter"},
		{"direction without counters", "tcp sport 443", "reply", false, true, "tcp sport 443 ct direction reply counter"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Join(queueMatchWithLimit(strings.Fields(tc.match), tc.dir, "20", tc.ctPackets, tc.ctDirection), " ")
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("does not alias the match slice", func(t *testing.T) {
		backing := make([]string, 3, 16)
		copy(backing, []string{"udp", "dport", "443"})
		first := queueMatchWithLimit(backing, "original", "9", false, true)
		second := queueMatchWithLimit(backing, "reply", "9", false, false)
		if got := strings.Join(first, " "); got != "udp dport 443 ct direction original counter" {
			t.Errorf("a later call rewrote the result of an earlier one through the shared match slice: %q", got)
		}
		if got := strings.Join(second, " "); got != "udp dport 443 counter" {
			t.Errorf("got %q", got)
		}
		if got := strings.Join(backing[:cap(backing)][3:5], " "); got != " " {
			t.Errorf("the match slice's spare capacity was written: %q", got)
		}
	})
}

func TestQueueMatchInDirection(t *testing.T) {
	syn := []string{"tcp", "sport", "443", "tcp", "flags", "&", "(syn|ack)", "==", "(syn|ack)"}
	if got := strings.Join(queueMatchInDirection(syn, "reply", true), " "); got != "tcp sport 443 tcp flags & (syn|ack) == (syn|ack) ct direction reply counter" {
		t.Errorf("got %q", got)
	}
	if got := strings.Join(queueMatchInDirection(syn, "reply", false), " "); got != "tcp sport 443 tcp flags & (syn|ack) == (syn|ack) counter" {
		t.Errorf("without the direction match the rule must stay as it was, got %q", got)
	}
}

func TestNFQueueActionExpr(t *testing.T) {
	if got := nfqueueActionExpr(537, 1); got != "queue num 537 bypass" {
		t.Errorf("got %q", got)
	}
	if got := nfqueueActionExpr(537, 4); got != "queue num 537-540 bypass" {
		t.Errorf("got %q", got)
	}
}

func TestNFQueuePkgsFor(t *testing.T) {
	newKmodFixture(t)
	pkgs := kmodPkgsFor([]string{"nft_queue", "nft_ct"})
	got := strings.Join(pkgs, " ")
	if got != "kmod-nft-queue kmod-nft-core" {
		t.Errorf("got %q", got)
	}

	pkgs = kmodPkgsFor([]string{"xt_NFQUEUE", "xt_NFQUEUE"})
	if len(pkgs) != 3 {
		t.Errorf("expected deduplicated packages, got %v", pkgs)
	}
}

func TestContainsFatalQueueModule(t *testing.T) {
	if containsFatalQueueModule([]string{"nft_ct"}) {
		t.Error("missing ct counters should not be fatal")
	}
	if !containsFatalQueueModule([]string{"nft_ct", "nft_queue"}) {
		t.Error("missing nft_queue should be fatal")
	}
	if !containsFatalQueueModule([]string{"xt_NFQUEUE"}) {
		t.Error("missing xt_NFQUEUE should be fatal")
	}
}
