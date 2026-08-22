package mtproto

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/gorilla/websocket"
)

const (
	webCarrierReadLimit     = 2 << 20
	webCarrierBatchBytes    = 512 << 10
	webCarrierBatchFrames   = 1024
	webCarrierWriteWait     = 20 * time.Second
	webCarrierPingInterval  = 30 * time.Second
	webCarrierIdleTimeout   = 90 * time.Second
	webWindowFlushInterval  = 20 * time.Millisecond
	webWindowFlushBytes     = 256 << 10
	webClosedStreamMemory   = 4096
	webMaxStreamsPerCarrier = 512
)

type webSession struct {
	srv    *Server
	secret *Secret
	conn   *websocket.Conn
	host   string
	remote net.Addr
	tag    string

	outCh  chan []byte
	ctrlCh chan []byte
	done   chan struct{}

	closeOnce sync.Once
	closeErr  error

	mu        sync.Mutex
	welcomed  bool
	streams   map[uint32]*webStream
	recvCred  map[uint32]int64
	pendWin   map[uint32]int
	pendTotal int
	closedIDs map[uint32]struct{}
	closedSeq []uint32

	sentFirst bool

	serve func(*webStream)
}

func newWebSession(srv *Server, secret *Secret, conn *websocket.Conn, host string, remote net.Addr, tag string) *webSession {
	sess := &webSession{
		srv:       srv,
		secret:    secret,
		conn:      conn,
		host:      host,
		remote:    remote,
		tag:       tag,
		outCh:     make(chan []byte, 512),
		ctrlCh:    make(chan []byte, 64),
		done:      make(chan struct{}),
		streams:   make(map[uint32]*webStream),
		recvCred:  make(map[uint32]int64),
		pendWin:   make(map[uint32]int),
		closedIDs: make(map[uint32]struct{}),
	}
	sess.serve = func(st *webStream) { srv.serveWebStream(st, secret, host) }
	return sess
}

func (s *webSession) run() {
	s.conn.SetReadLimit(webCarrierReadLimit)
	_ = s.conn.SetReadDeadline(time.Now().Add(webCarrierIdleTimeout))
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(webCarrierIdleTimeout))
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.writeLoop() }()
	go func() { defer wg.Done(); s.windowLoop() }()

	err := s.readLoop()
	s.fail(err)
	wg.Wait()
	s.dropAllStreams()
}

func (s *webSession) readLoop() error {
	for {
		typ, msg, err := s.conn.ReadMessage()
		if err != nil {
			return err
		}
		if typ != websocket.BinaryMessage {
			continue
		}
		_ = s.conn.SetReadDeadline(time.Now().Add(webCarrierIdleTimeout))

		frames, err := parseWebFrames(msg)
		if err != nil {
			return err
		}
		for _, f := range frames {
			if err := s.dispatch(f); err != nil {
				return err
			}
		}
	}
}

