package dns

import (
	"net"
	"strings"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

const pinTestTTL = 60

func pinnedNames(names ...string) func(string) bool {
	return func(name string) bool {
		n := strings.ToLower(strings.TrimSuffix(name, "."))
		for _, p := range names {
			if n == p || strings.HasSuffix(n, "."+p) {
				return true
			}
		}
		return false
	}
}

func aliasTestReply(t *testing.T, qname string, qtype dnsmessage.Type, h dnsmessage.Header, fill func(b *dnsmessage.Builder) error) []byte {
	t.Helper()
	h.ID = 0x4321
	h.Response = true
	h.RecursionDesired = true
	h.RecursionAvailable = true
	return builtMessage(t, qname, qtype, h, fill)
}

func cnameRR(b *dnsmessage.Builder, owner, target string, ttl uint32) error {
	h := rrHeader(owner, dnsmessage.TypeCNAME)
	h.TTL = ttl
	return b.CNAMEResource(h, dnsmessage.CNAMEResource{CNAME: dnsmessage.MustNewName(target)})
}

func soaRR(b *dnsmessage.Builder, zone string) error {
	return b.SOAResource(rrHeader(zone, dnsmessage.TypeSOA), dnsmessage.SOAResource{
		NS:      dnsmessage.MustNewName("a.ns." + zone),
		MBox:    dnsmessage.MustNewName("dns.facebook.com."),
		Serial:  4207849484,
		Refresh: 14400,
		Retry:   1800,
		Expire:  604800,
		MinTTL:  3600,
	})
}

func optRR(b *dnsmessage.Builder, dnssecOK bool) error {
	var h dnsmessage.ResourceHeader
	if err := h.SetEDNS0(1232, dnsmessage.RCodeSuccess, dnssecOK); err != nil {
		return err
	}
	return b.OPTResource(h, dnsmessage.OPTResource{})
}

func emptyAnswerFor(t *testing.T, out []byte, qname string, qtype dnsmessage.Type) dnsmessage.Header {
	t.Helper()
	if out == nil {
		t.Fatal("the answer still carries the pinned name's alias, a caching resolver keeps it and bypasses the pin")
	}
	h, an, ns, ar := parsedMessage(t, out)
	if !h.Response || h.ID != 0x4321 || h.RCode != dnsmessage.RCodeSuccess || h.Truncated || h.AuthenticData {
		t.Fatalf("header %+v, want a complete NOERROR response with the reply's id and no AD", h)
	}
	if len(an) != 0 || len(ns) != 0 || len(ar) != 0 {
		t.Fatalf("%d answers, %d authorities and %d additionals, want an empty answer", len(an), len(ns), len(ar))
	}
	var p dnsmessage.Parser
	if _, err := p.Start(out); err != nil {
		t.Fatal(err)
	}
	q, err := p.Question()
	if err != nil {
		t.Fatal(err)
	}
	if q.Name.String() != qname || q.Type != qtype {
		t.Fatalf("question %s %v, want the reply's %s %v", q.Name, q.Type, qname, qtype)
	}
	return h
}

func TestWithoutPinnedAliasEmptiesAnMXAnswerCarryingTheAlias(t *testing.T) {
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeMX, dnsmessage.Header{Authoritative: true}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 2442); err != nil {
			return err
		}
		if err := b.StartAuthorities(); err != nil {
			return err
		}
		if err := soaRR(b, "instagram.com."); err != nil {
			return err
		}
		if err := b.StartAdditionals(); err != nil {
			return err
		}
		return optRR(b, false)
	})

	out := WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL)

	if h := emptyAnswerFor(t, out, "www.instagram.com.", dnsmessage.TypeMX); !h.Authoritative {
		t.Error("the AA bit of the reply was lost")
	}
}

func TestWithoutPinnedAliasEmptiesATXTAnswerOfTheAliasTarget(t *testing.T) {
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeTXT, dnsmessage.Header{Truncated: true}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 2132); err != nil {
			return err
		}
		return b.TXTResource(rrHeader("z-p42-instagram.c10r.instagram.com.", dnsmessage.TypeTXT), dnsmessage.TXTResource{TXT: []string{"v=spf1 a ~all"}})
	})

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL), "www.instagram.com.", dnsmessage.TypeTXT)
}

func TestWithoutPinnedAliasHandlesASignedAnswer(t *testing.T) {
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeMX, dnsmessage.Header{AuthenticData: true}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 3600); err != nil {
			return err
		}
		if err := b.UnknownResource(rrHeader("www.instagram.com.", dnsmessage.Type(rrTypeRRSIG)), dnsmessage.UnknownResource{Type: dnsmessage.Type(rrTypeRRSIG), Data: rrsigRData(rrTypeCNAME, "instagram.com")}); err != nil {
			return err
		}
		if err := b.StartAuthorities(); err != nil {
			return err
		}
		if err := soaRR(b, "instagram.com."); err != nil {
			return err
		}
		if err := b.StartAdditionals(); err != nil {
			return err
		}
		return optRR(b, true)
	})

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL), "www.instagram.com.", dnsmessage.TypeMX)
}

func TestWithoutPinnedAliasMatchesNamesWhateverTheirCase(t *testing.T) {
	resp := aliasTestReply(t, "wWw.InStAgRaM.cOm.", dnsmessage.TypeMX, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		return cnameRR(b, "WWW.INSTAGRAM.COM.", "z-p42-instagram.c10r.instagram.com.", 3600)
	})

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL), "wWw.InStAgRaM.cOm.", dnsmessage.TypeMX)
}

