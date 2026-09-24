package config

import (
	"reflect"
	"testing"
)

func adoptTarget() SetConfig {
	set := NewSetConfig()
	set.Id, set.Name, set.Enabled = "set-yt", "YouTube", false
	set.Targets.SNIDomains = []string{"youtube.com"}
	set.Targets.GeoSiteCategories = []string{"youtube"}
	set.Targets.TLSVersion = "1.3"
	set.Targets.IPVersion = "4"
	set.Routing.Enabled = true
	set.Routing.EgressInterface = "wg0"
	set.Escalate.To = "set-next"
	set.MSSClamp = MSSClampConfig{Enabled: true, Size: 88}
	set.Hub = &HubOrigin{ID: "hub-1", Version: 3}
	set.Discovery.URLs = []string{"https://www.youtube.com/"}
	set.TCP.DPortFilter = "443,8443"
	set.TCP.RSTProtection = RSTProtectionConfig{Enabled: true, TTLTolerance: 5}
	set.TCP.IPBlockDetect = IPBlockDetectConfig{Enabled: true, RetransmitThreshold: 9, HealDNS: true}
	set.TCP.ConnBytesLimit = 3
	set.UDP.Mode = "drop"
	set.UDP.DPortFilter = "443"
	set.Fragmentation.Strategy = "oob"
	set.Faking.Strategy = "timestamp"
	set.DNS = DNSConfig{Enabled: true, TargetDNS: "1.1.1.1", Pins: map[string][]string{"youtube.com": {"8.8.8.8"}}}
	return set
}

func adoptStrategy() SetConfig {
	set := NewSetConfig()
	set.Id, set.Name, set.Enabled = "preset", "preset", true
	set.Targets.SNIDomains = []string{"www.youtube.com"}
	set.Targets.TLSVersion = "1.2"
	set.TCP.ConnBytesLimit = 7
	set.TCP.DPortFilter = "80"
	set.TCP.RSTProtection = RSTProtectionConfig{Enabled: false, TTLTolerance: 1}
	set.TCP.IPBlockDetect = IPBlockDetectConfig{Enabled: false, RetransmitThreshold: 1}
	set.TCP.Win = WinConfig{Mode: "oscillate", Values: []int{1, 2, 3}}
	set.TCP.Desync = DesyncConfig{Mode: "rst", TTL: 3, Count: 2}
	set.UDP.Mode = "fake"
	set.UDP.DPortFilter = "53"
	set.UDP.FakePayloadData = []byte{1, 2}
	set.Fragmentation.Strategy = "tcp"
	set.Fragmentation.StrategyPool = []string{"tcp", "tls"}
	set.Fragmentation.SeqOverlapPattern = []string{"0x16"}
	set.Fragmentation.SeqOverlapBytes = []byte{0x16}
	set.Faking.Strategy = "pastseq"
	set.Faking.PayloadData = []byte{9, 9, 9}
	set.Faking.TLSMod = []string{"rnd"}
	set.Faking.SNIMutation.FakeSNIs = []string{"ya.ru"}
	set.DNS = DNSConfig{Enabled: false, DoHURL: "https://dns.example/dns-query", Pins: map[string][]string{"www.youtube.com": {"9.9.9.9"}}}
	set.MSSClamp = MSSClampConfig{Enabled: false}
	set.Discovery.URLs = []string{"https://other.example/"}
	return set
}