func (s *webSession) dispatch(f webFrame) error {
	session := webFrameIsSession(f.typ)
	if session != (f.stream == 0) {
		return fmt.Errorf("%w: %s on stream %d", errWebFrameProtocol, f.typ, f.stream)
	}

	s.mu.Lock()
	welcomed := s.welcomed
	s.mu.Unlock()

	if !welcomed && f.typ != webFrameHello {
		return fmt.Errorf("%w: %s before HELLO", errWebFrameProtocol, f.typ)
	}

	switch f.typ {
	case webFrameHello:
		if welcomed {
			return fmt.Errorf("%w: repeated HELLO", errWebFrameProtocol)
		}
		if len(f.payload) != 1 || f.payload[0] != 1 {
			return fmt.Errorf("%w: unsupported HELLO version", errWebFrameProtocol)
		}
		s.mu.Lock()
		s.welcomed = true
		s.mu.Unlock()
		log.Debugf("%s web carrier handshake from %s (secret=%s)", s.tag, s.remote, s.secret.Label())
		return s.sendControl(webFrameBytes(webFrameWelcome, 0, nil))

	case webFrameOpen:
		if err := s.openStream(f.stream); err != nil {
			if errors.Is(err, errWebStreamRefused) {
				log.Debugf("%s web carrier refused stream %d: %v", s.tag, f.stream, err)
				s.rememberClosed(f.stream)
				return s.sendControl(webFrameBytes(webFrameClose, f.stream, nil))
			}
			return err
		}
		return nil

	case webFrameData:
		return s.streamData(f.stream, f.payload)

	case webFrameWindow:
		if len(f.payload) != 4 {
			return fmt.Errorf("%w: WINDOW payload %d bytes", errWebFrameProtocol, len(f.payload))
		}
		delta := binary.BigEndian.Uint32(f.payload)
		if delta == 0 || delta > webInitialWindow {
			return fmt.Errorf("%w: WINDOW delta %d", errWebFrameProtocol, delta)
		}
		if st := s.lookup(f.stream); st != nil {
			st.addSendCredit(int64(delta))
		} else if !s.recentlyClosed(f.stream) {
			return fmt.Errorf("%w: WINDOW for unknown stream %d", errWebFrameProtocol, f.stream)
		}
		return nil

	case webFrameClose:
		if st := s.lookup(f.stream); st != nil {
			s.closeStream(f.stream, false)
		} else if !s.recentlyClosed(f.stream) {
			return fmt.Errorf("%w: CLOSE for unknown stream %d", errWebFrameProtocol, f.stream)
		}
		return nil

	case webFramePing:
		return s.sendControl(webFrameBytes(webFramePong, 0, f.payload))

	case webFramePong:
		return nil

	case webFrameBye:
		return errors.New("carrier sent BYE")

	case webFrameWelcome, webFrameAuthChal, webFrameAuthResp:
		return fmt.Errorf("%w: unexpected %s from carrier", errWebFrameProtocol, f.typ)
	}
	return fmt.Errorf("%w: unhandled %s", errWebFrameProtocol, f.typ)
}

func (s *webSession) lookup(id uint32) *webStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.streams[id]
}

func (s *webSession) rememberClosed(id uint32) {
	s.mu.Lock()
	s.rememberClosedLocked(id)
	s.mu.Unlock()
}

func (s *webSession) recentlyClosed(id uint32) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.closedIDs[id]
	return ok
}

func (s *webSession) openStream(id uint32) error {
	s.mu.Lock()
	if _, exists := s.streams[id]; exists {
		s.mu.Unlock()
		return fmt.Errorf("%w: OPEN for live stream %d", errWebFrameProtocol, id)
	}
	if _, closed := s.closedIDs[id]; closed {
		s.mu.Unlock()
		return fmt.Errorf("%w: OPEN reuses stream %d", errWebFrameProtocol, id)
	}
	if len(s.streams) >= webMaxStreamsPerCarrier {
		s.mu.Unlock()
		return fmt.Errorf("%w: over %d concurrent streams", errWebStreamRefused, webMaxStreamsPerCarrier)
	}
	st := newWebStream(s, id, s.remote)
	s.streams[id] = st
	s.recvCred[id] = webInitialWindow
	s.mu.Unlock()

	go func() {
		defer s.closeStream(id, true)
		s.serve(st)
	}()
	return nil
}

func (s *webSession) streamData(id uint32, payload []byte) error {
	s.mu.Lock()
	st := s.streams[id]
	if st == nil {
		_, closed := s.closedIDs[id]
		s.mu.Unlock()
		if closed {
			return nil
		}
		return fmt.Errorf("%w: DATA for unknown stream %d", errWebFrameProtocol, id)
	}
	cred := s.recvCred[id]
	if int64(len(payload)) > cred {
		s.mu.Unlock()
		return fmt.Errorf("%w: stream %d sent %d bytes over %d credit", errWebFrameProtocol, id, len(payload), cred)
	}
	s.recvCred[id] = cred - int64(len(payload))
	s.mu.Unlock()

	buf := make([]byte, len(payload))
	copy(buf, payload)
	st.deliver(buf)
	return nil
}

func (s *webSession) grantWindow(id uint32, n int) {
	if n <= 0 {
		return
	}
	s.mu.Lock()
	if _, live := s.streams[id]; !live {
		s.mu.Unlock()
		return
	}
	s.recvCred[id] += int64(n)
	s.pendWin[id] += n
	s.pendTotal += n
	flush := s.pendTotal >= webWindowFlushBytes
	var batch []byte
	if flush {
		batch = s.drainWindowsLocked()
	}
	s.mu.Unlock()
	if batch != nil {
		_ = s.sendControl(batch)
	}
}

