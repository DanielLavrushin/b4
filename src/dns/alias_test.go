package dns

import (
	"bytes"
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

func optRR(b *dnsmessage.Builder, dnssecOK bool, options ...dnsmessage.Option) error {
	var h dnsmessage.ResourceHeader
	if err := h.SetEDNS0(1232, dnsmessage.RCodeSuccess, dnssecOK); err != nil {
		return err
	}
	return b.OPTResource(h, dnsmessage.OPTResource{Options: options})
}

func rewrittenFor(t *testing.T, out []byte, qname string, qtype dnsmessage.Type) (dnsmessage.Header, []dnsmessage.Resource, []dnsmessage.Resource) {
	t.Helper()
	if out == nil {
		t.Fatal("the answer still carries the pinned name's alias, a caching resolver keeps it and bypasses the pin")
	}
	h, an, ns, ar := parsedMessage(t, out)
	if !h.Response || h.ID != 0x4321 || h.RCode != dnsmessage.RCodeSuccess || h.AuthenticData {
		t.Fatalf("header %+v, want a NOERROR response with the reply's id and no AD", h)
	}
	if len(ns) != 0 {
		t.Fatalf("%d authority records, want none", len(ns))
	}
	for _, rr := range an {
		if rr.Header.Type == dnsmessage.TypeCNAME || rr.Header.Type == dnsmessage.Type(rrTypeDNAME) || rr.Header.Type == dnsmessage.Type(rrTypeRRSIG) {
			t.Fatalf("answer %v kept, want only records of the asked type", rr.Header)
		}
		if rr.Header.Name.String() != qname || rr.Header.Type != qtype {
			t.Fatalf("answer %v, want records of type %v owned by the asked name %s", rr.Header, qtype, qname)
		}
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
	return h, an, ar
}

func emptyAnswerFor(t *testing.T, out []byte, qname string, qtype dnsmessage.Type, wantOPT int) dnsmessage.Header {
	t.Helper()
	h, an, ar := rewrittenFor(t, out, qname, qtype)
	if len(an) != 0 || len(ar) != wantOPT {
		t.Fatalf("%d answers and %d additionals, want an empty answer carrying %d OPT records", len(an), len(ar), wantOPT)
	}
	return h
}

func TestWithoutPinnedAliasEmptiesAnMXAnswerCarryingTheAlias(t *testing.T) {
	cookie := dnsmessage.Option{Code: 10, Data: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}}
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
		return optRR(b, false, cookie)
	})

	out := WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0)

	if h := emptyAnswerFor(t, out, "www.instagram.com.", dnsmessage.TypeMX, 1); !h.Authoritative || h.Truncated {
		t.Errorf("header %+v, want the reply's AA bit kept and no TC on a complete reply", h)
	}
	_, _, _, ar := parsedMessage(t, out)
	opt, ok := ar[0].Body.(*dnsmessage.OPTResource)
	if !ok || len(opt.Options) != 1 || opt.Options[0].Code != 10 || !bytes.Equal(opt.Options[0].Data, cookie.Data) || ar[0].Header.Class != 1232 {
		t.Fatalf("additional %+v, want the reply's OPT record with its cookie: a resolver that holds a server cookie drops an answer without it and asks again over TCP", ar[0])
	}
}

func TestWithoutPinnedAliasKeepsTheTruncationOfATruncatedReply(t *testing.T) {
	for _, qtype := range []dnsmessage.Type{dnsmessage.TypeMX, dnsmessage.TypeTXT} {
		resp := aliasTestReply(t, "www.instagram.com.", qtype, dnsmessage.Header{Truncated: true}, func(b *dnsmessage.Builder) error {
			if err := b.StartAnswers(); err != nil {
				return err
			}
			return cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 3600)
		})

		if h := emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", qtype, 0); !h.Truncated {
			t.Errorf("%v: TC cleared on a truncated reply, the client takes the alias-only part for the whole answer instead of asking again over TCP", qtype)
		}
	}
}

func TestWithoutPinnedAliasKeepsTheTargetsTXTUnderTheAskedName(t *testing.T) {
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeTXT, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 2132); err != nil {
			return err
		}
		return b.TXTResource(rrHeader("z-p42-instagram.c10r.instagram.com.", dnsmessage.TypeTXT), dnsmessage.TXTResource{TXT: []string{"v=spf1 a ~all"}})
	})

	_, an, _ := rewrittenFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.TypeTXT)

	if len(an) != 1 || strings.Join(an[0].Body.(*dnsmessage.TXTResource).TXT, "") != "v=spf1 a ~all" || an[0].Header.TTL != pinTestTTL {
		t.Fatalf("answers %+v, want the target's TXT record under the asked name with the pin TTL", an)
	}
}

