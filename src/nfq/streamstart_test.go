package nfq

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestStreamStartIsTheByteAfterTheSyn(t *testing.T) {
	isn := uint32(0xFFFFFFFF)
	now := time.Now()
	for _, family := range comboFamilies() {
		t.Run(family.name, func(t *testing.T) {
			c := newStreamStartCache()
			syn, tcpAt := comboSyn(t, family.packet, family.parse, isn)
			c.Open(syn, tcpAt, now)

			data := family.packet([]byte("GET / HTTP/1.1\r\n\r\n"), isn+1)
			if !c.Starts(data, tcpAt, isn+1) {
				t.Fatal("the packet right after the SYN, across the sequence wrap, opens the stream")
			}
			if c.Starts(data, tcpAt, isn+2) {
				t.Fatal("a packet one byte later is not the stream's start")
			}
			other := family.packet(nil, isn+1)
			binary.BigEndian.PutUint16(other[tcpAt:], 54321)
			if c.Starts(other, tcpAt, isn+1) {
				t.Fatal("another connection's packet at the same number is not this stream's start")
			}

			resyn, _ := comboSyn(t, family.packet, family.parse, 7000)
			c.Open(resyn, tcpAt, now)
			if c.Starts(data, tcpAt, isn+1) || !c.Starts(data, tcpAt, 7001) {
				t.Fatal("a new SYN on the same ports starts a new stream")
			}
		})
	}
}

func TestStreamStartsDoNotMixAddressFamilies(t *testing.T) {
	c := newStreamStartCache()
	v4syn, v4at := comboSyn(t, makeV4TCPPacket, ExtractPacketInfoV4, 100)
	c.Open(v4syn, v4at, time.Now())
	v6data := makeV6TCPPacket(nil, 101)
	if c.Starts(v6data, IPv6HeaderLen, 101) {
		t.Fatal("an IPv6 packet never matches an IPv4 stream")
	}
	if c.Starts(makeV4TCPPacket(nil, 101)[:IPv4HeaderMinLen+4], IPv4HeaderMinLen, 101) {
		t.Fatal("a truncated header matches nothing")
	}
	var nilCache *streamStartCache
	nilCache.Open(v4syn, v4at, time.Now())
	nilCache.Cleanup(time.Now())
	if nilCache.Starts(makeV4TCPPacket(nil, 101), v4at, 101) {
		t.Fatal("a missing cache knows no stream")
	}
}

func streamSynFromPort(t *testing.T, port uint16, isn uint32) ([]byte, int) {
	t.Helper()
	syn, tcpAt := comboSyn(t, makeV4TCPPacket, ExtractPacketInfoV4, isn)
	binary.BigEndian.PutUint16(syn[tcpAt:], port)
	return syn, tcpAt
}

func TestStreamStartsExpireAndStayBounded(t *testing.T) {
	c := newStreamStartCache()
	now := c.rotated
	stale, tcpAt := streamSynFromPort(t, 1, 100)
	c.Open(stale, tcpAt, now)
	c.Cleanup(now.Add(streamStartTTL - time.Second))
	if !c.Starts(stale, tcpAt, 101) {
		t.Fatal("a stream younger than its lifetime stays")
	}
	c.Cleanup(now.Add(streamStartTTL))
	fresh, _ := streamSynFromPort(t, 2, 500)
	c.Open(fresh, tcpAt, now.Add(streamStartTTL+time.Second))
	if !c.Starts(stale, tcpAt, 101) {
		t.Fatal("one cleanup past the lifetime keeps the stream for one more period")
	}
	c.Cleanup(now.Add(2 * streamStartTTL))
	if c.Starts(stale, tcpAt, 101) || !c.Starts(fresh, tcpAt, 501) {
		t.Fatal("two cleanups drop a stream past its lifetime and keep the one opened between them")
	}

	reopened, _ := streamSynFromPort(t, 1, 100)
	c.Open(reopened, tcpAt, now)
	c.Cleanup(now.Add(3 * streamStartTTL))
	again, _ := streamSynFromPort(t, 1, 9000)
	c.Open(again, tcpAt, now.Add(3*streamStartTTL))
	if c.Starts(again, tcpAt, 101) || !c.Starts(again, tcpAt, 9001) {
		t.Fatal("a new SYN on the ports of a stream from the previous period starts a new stream")
	}

	c = newStreamStartCache()
	for i := range maxStreamStarts {
		syn, _ := streamSynFromPort(t, uint16(i), 100)
		c.Open(syn, tcpAt, now)
		if held := len(c.cur) + len(c.prev); held > maxStreamStarts {
			t.Fatalf("the cache holds %d streams, want at most %d", held, maxStreamStarts)
		}
	}
	first, _ := streamSynFromPort(t, 0, 100)
	last, _ := streamSynFromPort(t, maxStreamStarts-1, 100)
	if c.Starts(first, tcpAt, 101) || !c.Starts(last, tcpAt, 101) {
		t.Fatal("a cache that keeps filling drops its oldest streams and keeps the newest")
	}
}