func (s *webSession) drainWindowsLocked() []byte {
	if len(s.pendWin) == 0 {
		return nil
	}
	var out []byte
	for id, n := range s.pendWin {
		for n > 0 {
			step := n
			if step > webInitialWindow {
				step = webInitialWindow
			}
			var p [4]byte
			binary.BigEndian.PutUint32(p[:], uint32(step))
			out = appendWebFrame(out, webFrameWindow, id, p[:])
			n -= step
		}
		delete(s.pendWin, id)
	}
	s.pendTotal = 0
	return out
}

func (s *webSession) windowLoop() {
	ticker := time.NewTicker(webWindowFlushInterval)
	defer ticker.Stop()
	ping := time.NewTicker(webCarrierPingInterval)
	defer ping.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.mu.Lock()
			batch := s.drainWindowsLocked()
			s.mu.Unlock()
			if batch != nil {
				_ = s.sendControl(batch)
			}
		case <-ping.C:
			_ = s.sendControl(webFrameBytes(webFramePing, 0, nil))
		}
	}
}

func (s *webSession) sendData(id uint32, payload []byte) error {
	frame := webFrameBytes(webFrameData, id, payload)
	select {
	case s.outCh <- frame:
		return nil
	case <-s.done:
		return errWebStreamClosed
	}
}

func (s *webSession) sendControl(frame []byte) error {
	select {
	case s.ctrlCh <- frame:
		return nil
	case <-s.done:
		return errWebStreamClosed
	default:
		s.fail(errors.New("control queue overflow"))
		return errWebStreamClosed
	}
}

func (s *webSession) writeLoop() {
	for {
		var batch []byte
		var frames int

		select {
		case <-s.done:
			return
		case b := <-s.ctrlCh:
			batch, frames = b, 1
		case b := <-s.outCh:
			batch, frames = b, 1
		}

	coalesce:
		for s.sentFirst && frames < webCarrierBatchFrames && len(batch) < webCarrierBatchBytes {
			select {
			case b := <-s.ctrlCh:
				batch = append(batch, b...)
				frames++
			default:
				select {
				case b := <-s.outCh:
					batch = append(batch, b...)
					frames++
				default:
					break coalesce
				}
			}
		}

		s.sentFirst = true
		_ = s.conn.SetWriteDeadline(time.Now().Add(webCarrierWriteWait))
		if err := s.conn.WriteMessage(websocket.BinaryMessage, batch); err != nil {
			s.fail(err)
			return
		}
	}
}

func (s *webSession) closeStream(id uint32, notify bool) {
	s.mu.Lock()
	st := s.streams[id]
	if st == nil {
		s.mu.Unlock()
		return
	}
	delete(s.streams, id)
	delete(s.recvCred, id)
	delete(s.pendWin, id)
	s.rememberClosedLocked(id)
	s.mu.Unlock()

	st.shutdown(errWebStreamClosed)
	if notify {
		_ = s.sendControl(webFrameBytes(webFrameClose, id, nil))
	}
}

func (s *webSession) rememberClosedLocked(id uint32) {
	if _, ok := s.closedIDs[id]; ok {
		return
	}
	s.closedIDs[id] = struct{}{}
	s.closedSeq = append(s.closedSeq, id)
	if len(s.closedSeq) > webClosedStreamMemory {
		oldest := s.closedSeq[0]
		s.closedSeq = s.closedSeq[1:]
		delete(s.closedIDs, oldest)
	}
}

func (s *webSession) dropAllStreams() {
	s.mu.Lock()
	streams := make([]*webStream, 0, len(s.streams))
	for id, st := range s.streams {
		streams = append(streams, st)
		delete(s.streams, id)
		delete(s.recvCred, id)
		delete(s.pendWin, id)
	}
	s.mu.Unlock()
	for _, st := range streams {
		st.shutdown(errWebStreamClosed)
	}
}

func (s *webSession) fail(err error) {
	s.closeOnce.Do(func() {
		s.closeErr = err
		close(s.done)
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
}
