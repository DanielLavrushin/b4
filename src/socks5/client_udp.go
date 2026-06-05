package socks5

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

type UDPUpstream struct {
	ctrl  net.Conn
	relay net.Conn
	hdr   []byte
}

func DialUpstreamUDP(ctx context.Context, cfg ClientConfig, dstIP net.IP, dstPort int) (*UDPUpstream, error) {
	if cfg.Host == "" || cfg.Port < 1 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid upstream config")
	}
	if dstPort < 1 || dstPort > 65535 {
		return nil, fmt.Errorf("invalid target port")
	}
	if dstIP == nil {
		return nil, fmt.Errorf("invalid target ip")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = dialTimeout
	}

	d := net.Dialer{Timeout: timeout}
	ApplyBypassMark(&d, cfg.BypassMark)
	ctrlAddr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	ctrl, err := d.DialContext(ctx, "tcp", ctrlAddr)
	if err != nil {
		return nil, fmt.Errorf("dial upstream: %w", err)
	}

	_ = ctrl.SetDeadline(time.Now().Add(timeout))
	if err := clientGreet(ctrl, cfg.Username, cfg.Password); err != nil {
		ctrl.Close()
		return nil, err
	}
	relayHost, relayPort, err := clientUDPAssociate(ctrl)
	if err != nil {
		ctrl.Close()
		return nil, err
	}
	_ = ctrl.SetDeadline(time.Time{})

	if ip := net.ParseIP(relayHost); ip == nil || ip.IsUnspecified() {
		relayHost = cfg.Host
	}

	ud := net.Dialer{Timeout: timeout}
	ApplyBypassMark(&ud, cfg.BypassMark)
	relayAddr := net.JoinHostPort(relayHost, strconv.Itoa(relayPort))
	relay, err := ud.DialContext(ctx, "udp", relayAddr)
	if err != nil {
		ctrl.Close()
		return nil, fmt.Errorf("dial relay: %w", err)
	}

	u := &UDPUpstream{
		ctrl:  ctrl,
		relay: relay,
		hdr:   buildUDPHeader(&net.UDPAddr{IP: dstIP, Port: dstPort}),
	}
	go u.watchCtrl()
	return u, nil
}

func (u *UDPUpstream) watchCtrl() {
	buf := make([]byte, 1)
	_, _ = u.ctrl.Read(buf)
	u.Close()
}

func (u *UDPUpstream) Write(payload []byte) (int, error) {
	pkt := make([]byte, 0, len(u.hdr)+len(payload))
	pkt = append(pkt, u.hdr...)
	pkt = append(pkt, payload...)
	if _, err := u.relay.Write(pkt); err != nil {
		return 0, err
	}
	return len(payload), nil
}

func (u *UDPUpstream) Read(buf []byte) (int, error) {
	n, err := u.relay.Read(buf)
	if err != nil {
		return 0, err
	}
	if n < 4 {
		return 0, fmt.Errorf("short datagram")
	}
	if buf[0] != 0 || buf[1] != 0 {
		return 0, fmt.Errorf("bad rsv")
	}
	if buf[2] != 0 {
		return 0, fmt.Errorf("fragmented datagram")
	}
	_, off, perr := parseUDPAddress(buf[:n])
	if perr != nil {
		return 0, perr
	}
	copy(buf, buf[off:n])
	return n - off, nil
}

func (u *UDPUpstream) SetReadDeadline(t time.Time) error {
	return u.relay.SetReadDeadline(t)
}

func (u *UDPUpstream) Close() error {
	if u.ctrl != nil {
		u.ctrl.Close()
	}
	if u.relay != nil {
		return u.relay.Close()
	}
	return nil
}

func clientUDPAssociate(conn net.Conn) (host string, port int, err error) {
	req := []byte{socks5Version, cmdUDPAssociate, 0x00, atypIPv4, 0, 0, 0, 0, 0, 0}
	if _, err := conn.Write(req); err != nil {
		return "", 0, fmt.Errorf("udp associate write: %w", err)
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return "", 0, fmt.Errorf("udp associate reply head: %w", err)
	}
	if head[0] != socks5Version {
		return "", 0, fmt.Errorf("upstream bad version in reply: %d", head[0])
	}
	if head[1] != repSuccess {
		return "", 0, fmt.Errorf("upstream udp associate rejected: code=%d", head[1])
	}

	var hostBuf []byte
	switch head[3] {
	case atypIPv4:
		hostBuf = make([]byte, 4)
	case atypIPv6:
		hostBuf = make([]byte, 16)
	case atypDomain:
		l := make([]byte, 1)
		if _, err := io.ReadFull(conn, l); err != nil {
			return "", 0, fmt.Errorf("udp associate reply addr len: %w", err)
		}
		hostBuf = make([]byte, int(l[0]))
	default:
		return "", 0, fmt.Errorf("upstream bad atyp in reply: %d", head[3])
	}
	if _, err := io.ReadFull(conn, hostBuf); err != nil {
		return "", 0, fmt.Errorf("udp associate reply addr: %w", err)
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", 0, fmt.Errorf("udp associate reply port: %w", err)
	}
	port = int(binary.BigEndian.Uint16(portBuf))

	switch head[3] {
	case atypIPv4, atypIPv6:
		host = net.IP(hostBuf).String()
	case atypDomain:
		host = string(hostBuf)
	}
	return host, port, nil
}
