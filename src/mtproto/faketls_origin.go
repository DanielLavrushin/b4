package mtproto

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	mrand "math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/log"
	"golang.org/x/crypto/cryptobyte"
)

const (
	tlsRecordAlert      = 0x15
	originProbeTimeout  = 10 * time.Second
	originProbeRounds   = 3
	originRefreshEvery  = 6 * time.Hour
	maxOriginHosts      = 8
	minAppDataRecord    = 64
	maxAppDataRecord    = 16640
	maxOriginRecordSize = 16384 + 2048
)

var originProbePort = "443"

var originRetryEvery = 5 * time.Minute

var fallbackAppData = minAppDataRecord + mrand.Intn(97)

var (
	errOriginNotTLS13 = errors.New("does not speak TLS 1.3")
	errOriginRetry    = errors.New("asks for a key share b4 does not send")
	helloRetryRandom  = sha256.Sum256([]byte("HelloRetryRequest"))
)

type originReply struct {
	cipher        uint16
	group         uint16
	versionsFirst bool
	appData       int
}

type originProfile struct {
	appData       [2]int
	cipher        uint16
	group         uint16
	versionsFirst bool
}

func originClientHello(host string, hybrid bool) ([]byte, error) {
	cfg := &tls.Config{
		ServerName:         host,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{"h2", "http/1.1"},
		InsecureSkipVerify: true,
	}
	if !hybrid {
		cfg.CurvePreferences = []tls.CurveID{tls.X25519, tls.CurveP256, tls.CurveP384}
	}
	cli, srv := net.Pipe()
	defer srv.Close()
	go func() {
		_ = tls.Client(cli, cfg).Handshake()
		_ = cli.Close()
	}()
	_ = srv.SetReadDeadline(time.Now().Add(5 * time.Second))
	hdr := make([]byte, 5)
	if _, err := io.ReadFull(srv, hdr); err != nil {
		return nil, err
	}
	hello := make([]byte, 5+int(binary.BigEndian.Uint16(hdr[3:5])))
	copy(hello, hdr)
	if _, err := io.ReadFull(srv, hello[5:]); err != nil {
		return nil, err
	}
	telegramSuiteOrder(hello)
	return hello, nil
}

func telegramSuiteOrder(hello []byte) {
	p := 5 + 4 + 2 + 32
	if len(hello) <= p {
		return
	}
	p += 1 + int(hello[p])
	if len(hello) < p+8 || binary.BigEndian.Uint16(hello[p:]) != 6 {
		return
	}
	suites := hello[p+2 : p+8]
	seen := 0
	for i := 0; i < 6; i += 2 {
		switch binary.BigEndian.Uint16(suites[i:]) {
		case 0x1301:
			seen |= 1
		case 0x1302:
			seen |= 2
		case 0x1303:
			seen |= 4
		}
	}
	if seen != 7 {
		return
	}
	binary.BigEndian.PutUint16(suites[0:], 0x1301)
	binary.BigEndian.PutUint16(suites[2:], 0x1302)
	binary.BigEndian.PutUint16(suites[4:], 0x1303)
}

