package nfq

import "time"

const (
	wsSizeHelloMax     = 64 << 10
	wsSizeHelloBudget  = 4 << 20
	wsSizeHelloPackets = 128
	wsSizeHelloTimeout = 5 * time.Second
)

type wsSizeHello struct {
	data []byte
	seen []byte
	next int
}

func (c *wsSizeCache) releaseHello(f *wsSizeFlow) {
	if f.hello != nil {
		c.helloBytes -= len(f.hello.data) + len(f.hello.seen)
		f.hello = nil
	}
}

func (c *wsSizeCache) dropLocked(key streamKey) {
	f := c.flows[key]
	c.releaseHello(&f)
	delete(c.flows, key)
}

func (c *wsSizeCache) growHello(f *wsSizeFlow, need int) bool {
	if f.hello != nil && len(f.hello.data) >= need {
		return true
	}
	size := 256
	for size < need {
		size *= 2
	}
	old := 0
	if f.hello != nil {
		old = len(f.hello.data) + len(f.hello.seen)
	}
	cost := size + (size+7)/8
	if c.helloBytes-old+cost > wsSizeHelloBudget {
		return false
	}
	h := &wsSizeHello{data: make([]byte, size), seen: make([]byte, (size+7)/8)}
	if f.hello != nil {
		copy(h.data, f.hello.data)
		copy(h.seen, f.hello.seen)
		h.next = f.hello.next
	}
	f.hello = h
	c.helloBytes += cost - old
	return true
}

func (c *wsSizeCache) rewriteHello(f *wsSizeFlow, payload []byte, seq uint32, syn bool, now time.Time) bool {
	if !now.Before(f.helloDeadline) {
		c.releaseHello(f)
		f.cutoff, f.helloEnd = true, 0
		return false
	}
	if syn {
		seq++
	}
	offset := int(int32(seq - (f.isn + 1)))
	if f.cutoff {
		return len(payload) > 0 && f.helloEnd > 0 && offset < f.helloEnd && offset+len(payload) > 0
	}
	if len(payload) == 0 {
		return true
	}
	f.helloPackets++
	fallback := func() bool {
		f.cutoff = true
		c.releaseHello(f)
		return true
	}
	if f.helloPackets > wsSizeHelloPackets {
		return fallback()
	}
	if offset < 0 {
		if offset <= -len(payload) {
			return true
		}
		payload, offset = payload[-offset:], 0
	}
	if offset >= wsSizeHelloMax || len(payload) > wsSizeHelloMax-offset {
		return fallback()
	}
	if !c.growHello(f, offset+len(payload)) {
		return fallback()
	}
	h := f.hello
	for i, b := range payload {
		at := offset + i
		bit := byte(1 << (at % 8))
		if h.seen[at/8]&bit != 0 && h.data[at] != b {
			return fallback()
		}
		h.data[at] = b
		h.seen[at/8] |= bit
	}
	for h.next < len(h.data) && h.seen[h.next/8]&(1<<(h.next%8)) != 0 {
		h.next++
	}
	end, valid := wsSizeClientHelloEnd(h.data[:h.next])
	if !valid {
		return fallback()
	}
	if end > 0 {
		f.cutoff, f.helloEnd = true, end
		c.releaseHello(f)
	}
	return true
}

func wsSizeClientHelloEnd(data []byte) (int, bool) {
	var header [4]byte
	filled, left, pos := 0, -1, 0
	for records := 0; records < wsSizeHelloPackets; records++ {
		if len(data)-pos > 0 && data[pos] != TLSHandshakeType {
			return 0, false
		}
		if len(data)-pos < 5 {
			return 0, true
		}
		n := int(data[pos+3])<<8 | int(data[pos+4])
		if data[pos+1] != TLSVersionMajor || data[pos+2] > TLSMaxVersionMinor || n == 0 || n > maxTLSRecordLen {
			return 0, false
		}
		pos += 5
		recordEnd := pos + n
		if recordEnd > wsSizeHelloMax {
			return 0, false
		}
		available := min(recordEnd, len(data))
		for filled < len(header) && pos < available {
			header[filled] = data[pos]
			filled++
			pos++
			if header[0] != TLSClientHello {
				return 0, false
			}
		}
		if filled == len(header) && left < 0 {
			left = int(header[1])<<16 | int(header[2])<<8 | int(header[3])
			if left == 0 || left > wsSizeHelloMax-pos {
				return 0, false
			}
		}
		if left >= 0 {
			take := min(left, available-pos)
			left -= take
			pos += take
			if left == 0 {
				return pos, true
			}
		}
		if available < recordEnd {
			return 0, true
		}
		pos = recordEnd
	}
	return 0, false
}