func TestWithoutPinnedAliasKeepsDelegatedRecordsUnderAPinnedDomain(t *testing.T) {
	dmarc := aliasTestReply(t, "_dmarc.apex.test.", dnsmessage.TypeTXT, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "_dmarc.apex.test.", "_dmarc.mail.example.net.", 3600); err != nil {
			return err
		}
		h := rrHeader("_dmarc.mail.example.net.", dnsmessage.TypeTXT)
		h.TTL = 20
		return b.TXTResource(h, dnsmessage.TXTResource{TXT: []string{"v=DMARC1; p=none"}})
	})
	_, an, _ := rewrittenFor(t, WithoutPinnedAlias(dmarc, pinnedNames("apex.test"), pinTestTTL, 0), "_dmarc.apex.test.", dnsmessage.TypeTXT)
	if len(an) != 1 || strings.Join(an[0].Body.(*dnsmessage.TXTResource).TXT, "") != "v=DMARC1; p=none" || an[0].Header.TTL != 20 {
		t.Fatalf("answers %+v, want the delegated DMARC record under the asked name with its own TTL", an)
	}

	mx := aliasTestReply(t, "sub.apex.test.", dnsmessage.TypeMX, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "sub.apex.test.", "mail.example.net.", 3600); err != nil {
			return err
		}
		if err := b.MXResource(rrHeader("mail.example.net.", dnsmessage.TypeMX), dnsmessage.MXResource{Pref: 10, MX: dnsmessage.MustNewName("mx1.mail.example.net.")}); err != nil {
			return err
		}
		return b.MXResource(rrHeader("mail.example.net.", dnsmessage.TypeMX), dnsmessage.MXResource{Pref: 20, MX: dnsmessage.MustNewName("mx2.mail.example.net.")})
	})
	_, an, _ = rewrittenFor(t, WithoutPinnedAlias(mx, pinnedNames("apex.test"), pinTestTTL, 0), "sub.apex.test.", dnsmessage.TypeMX)
	var got []string
	for _, rr := range an {
		got = append(got, rr.Body.(*dnsmessage.MXResource).MX.String())
	}
	if strings.Join(got, ",") != "mx1.mail.example.net.,mx2.mail.example.net." {
		t.Fatalf("exchanges %v, want both of the target's MX records, names intact", got)
	}
}

func TestWithoutPinnedAliasKeepsANAPTRRecordWithItsReplacementName(t *testing.T) {
	naptr := []byte{0, 10, 0, 100, 1, 'S', 7, 'S', 'I', 'P', '+', 'D', '2', 'U', 0, 0xC0, 0x0C}
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.Type(rrTypeNAPTR), dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 3600); err != nil {
			return err
		}
		return b.UnknownResource(rrHeader("z-p42-instagram.c10r.instagram.com.", dnsmessage.Type(rrTypeNAPTR)), dnsmessage.UnknownResource{Type: dnsmessage.Type(rrTypeNAPTR), Data: naptr})
	})

	_, an, _ := rewrittenFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.Type(rrTypeNAPTR))
	want := append(append([]byte(nil), naptr[:15]...), encodeName("www.instagram.com")...)
	if len(an) != 1 || !bytes.Equal(an[0].Body.(*dnsmessage.UnknownResource).Data, want) {
		t.Fatalf("answers %+v, want the NAPTR record with its compressed replacement name written out", an)
	}
}

func TestWithoutPinnedAliasReadsPastLegacyRecordsItDoesNotKeep(t *testing.T) {
	rp := append(encodeName("admin.example.net"), encodeName("info.example.net")...)
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeMX, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 3600); err != nil {
			return err
		}
		if err := b.StartAuthorities(); err != nil {
			return err
		}
		return b.UnknownResource(rrHeader("instagram.com.", dnsmessage.Type(17)), dnsmessage.UnknownResource{Type: dnsmessage.Type(17), Data: rp})
	})
	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.TypeMX, 0)

	asked := aliasTestReply(t, "www.instagram.com.", dnsmessage.Type(17), dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 3600); err != nil {
			return err
		}
		return b.UnknownResource(rrHeader("z-p42-instagram.c10r.instagram.com.", dnsmessage.Type(17)), dnsmessage.UnknownResource{Type: dnsmessage.Type(17), Data: rp})
	})
	if out := WithoutPinnedAlias(asked, pinnedNames("www.instagram.com"), pinTestTTL, 0); out != nil {
		t.Fatal("a record b4 cannot copy safely was rewritten, its names could point into the old message")
	}
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

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.TypeMX, 1)
}

func TestWithoutPinnedAliasMatchesNamesWhateverTheirCase(t *testing.T) {
	resp := aliasTestReply(t, "wWw.InStAgRaM.cOm.", dnsmessage.TypeMX, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		return cnameRR(b, "WWW.INSTAGRAM.COM.", "z-p42-instagram.c10r.instagram.com.", 3600)
	})

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "wWw.InStAgRaM.cOm.", dnsmessage.TypeMX, 0)
}

