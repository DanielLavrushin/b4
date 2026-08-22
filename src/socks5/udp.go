package socks5

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

// handleUDPAssociate handles the SOCKS5 UDP ASSOCIATE command.
// Creates a per-association UDP listener and relays packets.
func (s *Server) handleUDPAssociate(conn net.Conn, clientDest string) error {
	log.Infof("SOCKS5 UDP ASSOCIATE from %s, client dest: %s", conn.RemoteAddr(), clientDest)

	tcpRemote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		sendReply(conn, repServerFailure, nil)
		return fmt.Errorf("UDP associate on a non-TCP control connection")
	}
	clientIP := tcpRemote.IP
	var clientPort int
	if clientDest != "" {
		if _, portStr, err := net.SplitHostPort(clientDest); err == nil {
			if p, err := strconv.Atoi(portStr); err == nil {
				clientPort = p
			}
		}
	}

	localAddr, ok := conn.LocalAddr().(*net.TCPAddr)
	if !ok {
		sendReply(conn, repServerFailure, nil)
		return fmt.Errorf("UDP associate on a non-TCP control connection")
	}
	udpAddr := &net.UDPAddr{
		IP:   localAddr.IP,
		Port: 0, // Let OS assign port
	}

	bindLn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		log.Errorf("SOCKS5 UDP listen failed: %v", err)
		sendReply(conn, repServerFailure, nil)
		return fmt.Errorf("listen UDP failed: %w", err)
	}

	log.Infof("SOCKS5 UDP relay listening on %s", bindLn.LocalAddr())

	// Send success reply with UDP bind address
	if err := sendReply(conn, repSuccess, bindLn.LocalAddr()); err != nil {
		log.Errorf("SOCKS5 UDP send reply failed: %v", err)
		bindLn.Close()
		return fmt.Errorf("send UDP reply: %w", err)
	}

	// Clear handshake deadline - UDP associations are long-lived
	if err := conn.SetDeadline(time.Time{}); err != nil {
		log.Errorf("SOCKS5 UDP clear deadline failed: %v", err)
		bindLn.Close()
		return fmt.Errorf("clear deadline: %w", err)
	}

	// Start UDP relay in goroutine
	go s.udpRelay(bindLn, conn, clientIP, clientPort)

	// Keep TCP connection alive - when it closes, UDP association ends (RFC 1928)
	// Use a small buffer since we only care about detecting close
	buf := make([]byte, 1)
	for {
		_, err := conn.Read(buf)
		if err != nil {
			bindLn.Close()
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				log.Infof("SOCKS5 UDP associate closed: %s", conn.RemoteAddr())
				return nil
			}
			log.Errorf("SOCKS5 UDP TCP read error: %v", err)
			return err
		}
	}
}

