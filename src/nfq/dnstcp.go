package nfq

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/socks5"
	"golang.org/x/sys/unix"
)

const (
	dnsTCPMaxMessage  = 65535
	dnsTCPMinQueryLen = 12
)

type dnsTCPServer struct {
	port     int
	worker   *Worker
	ctx      context.Context
	cancel   context.CancelFunc
	lnV4     net.Listener
	lnV6     net.Listener
	wgAccept sync.WaitGroup
	wg       sync.WaitGroup
}

func newDNSTCPServer(w *Worker, port int) *dnsTCPServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &dnsTCPServer{port: port, worker: w, ctx: ctx, cancel: cancel}
}

func (s *dnsTCPServer) Start() error {
	if s.port < 1 || s.port > 65535 {
		return fmt.Errorf("invalid dns tcp port: %d", s.port)
	}
	cfg := s.worker.getConfig()
	if !cfg.Queue.IPv4Enabled && !cfg.Queue.IPv6Enabled {
		s.cancel()
		return errors.New("dns tcp listen: both IPv4 and IPv6 are disabled")
	}

	lc := net.ListenConfig{}
	var err4, err6 error

	if cfg.Queue.IPv4Enabled {
		var lnV4 net.Listener
		if lnV4, err4 = lc.Listen(s.ctx, "tcp4", net.JoinHostPort("0.0.0.0", fmt.Sprintf("%d", s.port))); err4 == nil {
			s.lnV4 = lnV4
			s.wgAccept.Add(1)
			go s.acceptLoop(lnV4, "v4")
			log.Infof("DNS: TCP listener on 0.0.0.0:%d (matched queries resolved through the set's DNS, others forwarded)", s.port)
		} else {
			log.Tracef("DNS TCP: v4 listener unavailable: %v", err4)
		}
	}

	if cfg.Queue.IPv6Enabled {
		var lnV6 net.Listener
		if lnV6, err6 = lc.Listen(s.ctx, "tcp6", net.JoinHostPort("::", fmt.Sprintf("%d", s.port))); err6 == nil {
			s.lnV6 = lnV6
			s.wgAccept.Add(1)
			go s.acceptLoop(lnV6, "v6")
			log.Infof("DNS: TCP listener on [::]:%d", s.port)
		} else {
			log.Tracef("DNS TCP: v6 listener unavailable: %v", err6)
		}
	}

	if s.lnV4 == nil && s.lnV6 == nil {
		s.cancel()
		return fmt.Errorf("dns tcp listen: v4: %v, v6: %v", err4, err6)
	}
	return nil
}

func (s *dnsTCPServer) ReadyV4() bool { return s.lnV4 != nil }

func (s *dnsTCPServer) ReadyV6() bool { return s.lnV6 != nil }

func (s *dnsTCPServer) Stop() {
	s.cancel()
	if s.lnV4 != nil {
		_ = s.lnV4.Close()
	}
	if s.lnV6 != nil {
		_ = s.lnV6.Close()
	}
	s.wgAccept.Wait()
	s.wg.Wait()
}

func (s *dnsTCPServer) acceptLoop(ln net.Listener, family string) {
	defer s.wgAccept.Done()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if s.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			log.Tracef("DNS TCP: accept error (%s): %v", family, err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			s.handle(c)
		}(conn)
	}
}