func TestWithoutPinnedAliasCatchesADNAMEOfAPinnedName(t *testing.T) {
	target := encodeName("instagram.example.net")
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeTXT, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		return b.UnknownResource(rrHeader("instagram.com.", dnsmessage.Type(rrTypeDNAME)), dnsmessage.UnknownResource{Type: dnsmessage.Type(rrTypeDNAME), Data: target})
	})

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.TypeTXT, 0)
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

	_, an, ar := rewrittenFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.TypeA)
	if len(an) != 1 || an[0].Header.TTL != 46 || len(ar) != 1 {
		t.Fatalf("%d answers (TTL %v) and %d additionals, want one A record keeping the resolver's 46 s and the OPT record", len(an), an, len(ar))
	}
	if got := net.IP(an[0].Body.(*dnsmessage.AResource).A[:]).String(); got != "31.13.72.174" {
		t.Fatalf("address %s, want the resolver's 31.13.72.174", got)
	}
}

func TestWithoutPinnedAliasCapsALongTTLAtThePinTTL(t *testing.T) {
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeA, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 3600); err != nil {
			return err
		}
		h := rrHeader("z-p42-instagram.c10r.instagram.com.", dnsmessage.TypeA)
		h.TTL = 300
		return b.AResource(h, dnsmessage.AResource{A: [4]byte{31, 13, 72, 174}})
	})

	_, an, _ := rewrittenFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.TypeA)
	if len(an) != 1 || an[0].Header.TTL != pinTestTTL {
		t.Fatalf("answers %+v, want the address kept with the pin TTL", an)
	}
}

func TestWithoutPinnedAliasKeepsAnIPv4MappedAAAA(t *testing.T) {
	resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeAAAA, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.instagram.com.", "z-p42-instagram.c10r.instagram.com.", 3600); err != nil {
			return err
		}
		var mapped [16]byte
		copy(mapped[:], net.ParseIP("::ffff:31.13.72.174").To16())
		return b.AAAAResource(rrHeader("z-p42-instagram.c10r.instagram.com.", dnsmessage.TypeAAAA), dnsmessage.AAAAResource{AAAA: mapped})
	})

	_, an, _ := rewrittenFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.TypeAAAA)
	if len(an) != 1 || net.IP(an[0].Body.(*dnsmessage.AAAAResource).AAAA[:]).String() != "31.13.72.174" {
		t.Fatalf("answers %+v, want the IPv4-mapped AAAA record kept", an)
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

	emptyAnswerFor(t, WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0), "www.instagram.com.", dnsmessage.TypeMX, 0)
}

func TestWithoutPinnedAliasPassesAnAddressAnswerThatCarriesNoAddress(t *testing.T) {
	for _, h := range []dnsmessage.Header{{Authoritative: true}, {Truncated: true}, {RCode: dnsmessage.RCodeNameError}} {
		resp := aliasTestReply(t, "www.instagram.com.", dnsmessage.TypeA, h, func(b *dnsmessage.Builder) error {
			if err := b.StartAnswers(); err != nil {
				return err
			}
			return cnameRR(b, "www.instagram.com.", "edge.cdn.example.net.", 3600)
		})
		if out := WithoutPinnedAlias(resp, pinnedNames("www.instagram.com"), pinTestTTL, 0); out != nil {
			t.Errorf("header %+v: an A answer with the alias and no address was rewritten, a recursive resolver could no longer follow the alias to the site's addresses", h)
		}
	}
}

func TestWithoutPinnedAliasPassesAnAnswerThatWouldOutgrowTheClientsLimit(t *testing.T) {
	resp := aliasTestReply(t, "www.example.com.", dnsmessage.TypeMX, dnsmessage.Header{}, func(b *dnsmessage.Builder) error {
		if err := b.StartAnswers(); err != nil {
			return err
		}
		if err := cnameRR(b, "www.example.com.", "a-rather-long-mail-target-name.mail.example.net.", 3600); err != nil {
			return err
		}
		for i := range 20 {
			exchange := dnsmessage.MustNewName("m" + strings.Repeat("x", i%3) + ".a-rather-long-mail-target-name.mail.example.net.")
			if err := b.MXResource(rrHeader("a-rather-long-mail-target-name.mail.example.net.", dnsmessage.TypeMX), dnsmessage.MXResource{Pref: uint16(i), MX: exchange}); err != nil {
				return err
			}
		}
		return nil
	})
	if len(resp) > 512 {
		t.Fatalf("the test reply is %d bytes, it must fit a plain UDP answer", len(resp))
	}

	if out := WithoutPinnedAlias(resp, pinnedNames("www.example.com"), pinTestTTL, 0); out != nil && len(out) > 512 {
		t.Fatalf("the rewritten answer is %d bytes, past the 512 a client without EDNS takes", len(out))
	}
	if out := WithoutPinnedAlias(resp, pinnedNames("www.example.com"), pinTestTTL, MaxTCPMessageSize); out == nil {
		t.Fatal("over TCP the answer fits and must lose its alias")
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
		if out := WithoutPinnedAlias(tc.resp, pinnedNames("instagram.com"), pinTestTTL, 0); out != nil {
			t.Errorf("%s: rewritten, want it passed unchanged", tc.name)
		}
	}
	if out := WithoutPinnedAlias(aliasOf("www.instagram.com."), nil, pinTestTTL, 0); out != nil {
		t.Error("rewritten without a pin list")
	}
}