// udpRelay handles UDP packet relay for a single client.
func (s *Server) udpRelay(bindLn *net.UDPConn, tcpConn net.Conn, expectedClientIP net.IP, expectedClientPort int) {
	defer bindLn.Close()

	log.Infof("SOCKS5 UDP relay started for %s", tcpConn.RemoteAddr())

	// Connection pool for persistent UDP connections
	conns := &sync.Map{}
	defer func() {
		conns.Range(func(key, value interface{}) bool {
			if conn, ok := value.(net.Conn); ok {
				conn.Close()
			}
			return true
		})
	}()

	bufPtr := s.bufferPool.Get().(*[]byte)
	buf := *bufPtr
	defer s.bufferPool.Put(bufPtr)

	for {
		n, srcAddr, err := bindLn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				log.Infof("SOCKS5 UDP relay closed for %s", tcpConn.RemoteAddr())
				return
			}
			log.Errorf("SOCKS5 UDP read: %v", err)
			continue
		}

		log.Debugf("SOCKS5 UDP received %d bytes from %s", n, srcAddr)

		// Validate client address (RFC 1928)
		// expectedClientIP defaults to TCP remote IP, so it's always set
		srcEqual := expectedClientIP.Equal(srcAddr.IP) &&
			(expectedClientPort == 0 || expectedClientPort == srcAddr.Port)

		if !srcEqual {
			log.Debugf("SOCKS5 UDP rejecting packet from unexpected address: %s (expected: %s:%d)",
				srcAddr, expectedClientIP, expectedClientPort)
			continue
		}

		// Parse SOCKS5 UDP datagram
		pkt := buf[:n]
		if len(pkt) < 10 {
			log.Debugf("SOCKS5 UDP packet too short: %d bytes", len(pkt))
			continue
		}

		// Check RSV and FRAG fields
		if pkt[0] != 0 || pkt[1] != 0 {
			log.Debugf("SOCKS5 UDP invalid RSV: %d %d", pkt[0], pkt[1])
			continue
		}
		if pkt[2] != 0 {
			log.Debugf("SOCKS5 UDP fragmentation not supported: %d", pkt[2])
			continue
		}

		// Parse destination address
		dest, dataOffset, err := parseUDPAddress(pkt)
		if err != nil {
			log.Debugf("SOCKS5 UDP parse address failed: %v", err)
			continue
		}

		// Copy data before passing to goroutine (buf is reused on next ReadFromUDP)
		data := make([]byte, n-dataOffset)
		copy(data, pkt[dataOffset:])
		log.Debugf("SOCKS5 UDP parsed: dest=%s, data_len=%d", dest, len(data))

		go s.handleUDPPacket(bindLn, srcAddr, dest, data, conns)
	}
}

// handleUDPPacket processes one incoming SOCKS5 UDP packet with connection pooling.
func (s *Server) handleUDPPacket(bindLn *net.UDPConn, srcAddr *net.UDPAddr, dest string, data []byte, conns *sync.Map) {
	connKey := srcAddr.String() + "--" + dest

	log.Debugf("SOCKS5 UDP handling: client=%s, dest=%s, data_len=%d", srcAddr, dest, len(data))

	// Fast path: reuse existing connection
	if target, ok := conns.Load(connKey); ok {
		if _, err := target.(net.Conn).Write(data); err != nil {
			log.Errorf("SOCKS5 UDP write to %s failed: %v", dest, err)
			target.(net.Conn).Close()
			conns.Delete(connKey)
		}
		return
	}

	// Slow path: create new connection
	targetNew, err := net.Dial("udp", dest)
	if err != nil {
		log.Errorf("SOCKS5 UDP dial to %s failed: %v", dest, err)
		return
	}

	// Use LoadOrStore to handle concurrent creation race
	if existing, loaded := conns.LoadOrStore(connKey, targetNew); loaded {
		// Another goroutine created the connection first; use theirs
		targetNew.Close()
		if _, err := existing.(net.Conn).Write(data); err != nil {
			log.Errorf("SOCKS5 UDP write to %s failed: %v", dest, err)
			existing.(net.Conn).Close()
			conns.Delete(connKey)
		}
		return
	}

	// We stored our connection; use the actual resolved address from the dialed connection
	// (not a separate ResolveUDPAddr which may pick a different IP for multi-A/AAAA hostnames)
	destUDP := targetNew.RemoteAddr().(*net.UDPAddr)

	go s.udpReadFromTarget(bindLn, targetNew, srcAddr, destUDP, connKey, conns)

	// Log metrics once per new connection (not per packet)
	s.logAndRecordConnection("UDP", srcAddr.String(), dest, "socks5")

	// Send initial data
	if _, err := targetNew.Write(data); err != nil {
		log.Errorf("SOCKS5 UDP write to %s failed: %v", dest, err)
		targetNew.Close()
		conns.Delete(connKey)
	}
}