func TestWithoutPinnedAliasCatchesADNAMEOfAPinnedName(t *testing.T) {
	target := encodeName("instagram.example.net")
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeTXT, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		return b.UnknownResource(rrHeader("instagram.com.", dnsmessage.Type(rrTypeDNAME)), dnsmessage.UnknownResource{Type: dnsmessage.Type(rrTypeDNAME), Data: target})
	})

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("instagram.com"), pinTestTTL), "www.instagram.com.", dnsmessage.TypeTXT)
}

func TestWithoutPinnedAliasKeepsTheAddressesUnderTheAskedName(t *testing.T) {
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeA, dnsmessage.Header{AuthenticData: true}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 2377); err != nil {
			return err
		}
		h := rrHeader("z-p42-instagram.c10r.instagram.com.", dnsmessage.TypeA)
		h.TTL = 46
		if err := b.AResource(h, dnsmessage.AResource{A: [4]byte{31, 13, 72, 174}}); err != nil {
			return err
		}
		if err := b.StartAdditionals(); err != nil {
			return err
		}
		return optRR(b, false)
	})

	out := WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL)
	if out == nil {
		t.Fatal("an address answer for a pinned name still carries its alias")
	}
	h, an, ns, _ := parsedMessage(t, out)
	if h.AuthenticData || h.RCode != dnsmessage.RCodeSuccess || len(ns) != 0 {
		t.Fatalf("header %+v with %d authorities, want NOERROR without AD", h, len(ns))
	}
	if len(an) != 1 || an[0].Header.Type != dnsmessage.TypeA || an[0].Header.Name.String() != "www.instagram.com." || an[0].Header.TTL != pinTestTTL {
		t.Fatalf("answers %+v, want one A record owned by the asked name with the pin TTL", an)
	}
	if got := net.IP(an[0].Body.(*dnsmessage.AResource).A[:]).String(); got != "31.13.72.174" {
		t.Fatalf("address %s, want the resolver's 31.13.72.174", got)
	}
}

func TestWithoutPinnedAliasEmptiesAnAliasToANameThatDoesNotExist(t *testing.T) {
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeMX, dnsmessage.Header{RCode: dnsmessage.RCodeNameError}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "gone.c10r.instagram.com.", 3600); err != nil {
			return err
		}
		if err := b.StartAuthorities(); err != nil {
			return err
		}
		return soaRR(b, "instagram.com.")
	})

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL), "www.instagram.com.", dnsmessage.TypeMX)
}

func TestWithoutPinnedAliasPassesAnAddressAnswerThatCarriesNoAddress(t *testing.T) {
	for _, h := range []dnsmessage.Header{{Authoritative: true}, {Truncated: true}, {RCode: dnsmessage.RCodeNameError}} {
		resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeA, h, func(b *dnsmessage.Builder) error {
			if err := b.StartAnswers(); err != nil {
				return err
			}
			return cnameRR(b, "www.instagram.com.", "edge.cdn.example.net.", 3600)
		})
		if out := WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL); out != nil {
			t.Errorf("header %+v: an A answer with the alias and no address was rewritten, a recursive resolver could no longer follow the alias to the site's addresses", h)
		}
	}
}

func TestWithoutPinnedAliasLeavesOtherAnswersAlone(t *testing.T) {
	apexMX := aliasTestReply(t, "instagram.com.", dnsmessage.TypeMX, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		return b.MXResource(rrHeader("instagram.com.", dnsmessage.TypeMX), dnsmessage.MXResource{Pref: 10, MX: dnsmessage.MustNewName("mxa.instagram.com.")})
	})
	aliasOf := func(qname string) []byte {
		return aliasTestReply(t, qname, dnsmessage.TypeMX, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
			if err := b.StartAnswers(); err != nil {
				return err
			}
			return cnameRR(b, qname, "z-p42-instagram.c10r.instagram.com.", 3600)
		})
	}
	foreignAlias := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeMX, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		return cnameRR(b, "elsewhere.example.net.", "z-p42-instagram.c10r.instagram.com.", 3600)
	})
	servfail := aliasOf("www.instagram.com.")
	servfail[3] = (servfail[3] &^ 0x0F) | byte(dnsmessage.RCodeServerFailure)
	query := aliasOf("www.instagram.com.")
	query[2] &^= 0x80
	notify := aliasOf("www.instagram.com.")
	notify[2] |= 4 << 3
	cut := aliasOf("www.instagram.com.")
	cut = cut[:len(cut)-3]

	for _, tc := range []struct {
		name string
		resp []byte
	}{
		{"the MX of a pinned name that is no alias", apexMX},
		{"an alias of a name no pin covers", aliasOf("www.facebook.com.")},
		{"an alias another name owns", foreignAlias},
		{"a SERVFAIL", servfail},
		{"a query", query},
		{"another opcode", notify},
		{"a truncated message", cut},
	} {
		if out := WithoutPinnedAlias(tc.resp, pinnedNames("instagram.com"), pinTestTTL); out != nil {
			t.Errorf("%s: rewritten, want it passed unchanged", tc.name)
		}
	}
	if out := WithoutPinnedAlias(aliasOf("www.instagram.com."), nil, pinTestTTL); out != nil {
		t.Error("rewritten without a pin list")
	}
}
