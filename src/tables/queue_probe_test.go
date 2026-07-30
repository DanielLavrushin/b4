package tables

import (
	"strings"
	"testing"
)

func TestQueueMatchWithLimit(t *testing.T) {
	t.Run("ct counters supported", func(t *testing.T) {
		args := queueMatchWithLimit([]string{"tcp", "dport", "443"}, "20", true)
		got := strings.Join(args, " ")
		want := "tcp dport 443 ct original packets < 20 counter"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("ct counters unsupported", func(t *testing.T) {
		args := queueMatchWithLimit([]string{"tcp", "dport", "443"}, "20", false)
		got := strings.Join(args, " ")
		want := "tcp dport 443 counter"
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("does not alias the match slice", func(t *testing.T) {
		match := []string{"udp", "dport", "443"}
		queueMatchWithLimit(match, "9", true)
		if len(match) != 3 {
			t.Errorf("match slice was mutated: %v", match)
		}
	})
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
	pkgs := nfqueuePkgsFor([]string{"nft_queue", "nft_ct"})
	got := strings.Join(pkgs, " ")
	if got != "kmod-nft-queue kmod-nft-core" {
		t.Errorf("got %q", got)
	}

	pkgs = nfqueuePkgsFor([]string{"xt_NFQUEUE", "xt_NFQUEUE"})
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
