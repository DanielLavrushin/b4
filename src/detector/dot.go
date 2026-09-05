package detector

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

type dotConn struct {
	conn *tls.Conn
}

func dialDoT(ctx context.Context, mark uint, host string, port int, timeout time.Duration) (*dotConn, error) {
	if port == 0 {
		port = 853
	}
	raw, err := netprobe.Dialer(int(mark), timeout, timeout).DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	conf := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, InsecureSkipVerify: noCABundle()}
	c := tls.Client(raw, conf)
	hctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := c.HandshakeContext(hctx); err != nil {
		raw.Close()
		return nil, err
	}
	return &dotConn{conn: c}, nil
}

func (d *dotConn) query(query []byte, timeout time.Duration) ([]byte, error) {
	d.conn.SetDeadline(time.Now().Add(timeout))
	msg := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(msg, uint16(len(query)))
	copy(msg[2:], query)
	if _, err := d.conn.Write(msg); err != nil {
		return nil, err
	}
	var hdr [2]byte
	if _, err := io.ReadFull(d.conn, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	if n == 0 || n > 65535 {
		return nil, fmt.Errorf("bad DoT length %d", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(d.conn, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (d *dotConn) close() {
	if d != nil && d.conn != nil {
		d.conn.Close()
	}
}
