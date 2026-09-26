package config

import (
	"fmt"
	"net/netip"
	"sort"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

const (
	TelegramBridgeSetID   = "telegram-bridge"
	TelegramBridgeSetName = "Telegram bridge"
	TelegramBridgeMark    = uint32(0x24BAB)
)

const (
	TelegramCIDRSourceTelegram = "telegram"
	TelegramCIDRSourceMirror   = "mirror"
	TelegramCIDRSourceCache    = "cache"
	TelegramCIDRSourceGeoIP    = "geoip"
	TelegramCIDRSourceBuiltin  = "builtin"
)

const (
	telegramMinPrefixV4 = 12
	telegramMinPrefixV6 = 24
)

var TelegramBuiltinCIDRs = []string{
	"91.105.192.0/23",
	"91.108.4.0/22",
	"91.108.8.0/22",
	"91.108.12.0/22",
	"91.108.16.0/22",
	"91.108.20.0/22",
	"91.108.56.0/22",
	"95.161.64.0/20",
	"149.154.160.0/20",
	"185.76.151.0/24",
	"2001:67c:4e8::/48",
	"2001:b28:f23c::/48",
	"2001:b28:f23d::/48",
	"2001:b28:f23f::/48",
	"2a0a:f280::/32",
}

type TelegramCIDRs struct {
	Source    string
	UpdatedAt time.Time
	Primary   []string
	All       []string
	V4        int
	V6        int
}

var telegramCIDRs atomic.Pointer[TelegramCIDRs]

var builtinTelegramCIDRs = buildTelegramCIDRs(TelegramCIDRSourceBuiltin, time.Time{}, nil)

func CurrentTelegramCIDRs() *TelegramCIDRs {
	if cur := telegramCIDRs.Load(); cur != nil {
		return cur
	}
	return builtinTelegramCIDRs
}

func SetTelegramCIDRs(source string, updatedAt time.Time, primary []string) (*TelegramCIDRs, bool) {
	next := buildTelegramCIDRs(source, updatedAt, primary)
	prev := CurrentTelegramCIDRs()
	telegramCIDRs.Store(next)
	return next, !sameStrings(prev.All, next.All)
}

func buildTelegramCIDRs(source string, updatedAt time.Time, primary []string) *TelegramCIDRs {
	clean := NormalizeTelegramCIDRs(primary)
	seen := make(map[string]struct{}, len(clean)+len(TelegramBuiltinCIDRs))
	var all []string
	for _, list := range [][]string{clean, TelegramBuiltinCIDRs} {
		for _, p := range list {
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			all = append(all, p)
		}
	}
	sort.Strings(all)
	out := &TelegramCIDRs{Source: source, UpdatedAt: updatedAt, Primary: clean, All: all}
	for _, p := range all {
		if netip.MustParsePrefix(p).Addr().Is4() {
			out.V4++
		} else {
			out.V6++
		}
	}
	return out
}

func NormalizeTelegramCIDRs(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	var out []string
	for _, entry := range raw {
		p, ok := parseTelegramPrefix(entry)
		if !ok {
			continue
		}
		s := p.String()
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseTelegramPrefix(entry string) (netip.Prefix, bool) {
	p, err := netip.ParsePrefix(entry)
	if err != nil {
		addr, aerr := netip.ParseAddr(entry)
		if aerr != nil {
			return netip.Prefix{}, false
		}
		p = netip.PrefixFrom(addr, addr.BitLen())
	}
	p = p.Masked()
	if p.Addr().Is4In6() {
		return netip.Prefix{}, false
	}
	if p.Addr().Is4() && p.Bits() < telegramMinPrefixV4 {
		return netip.Prefix{}, false
	}
	if p.Addr().Is6() && p.Bits() < telegramMinPrefixV6 {
		return netip.Prefix{}, false
	}
	return p, true
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (c *Config) TelegramBridgeEnabled() bool {
	return c != nil && c.System.MTProto.Bridge.Enabled
}

func (c *Config) TelegramInUse() bool {
	if c == nil {
		return false
	}
	if c.System.MTProto.Enabled || c.System.MTProto.Bridge.Enabled {
		return true
	}
	for _, set := range c.Sets {
		if set != nil && set.Enabled && set.Routing.Enabled && set.Routing.Mode == RoutingModeMTProtoWS {
			return true
		}
	}
	return false
}

func (c *Config) TelegramBridgeSet() *SetConfig {
	set := NewSetConfig()
	set.Id = TelegramBridgeSetID
	set.Name = TelegramBridgeSetName
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = RoutingModeMTProtoWS
	set.Routing.FWMark = TelegramBridgeMark
	set.Targets.IpsToMatch = CurrentTelegramCIDRs().All
	return &set
}

func (c *Config) RoutingSets() []*SetConfig {
	if !c.TelegramBridgeEnabled() {
		return c.Sets
	}
	out := make([]*SetConfig, 0, len(c.Sets)+1)
	out = append(out, c.TelegramBridgeSet())
	return append(out, c.Sets...)
}

func IsTelegramBridgeMark(mark uint32) bool {
	return mark&PerSetRouteMarkBits == TelegramBridgeMark
}

func IsTelegramBridgeSet(set *SetConfig) bool {
	return set != nil && set.Id == TelegramBridgeSetID
}

func (c *Config) releaseTelegramBridgeReservations() {
	taken := make(map[string]struct{}, len(c.Sets))
	for _, set := range c.Sets {
		if set != nil {
			taken[set.Id] = struct{}{}
		}
	}
	for _, set := range c.Sets {
		if set == nil {
			continue
		}
		if set.Id == TelegramBridgeSetID {
			newID := ""
			for n := 0; newID == ""; n++ {
				candidate := uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("b4-telegram-bridge-user:%s:%d", set.Name, n))).String()
				if _, dup := taken[candidate]; !dup {
					newID = candidate
				}
			}
			taken[newID] = struct{}{}
			for _, other := range c.Sets {
				if other != nil && other.Escalate.To == TelegramBridgeSetID {
					other.Escalate.To = newID
				}
			}
			log.Warnf("Set '%s' carried the id reserved for the Telegram bridge, it was given the id %s", set.Name, newID)
			set.Id = newID
		}
		if set.Routing.FWMark != 0 && IsTelegramBridgeMark(set.Routing.FWMark) {
			log.Warnf("Set '%s' pinned the routing mark 0x%x reserved for the Telegram bridge, the pin was removed", set.Name, set.Routing.FWMark)
			set.Routing.FWMark = 0
		}
	}
}
