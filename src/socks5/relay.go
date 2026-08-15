package socks5

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

var (
	relayIdleTimeout   = time.Hour
	relayHalfCloseIdle = 5 * time.Minute
	relayWatchInterval = 15 * time.Second
)

var relayBufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, bufferSize)
		return &buf
	},
}

type relayState struct {
	last       atomic.Int64
	halfClosed atomic.Bool
	idleLimit  time.Duration
	halfLimit  time.Duration
}

func (s *relayState) touch() { s.last.Store(time.Now().UnixNano()) }

func (s *relayState) idle() time.Duration {
	return time.Since(time.Unix(0, s.last.Load()))
}

func (s *relayState) limit() time.Duration {
	if s.halfClosed.Load() {
		return s.halfLimit
	}
	return s.idleLimit
}

type relayReader struct {
	src net.Conn
	st  *relayState
}

func (r *relayReader) Read(p []byte) (int, error) {
	n, err := r.src.Read(p)
	if n > 0 {
		r.st.touch()
	}
	return n, err
}

type relayWriter struct{ dst io.Writer }

func (w relayWriter) Write(p []byte) (int, error) { return w.dst.Write(p) }

func relayTick(idleLimit, halfLimit time.Duration) time.Duration {
	tick := halfLimit / 4
	if t := idleLimit / 4; t < tick {
		tick = t
	}
	if tick > relayWatchInterval {
		tick = relayWatchInterval
	}
	if tick < 50*time.Millisecond {
		tick = 50 * time.Millisecond
	}
	return tick
}

func relayBenign(err error) bool {
	return err == nil ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, os.ErrDeadlineExceeded)
}

func Relay(a, b net.Conn) error {
	var closeOnce sync.Once
	shutdown := func() {
		closeOnce.Do(func() {
			_ = a.Close()
			_ = b.Close()
		})
	}

	st := &relayState{idleLimit: relayIdleTimeout, halfLimit: relayHalfCloseIdle}
	st.touch()
	tick := relayTick(st.idleLimit, st.halfLimit)

	errs := make(chan error, 2)
	cp := func(dst, src net.Conn) {
		bufPtr := relayBufPool.Get().(*[]byte)
		_, err := io.CopyBuffer(relayWriter{dst: dst}, &relayReader{src: src, st: st}, *bufPtr)
		relayBufPool.Put(bufPtr)
		if err != nil {
			shutdown()
			errs <- err
			return
		}
		if c, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
		st.halfClosed.Store(true)
		errs <- nil
	}

	go cp(a, b)
	go cp(b, a)

	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if st.idle() > st.limit() {
					shutdown()
					return
				}
			}
		}
	}()

	err1 := <-errs
	err2 := <-errs
	close(done)
	shutdown()

	if !relayBenign(err1) {
		return err1
	}
	if !relayBenign(err2) {
		return err2
	}
	return nil
}