func originalDst(c net.Conn) (net.IP, int, error) {
	tc, ok := c.(*net.TCPConn)
	if !ok {
		return nil, 0, errors.New("not a tcp connection")
	}
	la, ok := tc.LocalAddr().(*net.TCPAddr)
	if !ok {
		return nil, 0, errors.New("no tcp local address")
	}
	raw, err := tc.SyscallConn()
	if err != nil {
		return nil, 0, err
	}

	if la.IP.To4() != nil {
		var mreq *unix.IPv6Mreq
		var inner error
		if err := raw.Control(func(fd uintptr) {
			mreq, inner = unix.GetsockoptIPv6Mreq(int(fd), unix.IPPROTO_IP, unix.SO_ORIGINAL_DST)
		}); err != nil {
			return nil, 0, err
		}
		if inner != nil {
			return nil, 0, inner
		}
		ip := net.IPv4(mreq.Multiaddr[4], mreq.Multiaddr[5], mreq.Multiaddr[6], mreq.Multiaddr[7])
		port := int(mreq.Multiaddr[2])<<8 | int(mreq.Multiaddr[3])
		return ip, port, nil
	}

	var info *unix.IPv6MTUInfo
	var inner error
	if err := raw.Control(func(fd uintptr) {
		info, inner = unix.GetsockoptIPv6MTUInfo(int(fd), unix.IPPROTO_IPV6, unix.SO_ORIGINAL_DST)
	}); err != nil {
		return nil, 0, err
	}
	if inner != nil {
		return nil, 0, inner
	}
	p := (*[2]byte)(unsafe.Pointer(&info.Addr.Port))
	port := int(p[0])<<8 | int(p[1])
	ip := make(net.IP, net.IPv6len)
	copy(ip, info.Addr.Addr[:])
	return ip, port, nil
}

