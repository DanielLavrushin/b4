package warmtuple

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func routeFingerprint(ctx context.Context, target netip.AddrPort, source netip.Addr) (string, error) {
	warm, err := markedRoute(ctx, target.Addr(), source, config.SelfDialMark)
	if err != nil {
		return "", err
	}
	pass, err := markedRoute(ctx, target.Addr(), source, config.SelfDialRelayMark)
	if err != nil {
		return "", err
	}
	if warm != pass {
		return "", errors.New("warm and pass marks select different routes")
	}
	return warm, nil
}

func markedRoute(ctx context.Context, target, source netip.Addr, mark uint32) (string, error) {
	c, err := netlink.Dial(unix.NETLINK_ROUTE, nil)
	if err != nil {
		return "", err
	}
	defer c.Close()
	deadline := time.Now().Add(time.Second)
	if until, ok := ctx.Deadline(); ok && until.Before(deadline) {
		deadline = until
	}
	if err := c.SetDeadline(deadline); err != nil {
		return "", err
	}
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	defer stop()
	family := byte(unix.AF_INET)
	if !target.Is4() {
		family = unix.AF_INET6
	}
	payload := []byte{family, byte(target.BitLen()), byte(source.BitLen()), 0, 0, 0, 0, 0, 0, 0, 0, 0}
	attrs := netlink.NewAttributeEncoder()
	attrs.Bytes(unix.RTA_DST, target.AsSlice())
	attrs.Bytes(unix.RTA_SRC, source.AsSlice())
	attrs.Uint32(unix.RTA_MARK, mark)
	encoded, err := attrs.Encode()
	if err != nil {
		return "", err
	}
	messages, err := c.Execute(netlink.Message{Header: netlink.Header{Type: unix.RTM_GETROUTE, Flags: netlink.Request}, Data: append(payload, encoded...)})
	if err != nil {
		return "", err
	}
	for _, message := range messages {
		if len(message.Data) < 12 {
			continue
		}
		table := uint32(message.Data[4])
		device := uint32(0)
		gateway := ""
		decoder, err := netlink.NewAttributeDecoder(message.Data[12:])
		if err != nil {
			return "", err
		}
		for decoder.Next() {
			switch decoder.Type() {
			case unix.RTA_TABLE:
				table = decoder.Uint32()
			case unix.RTA_OIF:
				device = decoder.Uint32()
			case unix.RTA_GATEWAY:
				gateway = fmt.Sprintf("%x", decoder.Bytes())
			}
		}
		if err := decoder.Err(); err != nil {
			return "", err
		}
		if device != 0 {
			return fmt.Sprintf("%d/%d/%s", table, device, gateway), nil
		}
	}
	return "", errors.New("no egress route for warm tuple")
}
