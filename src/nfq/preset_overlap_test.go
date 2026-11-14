package nfq_test

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/nfq"
)

func realClientHello(t *testing.T, name string) []byte {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn := tls.Client(client, &tls.Config{ServerName: name})
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_ = conn.Handshake()
	}()
	t.Cleanup(func() {
		server.Close()
		client.Close()
		<-done
	})
	_ = server.SetDeadline(time.Now().Add(5 * time.Second))
	head := make([]byte, 5)
	if _, err := io.ReadFull(server, head); err != nil {
		t.Fatalf("read the record header of the ClientHello: %v", err)
	}
	body := make([]byte, binary.BigEndian.Uint16(head[3:5]))
	if _, err := io.ReadFull(server, body); err != nil {
		t.Fatalf("read the ClientHello: %v", err)
	}
	return append(head, body...)
}

func everyDiscoveryPreset() []discovery.ConfigPreset {
	families := []discovery.StrategyFamily{
		discovery.FamilyTCPFrag, discovery.FamilyTLSRec, discovery.FamilyOOB, discovery.FamilyIPFrag,
		discovery.FamilyFakeSNI, discovery.FamilySACK, discovery.FamilySynFake, discovery.FamilyDesync,
		discovery.FamilyWindow, discovery.FamilyDelay, discovery.FamilyMutation, discovery.FamilyDisorder,
		discovery.FamilyOverlap, discovery.FamilyExtSplit, discovery.FamilyFirstByte, discovery.FamilyCombo,
		discovery.FamilyHybrid, discovery.FamilyIncoming,
	}
	presets := discovery.GetPhase1Presets()
	best := map[discovery.StrategyFamily]discovery.ConfigPreset{}
	for _, family := range families {
		optimized := discovery.GetPhase2Presets(family)
		if len(optimized) > 0 {
			best[family] = optimized[0]
		}
		presets = append(presets, optimized...)
	}
	return append(presets, discovery.GetCombinationPresets(families, best)...)
}

func TestEveryDiscoveryPresetWithAnOverlapLengthSendsItsOverlap(t *testing.T) {
	hello := realClientHello(t, "overlap.example.org")
	checked := map[string]int{}
	for _, preset := range everyDiscoveryPreset() {
		frag := preset.Config.Fragmentation
		length := frag.SeqOverlapLength
		if length <= 0 {
			continue
		}
		var at int
		switch frag.Strategy {
		case "combo":
			at = -length
		case "disorder":
			at = frag.Disorder.SplitPosition - length
		default:
			t.Fatalf("%s sets an overlap length under the %q strategy, which sends none", preset.Name, frag.Strategy)
		}
		if at+length >= len(hello) {
			t.Fatalf("%s cuts at byte %d and the ClientHello holds %d, so this test proves nothing", preset.Name, at+length, len(hello))
		}
		if preset.Config.Faking.SNIMutation.Mode != config.ConfigOff {
			t.Fatalf("%s rewrites the ClientHello, so its pieces cannot be compared with the packet", preset.Name)
		}
		set := preset.Config
		set.Fragmentation.FillSeqOverlapBytes()
		pattern := set.Fragmentation.SeqOverlapBytes
		if len(pattern) == 0 {
			t.Fatalf("%s sets an overlap length and no pattern", preset.Name)
		}
		for _, ipv6 := range []bool{false, true} {
			var got *nfq.InjectedPiece
			var starts []int
			for _, p := range nfq.InjectedRealPieces(t, &set, ipv6, hello) {
				starts = append(starts, p.Offset)
				if p.Offset == at && len(p.Data) > length {
					got = &p
				}
			}
			if got == nil {
				t.Errorf("%s, IPv6 %v: no piece starts at %d, %d bytes before byte %d of the ClientHello; the pieces start at %v", preset.Name, ipv6, at, length, at+length, starts)
				continue
			}
			want := append(bytes.Repeat(pattern, length/len(pattern)+1)[:length:length], hello[at+length:at+len(got.Data)]...)
			if !bytes.Equal(got.Data, want) {
				t.Errorf("%s, IPv6 %v: the piece at %d is not %d pattern bytes and then the ClientHello from byte %d on", preset.Name, ipv6, at, length, at+length)
			}
		}
		checked[frag.Strategy]++
	}
	for _, strategy := range []string{"combo", "disorder"} {
		if checked[strategy] == 0 {
			t.Errorf("no %s preset sets an overlap length, so this test proves nothing about %s", strategy, strategy)
		}
	}
}