func TestAdoptStrategy(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(from *SetConfig)
		want   func(dst, from SetConfig) SetConfig
	}{
		{
			name: "strategy fields come from the discovered set, the set keeps its own",
			want: func(dst, from SetConfig) SetConfig {
				want := dst
				want.TCP = from.TCP
				want.TCP.DPortFilter = dst.TCP.DPortFilter
				want.TCP.RSTProtection = dst.TCP.RSTProtection
				want.TCP.IPBlockDetect = dst.TCP.IPBlockDetect
				want.Fragmentation = from.Fragmentation
				want.Faking = from.Faking
				return want
			},
		},
		{
			name: "an enabled ip block detection comes along",
			mutate: func(from *SetConfig) {
				from.TCP.IPBlockDetect = IPBlockDetectConfig{Enabled: true, RetransmitThreshold: 4, SynDetect: true}
			},
			want: func(dst, from SetConfig) SetConfig {
				want := dst
				want.TCP = from.TCP
				want.TCP.DPortFilter = dst.TCP.DPortFilter
				want.TCP.RSTProtection = dst.TCP.RSTProtection
				want.Fragmentation = from.Fragmentation
				want.Faking = from.Faking
				return want
			},
		},
		{
			name: "an enabled DNS block comes along without its pins",
			mutate: func(from *SetConfig) {
				from.DNS.Enabled = true
				from.DNS.FragmentQuery = true
			},
			want: func(dst, from SetConfig) SetConfig {
				want := dst
				want.TCP = from.TCP
				want.TCP.DPortFilter = dst.TCP.DPortFilter
				want.TCP.RSTProtection = dst.TCP.RSTProtection
				want.TCP.IPBlockDetect = dst.TCP.IPBlockDetect
				want.Fragmentation = from.Fragmentation
				want.Faking = from.Faking
				want.DNS = from.DNS
				want.DNS.Pins = dst.DNS.Pins
				return want
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dst, from := adoptTarget(), adoptStrategy()
			if c.mutate != nil {
				c.mutate(&from)
			}
			want := c.want(adoptTarget(), from)

			dst.AdoptStrategy(&from)

			if !reflect.DeepEqual(dst, want) {
				t.Fatalf("adopted set differs\n got: %+v\nwant: %+v", dst, want)
			}
			if dst.Id != "set-yt" || dst.Name != "YouTube" || dst.Enabled {
				t.Errorf("identity must not move: id=%q name=%q enabled=%v", dst.Id, dst.Name, dst.Enabled)
			}
			if dst.UDP.Mode != "drop" || dst.UDP.DPortFilter != "443" || dst.UDP.FakePayloadData != nil {
				t.Errorf("UDP is not part of the adopted strategy: %+v", dst.UDP)
			}
			if len(dst.Targets.SNIDomains) != 1 || dst.Targets.SNIDomains[0] != "youtube.com" || dst.Targets.TLSVersion != "1.3" {
				t.Errorf("targets must not move: %+v", dst.Targets)
			}
			if !dst.Routing.Enabled || dst.Routing.EgressInterface != "wg0" || dst.Escalate.To != "set-next" {
				t.Errorf("routing and escalation must not move: %+v %+v", dst.Routing, dst.Escalate)
			}
			if !dst.MSSClamp.Enabled || dst.MSSClamp.Size != 88 {
				t.Errorf("MSS clamp must not move: %+v", dst.MSSClamp)
			}
			if dst.Hub == nil || dst.Hub.ID != "hub-1" {
				t.Errorf("hub origin must not move: %+v", dst.Hub)
			}
			if len(dst.Discovery.URLs) != 1 || dst.Discovery.URLs[0] != "https://www.youtube.com/" {
				t.Errorf("discovery URLs must not move: %v", dst.Discovery.URLs)
			}
			if dst.TCP.DPortFilter != "443,8443" || !dst.TCP.RSTProtection.Enabled {
				t.Errorf("the set's port filter and RST protection are its own: %q %+v", dst.TCP.DPortFilter, dst.TCP.RSTProtection)
			}
			if pins := dst.DNS.Pins; len(pins) != 1 || pins["youtube.com"][0] != "8.8.8.8" {
				t.Errorf("the set keeps its own DNS pins, got %v", pins)
			}
			if dst.TCP.ConnBytesLimit != 7 || dst.Fragmentation.Strategy != "tcp" || dst.Faking.Strategy != "pastseq" {
				t.Errorf("the strategy did not move: conn=%d frag=%q fake=%q", dst.TCP.ConnBytesLimit, dst.Fragmentation.Strategy, dst.Faking.Strategy)
			}
		})
	}
}