func probeOrigin(ctx context.Context, host string, mark uint, hybrid bool) (originReply, error) {
	hello, err := originClientHello(host, hybrid)
	if err != nil {
		return originReply{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, originProbeTimeout)
	defer cancel()
	conn, err := markedDialer(originProbeTimeout, mark).DialContext(ctx, "tcp", net.JoinHostPort(host, originProbePort))
	if err != nil {
		return originReply{}, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	if _, err := conn.Write(hello); err != nil {
		return originReply{}, err
	}
	return readOriginReply(conn)
}

func readOriginReply(r io.Reader) (originReply, error) {
	var reply originReply
	sawHello := false
	hdr := make([]byte, 5)
	for {
		if _, err := io.ReadFull(r, hdr); err != nil {
			return reply, err
		}
		length := int(binary.BigEndian.Uint16(hdr[3:5]))
		if length > maxOriginRecordSize {
			return reply, fmt.Errorf("record of %d bytes", length)
		}
		switch hdr[0] {
		case tlsRecordAppData:
			if !sawHello {
				return reply, errors.New("encrypted record before the ServerHello")
			}
			reply.appData = length
			return reply, nil
		case tlsRecordAlert:
			return reply, errOriginNotTLS13
		case tlsRecordChangeCipher:
			if _, err := io.CopyN(io.Discard, r, int64(length)); err != nil {
				return reply, err
			}
		case tlsRecordHandshake:
			if sawHello {
				return reply, errOriginNotTLS13
			}
			body := make([]byte, length)
			if _, err := io.ReadFull(r, body); err != nil {
				return reply, err
			}
			parsed, err := parseOriginServerHello(body)
			if err != nil {
				return reply, err
			}
			reply = parsed
			sawHello = true
		default:
			return reply, fmt.Errorf("unexpected record type 0x%02x", hdr[0])
		}
	}
}

func parseOriginServerHello(body []byte) (originReply, error) {
	var reply originReply
	s := cryptobyte.String(body)
	var hsType, compression uint8
	var version uint16
	var msg, sid, exts cryptobyte.String
	var random []byte
	if !s.ReadUint8(&hsType) || hsType != handshakeServerHello || !s.ReadUint24LengthPrefixed(&msg) {
		return reply, errors.New("first handshake message is not a ServerHello")
	}
	if !msg.ReadUint16(&version) || !msg.ReadBytes(&random, 32) || !msg.ReadUint8LengthPrefixed(&sid) ||
		!msg.ReadUint16(&reply.cipher) || !msg.ReadUint8(&compression) || !msg.ReadUint16LengthPrefixed(&exts) {
		return reply, errors.New("malformed ServerHello")
	}
	tls13 := false
	versionsAt, keyShareAt := -1, -1
	for i := 0; !exts.Empty(); i++ {
		var typ uint16
		var data cryptobyte.String
		if !exts.ReadUint16(&typ) || !exts.ReadUint16LengthPrefixed(&data) {
			return reply, errors.New("malformed ServerHello extensions")
		}
		switch typ {
		case 0x002b:
			var v uint16
			tls13 = data.ReadUint16(&v) && v == tls.VersionTLS13
			versionsAt = i
		case extKeyShare:
			if !data.ReadUint16(&reply.group) {
				return reply, errors.New("malformed key_share")
			}
			keyShareAt = i
		}
	}
	if !tls13 {
		return reply, errOriginNotTLS13
	}
	if [32]byte(random) == helloRetryRandom {
		return reply, errOriginRetry
	}
	reply.versionsFirst = versionsAt < keyShareAt
	return reply, nil
}

func profileOrigin(ctx context.Context, host string, mark uint) (originProfile, error) {
	var replies []originReply
	hybrid := true
	var lastErr error
	for i := 0; i < originProbeRounds; i++ {
		reply, err := probeOrigin(ctx, host, mark, hybrid)
		if err != nil && hybrid && len(replies) == 0 && !errors.Is(err, errOriginNotTLS13) && !errors.Is(err, errOriginRetry) && ctx.Err() == nil {
			hybrid = false
			reply, err = probeOrigin(ctx, host, mark, hybrid)
		}
		if err != nil {
			if errors.Is(err, errOriginNotTLS13) || errors.Is(err, errOriginRetry) || ctx.Err() != nil {
				return originProfile{}, err
			}
			lastErr = err
			continue
		}
		replies = append(replies, reply)
	}
	if len(replies) == 0 {
		return originProfile{}, lastErr
	}
	p := originProfile{
		appData:       [2]int{clampAppData(replies[0].appData), clampAppData(replies[0].appData)},
		cipher:        replies[0].cipher,
		versionsFirst: replies[0].versionsFirst,
	}
	if hybrid {
		p.group = replies[0].group
	}
	for _, r := range replies[1:] {
		p.appData[0] = min(p.appData[0], clampAppData(r.appData))
		p.appData[1] = max(p.appData[1], clampAppData(r.appData))
	}
	return p, nil
}

func clampAppData(n int) int {
	return min(max(n, minAppDataRecord), maxAppDataRecord)
}

func (p originProfile) appDataLen() int {
	lo, hi := p.appData[0], p.appData[1]
	if lo == 0 {
		return fallbackAppData
	}
	if hi <= lo {
		return lo
	}
	return lo + mrand.Intn(hi-lo+1)
}

func (p originProfile) String() string {
	size := strconv.Itoa(p.appData[0])
	if p.appData[1] != p.appData[0] {
		size += "-" + strconv.Itoa(p.appData[1])
	}
	group := "the key exchange the app offers"
	switch p.group {
	case groupX25519:
		group = "X25519 only"
	case groupX25519MLKEM768:
		group = "post-quantum key exchange"
	}
	return fmt.Sprintf("%s-byte record, cipher 0x%04x, %s", size, p.cipher, group)
}

func (s *Server) originFor(host string) originProfile {
	m := s.origins.Load()
	if m == nil {
		return originProfile{}
	}
	if p, ok := (*m)[strings.ToLower(host)]; ok {
		return p
	}
	if cfg := s.cfg.Load(); cfg != nil {
		if p, ok := (*m)[strings.ToLower(cfg.System.MTProto.FakeSNI)]; ok {
			return p
		}
	}
	return originProfile{}
}

func (s *Server) originHosts() []string {
	var hosts []string
	seen := map[string]bool{}
	add := func(h string) {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" || seen[h] || len(hosts) >= maxOriginHosts {
			return
		}
		seen[h] = true
		hosts = append(hosts, h)
	}
	if cfg := s.cfg.Load(); cfg != nil {
		add(cfg.System.MTProto.FakeSNI)
	}
	if ptr := s.secrets.Load(); ptr != nil {
		for _, sec := range *ptr {
			add(sec.Host)
		}
	}
	return hosts
}

func (s *Server) kickOrigins() {
	select {
	case s.originKick <- struct{}{}:
	default:
	}
}

func (s *Server) watchOrigins(ctx context.Context) {
	states := map[string]string{}
	all := true
	for {
		wait := originRefreshEvery
		if s.profileOrigins(ctx, all, states) {
			wait = originRetryEvery
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			all = wait == originRefreshEvery
		case <-s.originKick:
			timer.Stop()
			all = false
		}
	}
}

func (s *Server) profileOrigins(ctx context.Context, all bool, states map[string]string) (retry bool) {
	var old map[string]originProfile
	if m := s.origins.Load(); m != nil {
		old = *m
	}
	hosts := s.originHosts()
	next := make(map[string]originProfile, len(hosts))
	for _, host := range hosts {
		if p, ok := old[host]; ok && !all {
			next[host] = p
			continue
		}
		p, err := profileOrigin(ctx, host, selfDialMark())
		if ctx.Err() != nil {
			return false
		}
		noteOrigin(states, host, p, err)
		if err == nil {
			next[host] = p
			continue
		}
		if prev, ok := old[host]; ok {
			next[host] = prev
		}
		if !errors.Is(err, errOriginNotTLS13) && !errors.Is(err, errOriginRetry) {
			retry = true
		}
	}
	s.origins.Store(&next)
	return retry
}

func noteOrigin(states map[string]string, host string, p originProfile, err error) {
	state := fmt.Sprintf("MTProto fake TLS follows %s: %s", host, p)
	logf := log.Infof
	switch {
	case errors.Is(err, errOriginNotTLS13), errors.Is(err, errOriginRetry):
		state = fmt.Sprintf("MTProto fake TLS cannot follow %s, which %v; a fake domain that speaks TLS 1.3 fits better", host, err)
		logf = log.Warnf
	case err != nil:
		state = fmt.Sprintf("MTProto fake TLS could not reach %s to follow its answer: %v", host, err)
	}
	if states[host] == state {
		return
	}
	states[host] = state
	logf("%s", state)
}