func readDNSTCPMessage(r io.Reader) ([]byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(hdr[:]))
	if n == 0 || n > dnsTCPMaxMessage {
		return nil, fmt.Errorf("bad dns tcp length %d", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func writeDNSTCPMessage(w net.Conn, msg []byte, timeout time.Duration) error {
	if len(msg) == 0 || len(msg) > dnsTCPMaxMessage {
		return fmt.Errorf("bad dns tcp response length %d", len(msg))
	}
	out := make([]byte, 2+len(msg))
	binary.BigEndian.PutUint16(out[0:2], uint16(len(msg)))
	copy(out[2:], msg)
	if timeout > 0 {
		_ = w.SetWriteDeadline(time.Now().Add(timeout))
	}
	_, err := w.Write(out)
	return err
}

func (s *dnsTCPServer) handle(client net.Conn) {
	defer client.Close()

	stopOnShutdown := context.AfterFunc(s.ctx, func() { _ = client.Close() })
	defer stopOnShutdown()

	clientIP := net.IP(nil)
	clientPort := 0
	if ta, ok := client.RemoteAddr().(*net.TCPAddr); ok {
		clientIP = ta.IP
		clientPort = ta.Port
	}
	origIP, origPort, origErr := originalDst(client)
	if origErr != nil {
		log.Tracef("DNS TCP: original destination unavailable for %s: %v", client.RemoteAddr(), origErr)
	}

	srcMac := ""
	if clientIP != nil {
		srcMac = s.worker.getMacByIp(clientIP.String())
	}

	for {
		cfg := s.worker.getConfig()
		ioTimeout := cfg.DNSTCPIOTimeout()

		_ = client.SetReadDeadline(time.Now().Add(cfg.DNSTCPIdleTimeout()))
		query, err := readDNSTCPMessage(client)
		if err != nil {
			return
		}
		if len(query) < dnsTCPMinQueryLen {
			return
		}

		domain, ok := dns.ParseQueryDomain(query)
		if !ok {
			s.passthrough(client, origIP, origPort, origErr, query)
			return
		}
		domain = strings.ToLower(domain)

		matched, set := s.worker.getMatcher().MatchSNIWithSource(domain, srcMac)
		if !matched {
			log.Tracef("DNS TCP: %s matched no set (src %s), forwarding unchanged", domain, srcMac)
			s.passthrough(client, origIP, origPort, origErr, query)
			return
		}

		if set.Routing.Enabled && config.RoutingIsBlock(set.Routing.Mode) && !cfg.Queue.IsDiscovery {
			if resp := dns.BuildBlockResponse(query); resp != nil {
				s.logEvent(set, domain, clientIP, origIP, clientPort, srcMac, dnsActionSinkhole)
				metrics.GetMetricsCollector().RecordBlock(domain, srcMac)
				if writeDNSTCPMessage(client, resp, ioTimeout) != nil {
					return
				}
				continue
			}
			s.logEvent(set, domain, clientIP, origIP, clientPort, srcMac, dnsActionBlock)
			return
		}

		useDoH := set.DNS.DoHURL != ""
		if !(set.DNS.Enabled && (set.DNS.TargetDNS != "" || useDoH)) {
			s.logEvent(set, domain, clientIP, origIP, clientPort, srcMac, dnsActionPassthrough)
			s.passthrough(client, origIP, origPort, origErr, query)
			return
		}

		var targetIP net.IP
		if !useDoH {
			targetIP = net.ParseIP(set.DNS.TargetDNS)
			if targetIP == nil {
				s.logEvent(set, domain, clientIP, origIP, clientPort, srcMac, dnsActionBadTarget)
				s.passthrough(client, origIP, origPort, origErr, query)
				return
			}
		}

		s.logEvent(set, domain, clientIP, origIP, clientPort, srcMac, dnsRedirectAction(set))

		resp, rerr := s.resolve(set, cfg, query, targetIP)
		if rerr != nil {
			s.logEvent(set, domain, clientIP, origIP, clientPort, srcMac, dnsActionServfail)
			resp = dns.BuildServfailResponse(query)
			if len(resp) == 0 {
				return
			}
		}

		if ips := dns.ParseResponseIPs(resp); len(ips) > 0 && clientIP != nil {
			s.worker.storeHostHints(clientIP, set, domain, ips)
			if set.Routing.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery && RoutingHandleDNSFunc != nil {
				RoutingHandleDNSFunc(cfg, set, ips)
			}
		}

		if healed := s.worker.healDNSResponse(cfg, set, domain, resp); healed != nil {
			s.logEvent(set, domain, clientIP, origIP, clientPort, srcMac, dnsActionHeal)
			resp = healed
		}

		if writeDNSTCPMessage(client, resp, ioTimeout) != nil {
			return
		}
	}
}

func (s *dnsTCPServer) resolve(set *config.SetConfig, cfg *config.Config, query []byte, targetIP net.IP) ([]byte, error) {
	if set.DNS.DoHURL != "" {
		return s.worker.resolveDoHRedirect(set.DNS.DoHURL, int(cfg.MainInjectedMark()), query)
	}
	return dns.ResolveUpstream(query, targetIP, dns.ForwardOptions{
		Sender:       s.worker.sock,
		Fragment:     set.DNS.FragmentQuery,
		Seg2Delay:    config.ResolveSeg2Delay(set.UDP.Seg2Delay, set.UDP.Seg2DelayMax),
		ReverseOrder: set.Fragmentation.ReverseOrder,
		Mark:         int(cfg.MainInjectedMark()),
	})
}

func (s *dnsTCPServer) logEvent(set *config.SetConfig, domain string, clientIP, serverIP net.IP, clientPort int, srcMac, action string) {
	logDNSEvent("TCP", set, domain, clientIP, serverIP, uint16(clientPort), srcMac, action)
}

func (s *dnsTCPServer) passthrough(client net.Conn, origIP net.IP, origPort int, origErr error, firstQuery []byte) {
	if origErr != nil || origIP == nil || origPort == 0 || origPort == s.port {
		return
	}
	cfg := s.worker.getConfig()
	d := net.Dialer{Timeout: cfg.DNSTCPDialTimeout()}
	socks5.ApplyBypassMark(&d, uint32(cfg.MainInjectedMark()))

	upstream, err := d.DialContext(s.ctx, "tcp", net.JoinHostPort(origIP.String(), fmt.Sprintf("%d", origPort)))
	if err != nil {
		log.Tracef("DNS TCP: passthrough dial %s:%d failed: %v", origIP, origPort, err)
		return
	}
	defer upstream.Close()

	if writeDNSTCPMessage(upstream, firstQuery, cfg.DNSTCPIOTimeout()) != nil {
		return
	}

	done := make(chan struct{}, 2)
	go func() {
		_ = client.SetReadDeadline(time.Time{})
		_, _ = io.Copy(upstream, client)
		if tc, ok := upstream.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_ = upstream.SetReadDeadline(time.Time{})
		_, _ = io.Copy(client, upstream)
		if tc, ok := client.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()

	select {
	case <-done:
	case <-s.ctx.Done():
	}
}