func TestAdoptStrategySharesNothingWithTheSource(t *testing.T) {
	dst, from := adoptTarget(), adoptStrategy()
	from.TCP.IPBlockDetect.Enabled = true
	from.DNS.Enabled = true
	dst.AdoptStrategy(&from)

	if _, shared := sharedBacking(reflect.ValueOf(from), reflect.ValueOf(from), "set"); !shared {
		t.Fatal("the sharing probe must see a set sharing everything with itself")
	}
	if path, shared := sharedBacking(reflect.ValueOf(dst), reflect.ValueOf(from), "set"); shared {
		t.Fatalf("%s shares its backing store with the source set", path)
	}

	before := adoptStrategy()
	from.TCP.Win.Values[0] = 999
	from.Fragmentation.StrategyPool[0] = "mutated"
	from.Fragmentation.SeqOverlapPattern[0] = "mutated"
	from.Fragmentation.SeqOverlapBytes[0] = 0xff
	from.Faking.PayloadData[0] = 0xff
	from.Faking.TLSMod[0] = "mutated"
	from.Faking.SNIMutation.FakeSNIs[0] = "mutated"
	from.DNS.Pins["www.youtube.com"][0] = "0.0.0.0"
	from.TCP.ConnBytesLimit = 1

	if !reflect.DeepEqual(dst.TCP.Win.Values, before.TCP.Win.Values) ||
		!reflect.DeepEqual(dst.Fragmentation.StrategyPool, before.Fragmentation.StrategyPool) ||
		!reflect.DeepEqual(dst.Fragmentation.SeqOverlapPattern, before.Fragmentation.SeqOverlapPattern) ||
		!reflect.DeepEqual(dst.Fragmentation.SeqOverlapBytes, before.Fragmentation.SeqOverlapBytes) ||
		!reflect.DeepEqual(dst.Faking.PayloadData, before.Faking.PayloadData) ||
		!reflect.DeepEqual(dst.Faking.TLSMod, before.Faking.TLSMod) ||
		!reflect.DeepEqual(dst.Faking.SNIMutation.FakeSNIs, before.Faking.SNIMutation.FakeSNIs) ||
		dst.TCP.ConnBytesLimit != 7 {
		t.Fatalf("mutating the source changed the adopted set: %+v", dst)
	}
	if pins := dst.DNS.Pins; len(pins) != 1 || pins["youtube.com"][0] != "8.8.8.8" {
		t.Errorf("the set's own pins must survive, got %v", pins)
	}
}

func TestAdoptStrategyKeepsNilSlicesNil(t *testing.T) {
	dst, from := adoptTarget(), adoptStrategy()
	from.Faking.PayloadData = nil
	from.Fragmentation.StrategyPool = []string{}
	dst.AdoptStrategy(&from)
	if dst.Faking.PayloadData != nil {
		t.Error("a nil slice must stay nil")
	}
	if dst.Fragmentation.StrategyPool == nil {
		t.Error("an empty slice must stay empty, not nil, so the JSON keeps []")
	}
}

func sharedBacking(a, b reflect.Value, path string) (string, bool) {
	switch a.Kind() {
	case reflect.Struct:
		for i := 0; i < a.NumField(); i++ {
			if !a.Type().Field(i).IsExported() {
				continue
			}
			if p, shared := sharedBacking(a.Field(i), b.Field(i), path+"."+a.Type().Field(i).Name); shared {
				return p, true
			}
		}
	case reflect.Slice:
		if a.Len() > 0 && b.Len() > 0 && a.Pointer() == b.Pointer() {
			return path, true
		}
	case reflect.Map:
		if !a.IsNil() && !b.IsNil() && a.Pointer() == b.Pointer() {
			return path, true
		}
		if a.IsNil() || b.IsNil() {
			return "", false
		}
		for _, key := range a.MapKeys() {
			bv := b.MapIndex(key)
			if !bv.IsValid() {
				continue
			}
			if p, shared := sharedBacking(a.MapIndex(key), bv, path+"["+key.String()+"]"); shared {
				return p, true
			}
		}
	case reflect.Ptr:
		if !a.IsNil() && !b.IsNil() && a.Pointer() == b.Pointer() {
			return path, true
		}
	}
	return "", false
}