func TestASynIsRecordedOnlyWhileASetHasAnOverlap(t *testing.T) {
	const isn = uint32(70000)
	for _, overlap := range []int{0, 64} {
		set := overlapTestSet(overlap, []byte{0x16, 0x03, 0x03, 0x00, 0x00})
		set.Id, set.Name, set.Enabled = "overlap", "Overlap", true
		set.Targets.IpsToMatch = []string{"1.2.3.4"}
		w := newDualStackInjectingWorker(t, &set)
		syn, tcpAt := comboSyn(t, makeV4TCPPacket, ExtractPacketInfoV4, isn)
		w.ProcessPacket(syn)
		if got := w.streams.Starts(makeV4TCPPacket(nil, isn+1), tcpAt, isn+1); got != (overlap > 0) {
			t.Errorf("overlap %d: the SYN recorded a stream start %v, want %v", overlap, got, overlap > 0)
		}
	}
}

func TestTheStreamStartGateFollowsAConfigReload(t *testing.T) {
	const isn = uint32(80000)
	setWith := func(overlap int) *config.SetConfig {
		set := overlapTestSet(overlap, []byte{0x16, 0x03, 0x03, 0x00, 0x00})
		set.Id, set.Name, set.Enabled = "overlap", "Overlap", true
		set.Targets.IpsToMatch = []string{"1.2.3.4"}
		return &set
	}
	configWith := func(overlap int) *config.Config {
		cfg := config.NewConfig()
		cfg.Sets = []*config.SetConfig{setWith(overlap)}
		return &cfg
	}
	w := newDualStackInjectingWorker(t, setWith(64))
	for i, step := range []struct {
		reload *config.Config
		want   bool
	}{
		{nil, true},
		{configWith(0), false},
		{configWith(64), true},
		{configWith(0), false},
	} {
		if step.reload != nil {
			w.UpdateConfig(step.reload)
		}
		syn, tcpAt := streamSynFromPort(t, uint16(2000+i), isn)
		w.ProcessPacket(syn)
		if got := w.streams.Starts(syn, tcpAt, isn+1); got != step.want {
			t.Fatalf("step %d: the SYN recorded a stream start %v, want %v for the config installed then", i, got, step.want)
		}
	}
}

func TestOnlyASetWithAnOverlapRecordsStreams(t *testing.T) {
	cfg := config.NewConfig()
	plain := config.NewSetConfig()
	cfg.Sets = []*config.SetConfig{&plain}
	if usesSequenceOverlap(&cfg) {
		t.Fatal("a set without an overlap needs no stream starts")
	}
	lengthOnly := config.NewSetConfig()
	lengthOnly.Fragmentation.SeqOverlapLength = 64
	cfg.Sets = append(cfg.Sets, &lengthOnly)
	if usesSequenceOverlap(&cfg) {
		t.Fatal("an overlap length without a pattern sends no overlap")
	}
	overlap := overlapTestSet(64, []byte{0x16, 0x03, 0x03, 0x00, 0x00})
	cfg.Sets = append(cfg.Sets, nil, &overlap)
	if !usesSequenceOverlap(&cfg) {
		t.Fatal("a set with an overlap length and a pattern needs stream starts")
	}
}