// udpReadFromTarget reads responses from target server and sends back to client.
func (s *Server) udpReadFromTarget(bindLn *net.UDPConn, target net.Conn, srcAddr *net.UDPAddr, destAddr *net.UDPAddr, connKey string, conns *sync.Map) {
	defer func() {
		log.Debugf("SOCKS5 UDP closing connection: %s", connKey)
		target.Close()
		conns.Delete(connKey)
	}()

	readTimeout := time.Duration(s.getCfg().System.Socks5.UDPReadTimeout) * time.Second
	if readTimeout <= 0 {
		readTimeout = 30 * time.Second
	}

	bufPtr := s.bufferPool.Get().(*[]byte)
	buf := *bufPtr
	defer s.bufferPool.Put(bufPtr)

	for {
		target.SetReadDeadline(time.Now().Add(readTimeout))

		n, err := target.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				log.Debugf("SOCKS5 UDP target closed: %s", destAddr)
				return
			}
			log.Debugf("SOCKS5 UDP read from target %s failed: %v", destAddr, err)
			return
		}

		log.Debugf("SOCKS5 UDP received %d bytes from target %s", n, destAddr)

		// Build SOCKS5 UDP response header
		header := buildUDPHeader(destAddr)
		respLen := len(header) + n

		// Bounds check: ensure response fits in buffer
		if respLen > bufferSize {
			log.Errorf("SOCKS5 UDP response too large (%d bytes), dropping", respLen)
			continue
		}

		// Use a temp buffer from pool to assemble the response
		tmpBufPtr := s.bufferPool.Get().(*[]byte)
		tmpBuf := *tmpBufPtr
		copy(tmpBuf, header)
		copy(tmpBuf[len(header):], buf[:n])

		if _, err := bindLn.WriteToUDP(tmpBuf[:respLen], srcAddr); err != nil {
			s.bufferPool.Put(tmpBufPtr)
			log.Errorf("SOCKS5 UDP failed to reply to client %s: %v", srcAddr, err)
			return
		}

		s.bufferPool.Put(tmpBufPtr)
	}
}

// parseUDPAddress extracts the destination address from a SOCKS5 UDP packet.
// Returns the address string and the offset where payload data begins.
func parseUDPAddress(pkt []byte) (addr string, dataOffset int, err error) {
	if len(pkt) < 4 {
		return "", 0, fmt.Errorf("packet too short")
	}

	atyp := pkt[3]
	switch atyp {
	case atypIPv4:
		if len(pkt) < 10 {
			return "", 0, fmt.Errorf("packet too short for IPv4")
		}
		ip := net.IP(pkt[4:8])
		port := binary.BigEndian.Uint16(pkt[8:10])
		return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), 10, nil

	case atypIPv6:
		if len(pkt) < 22 {
			return "", 0, fmt.Errorf("packet too short for IPv6")
		}
		ip := net.IP(pkt[4:20])
		port := binary.BigEndian.Uint16(pkt[20:22])
		return net.JoinHostPort(ip.String(), strconv.Itoa(int(port))), 22, nil

	case atypDomain:
		if len(pkt) < 5 {
			return "", 0, fmt.Errorf("packet too short for domain length")
		}
		dlen := int(pkt[4])
		end := 5 + dlen + 2
		if len(pkt) < end {
			return "", 0, fmt.Errorf("packet too short for domain")
		}
		domain := string(pkt[5 : 5+dlen])
		port := binary.BigEndian.Uint16(pkt[5+dlen : end])
		return net.JoinHostPort(domain, strconv.Itoa(int(port))), end, nil

	default:
		return "", 0, fmt.Errorf("unsupported address type %d", atyp)
	}
}

// buildUDPHeader constructs a SOCKS5 UDP response header (without data).
func buildUDPHeader(from *net.UDPAddr) []byte {
	// RSV(2) + FRAG(1) + ATYP(1) + ADDR(4|16) + PORT(2)
	var hdr []byte
	hdr = append(hdr, 0, 0, 0) // RSV, FRAG

	if ip4 := from.IP.To4(); ip4 != nil {
		hdr = append(hdr, atypIPv4)
		hdr = append(hdr, ip4...)
	} else {
		hdr = append(hdr, atypIPv6)
		hdr = append(hdr, from.IP.To16()...)
	}

	portBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(portBuf, uint16(from.Port))
	hdr = append(hdr, portBuf...)

	return hdr
}
