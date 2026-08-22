package mtproto

import (
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"time"
)

type webDeadline struct {
	mu     sync.Mutex
	timer  *time.Timer
	cancel chan struct{}
}

func (d *webDeadline) set(t time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.timer != nil && !d.timer.Stop() {
		<-d.cancel
	}
	d.timer = nil

	closed := d.cancel != nil && isClosedChan(d.cancel)
	if t.IsZero() {
		if closed {
			d.cancel = make(chan struct{})
		}
		return
	}
	if closed {
		d.cancel = make(chan struct{})
	} else if d.cancel == nil {
		d.cancel = make(chan struct{})
	}
	if dur := time.Until(t); dur > 0 {
		cancel := d.cancel
		d.timer = time.AfterFunc(dur, func() { close(cancel) })
		return
	}
	close(d.cancel)
}

func (d *webDeadline) wait() chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel == nil {
		d.cancel = make(chan struct{})
	}
	return d.cancel
}

func isClosedChan(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

type webAddr struct {
	network string
	value   string
}

func (a webAddr) Network() string { return a.network }
func (a webAddr) String() string  { return a.value }

var errWebStreamClosed = errors.New("web proxy stream closed")

type webStream struct {
	id   uint32
	sess *webSession

	local  net.Addr
	remote net.Addr

	mu       sync.Mutex
	inbound  []byte
	readErr  error
	closed   bool
	dataCh   chan struct{}
	sendCred int64
	credCh   chan struct{}

	rDeadline webDeadline
	wDeadline webDeadline

	closeOnce sync.Once
}

func newWebStream(sess *webSession, id uint32, remote net.Addr) *webStream {
	return &webStream{
		id:       id,
		sess:     sess,
		local:    webAddr{network: "webproxy", value: sess.host},
		remote:   remote,
		dataCh:   make(chan struct{}, 1),
		sendCred: webInitialWindow,
		credCh:   make(chan struct{}, 1),
	}
}

func (c *webStream) signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (c *webStream) deliver(p []byte) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.inbound = append(c.inbound, p...)
	c.mu.Unlock()
	c.signal(c.dataCh)
}

func (c *webStream) addSendCredit(n int64) {
	c.mu.Lock()
	c.sendCred += n
	c.mu.Unlock()
	c.signal(c.credCh)
}

func (c *webStream) shutdown(err error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	if c.readErr == nil {
		c.readErr = err
	}
	c.mu.Unlock()
	c.signal(c.dataCh)
	c.signal(c.credCh)
}

func (c *webStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for {
		c.mu.Lock()
		if n := len(c.inbound); n > 0 {
			copied := copy(p, c.inbound)
			c.inbound = c.inbound[copied:]
			if len(c.inbound) == 0 {
				c.inbound = nil
			}
			c.mu.Unlock()
			c.sess.grantWindow(c.id, copied)
			return copied, nil
		}
		closed, err := c.closed, c.readErr
		c.mu.Unlock()

		if closed {
			if err == nil {
				err = io.EOF
			}
			return 0, err
		}
		select {
		case <-c.dataCh:
		case <-c.rDeadline.wait():
			return 0, os.ErrDeadlineExceeded
		}
	}
}

func (c *webStream) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > webMaxDataChunk {
			chunk = chunk[:webMaxDataChunk]
		}

		c.mu.Lock()
		for !c.closed && c.sendCred <= 0 {
			c.mu.Unlock()
			select {
			case <-c.credCh:
			case <-c.wDeadline.wait():
				return written, os.ErrDeadlineExceeded
			}
			c.mu.Lock()
		}
		if c.closed {
			err := c.readErr
			c.mu.Unlock()
			if err == nil {
				err = errWebStreamClosed
			}
			return written, err
		}
		if int64(len(chunk)) > c.sendCred {
			chunk = chunk[:c.sendCred]
		}
		c.sendCred -= int64(len(chunk))
		c.mu.Unlock()

		if err := c.sess.sendData(c.id, chunk); err != nil {
			return written, err
		}
		written += len(chunk)
		p = p[len(chunk):]
	}
	return written, nil
}

func (c *webStream) Close() error {
	c.closeOnce.Do(func() {
		c.shutdown(errWebStreamClosed)
		c.sess.closeStream(c.id, true)
	})
	return nil
}

func (c *webStream) LocalAddr() net.Addr  { return c.local }
func (c *webStream) RemoteAddr() net.Addr { return c.remote }

func (c *webStream) SetDeadline(t time.Time) error {
	c.rDeadline.set(t)
	c.wDeadline.set(t)
	return nil
}

func (c *webStream) SetReadDeadline(t time.Time) error {
	c.rDeadline.set(t)
	return nil
}

func (c *webStream) SetWriteDeadline(t time.Time) error {
	c.wDeadline.set(t)
	return nil
}
