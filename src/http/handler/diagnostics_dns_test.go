package handler

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/tables"
)

func todaysDNSBeforeConntrackRow(after, missing, pkgs []string) DiagCapability {
	c := DiagCapability{
		Name:      "dns_before_conntrack",
		Available: len(after) == 0,
		Missing:   missing,
		Packages:  pkgs,
		Reasons:   tables.KernelModuleReasons(missing),
	}
	if len(after) == 0 {
		c.Detail = "DNS queries are queued ahead of conntrack"
	} else {
		c.Detail = fmt.Sprintf("DNS queries are queued after conntrack on %s; kernels before 4.18 (4.14 before 4.14.173) then drop the second of two queries a device sends at once from one socket (A and AAAA)", strings.Join(after, ", "))
	}
	return c
}

func TestDNSQueryStateRows(t *testing.T) {
	copyMissing := []string{"nfnetlink_log", "xt_NFLOG"}
	listenerErr := "b4 has no DNS query copy listener"
	keeneticReasons := append(tables.KernelModuleReasons(copyMissing), listenerErr)

	for _, tc := range []struct {
		name string
		st   tables.DNSQueryState
		want []DiagCapability
	}{
		{
			name: "no rules applied",
			st:   tables.DNSQueryState{},
		},
		{
			name: "nftables held after conntrack, as today",
			st: tables.DNSQueryState{
				Applied: true, Release: "6.8.0",
				Placements: map[string]string{"nftables": "mangle"},
			},
			want: []DiagCapability{todaysDNSBeforeConntrackRow([]string{"nftables"}, nil, nil)},
		},
		{
			name: "unaffected iptables, IPv6 without raw, as today",
			st: tables.DNSQueryState{
				Applied: true, Release: "6.8.0",
				Placements:  map[string]string{"iptables": "raw", "ip6tables": "mangle"},
				RawMissing:  []string{"ip6table_raw"},
				RawPackages: []string{"kmod-ipt-raw6"},
			},
			want: []DiagCapability{todaysDNSBeforeConntrackRow([]string{"ip6tables"}, []string{"ip6table_raw"}, []string{"kmod-ipt-raw6"})},
		},
		{
			name: "unaffected iptables, every binary raw, as today",
			st: tables.DNSQueryState{
				Applied: true, Release: "6.8.0",
				Placements: map[string]string{"iptables": "raw", "ip6tables": "raw"},
			},
			want: []DiagCapability{todaysDNSBeforeConntrackRow(nil, nil, nil)},
		},
		{
			name: "affected vanilla kernel, every binary raw",
			st: tables.DNSQueryState{
				Applied: true, Release: "4.1.52", Affected: true,
				Placements: map[string]string{"iptables": "raw", "ip6tables": "raw"},
			},
			want: []DiagCapability{todaysDNSBeforeConntrackRow(nil, nil, nil)},
		},
		{
			name: "affected vanilla kernel, IPv4 raw and IPv6 copied",
			st: tables.DNSQueryState{
				Applied: true, Release: "4.1.52", Affected: true, Group: 537,
				Placements: map[string]string{"iptables": "raw", "ip6tables": "copy"},
			},
			want: []DiagCapability{
				todaysDNSBeforeConntrackRow(nil, nil, nil),
				{Name: "dns_query_copy", Available: true, Detail: "DNS queries are copied with NFLOG (group 537) and never held, on ip6tables"},
			},
		},
		{
			name: "affected vanilla kernel, IPv4 held in mangle",
			st: tables.DNSQueryState{
				Applied: true, Release: "4.9.337", Affected: true, Group: 537,
				Placements:   map[string]string{"iptables": "mangle", "ip6tables": "copy"},
				RawMissing:   []string{"iptable_raw"},
				RawPackages:  []string{"kmod-ipt-raw"},
				CopyMissing:  copyMissing,
				CopyPackages: []string{"kmod-nfnetlink-log", "kmod-ipt-nflog", "iptables-mod-nflog"},
				CopyError:    listenerErr,
			},
			want: []DiagCapability{
				todaysDNSBeforeConntrackRow([]string{"iptables"}, []string{"iptable_raw"}, []string{"kmod-ipt-raw"}),
				{
					Name:     "dns_query_copy",
					Missing:  copyMissing,
					Packages: []string{"kmod-nfnetlink-log", "kmod-ipt-nflog", "iptables-mod-nflog"},
					Reasons:  append(tables.KernelModuleReasons(copyMissing), listenerErr),
					Detail:   "DNS queries are held after conntrack on iptables, and this kernel (4.9.337) drops the second of two queries a device sends at once from one socket",
				},
			},
		},
		{
			name: "affected vanilla kernel, every binary copied",
			st: tables.DNSQueryState{
				Applied: true, Release: "4.9.303", Affected: true, Group: 537,
				Placements: map[string]string{"iptables": "copy", "ip6tables": "copy"},
			},
			want: []DiagCapability{
				{Name: "dns_query_copy", Available: true, Detail: "DNS queries are copied with NFLOG (group 537) and never held, on ip6tables, iptables"},
			},
		},
		{
			name: "KeeneticOS copied",
			st: tables.DNSQueryState{
				Applied: true, Release: "4.9-ndm-5", KeeneticOS: true, Affected: true, Group: 537,
				Placements: map[string]string{"iptables": "copy", "ip6tables": "copy"},
			},
			want: []DiagCapability{
				{Name: "dns_query_copy", Available: true, Detail: "DNS queries are copied with NFLOG (group 537) and never held, on ip6tables, iptables"},
			},
		},
		{
			name: "KeeneticOS held because another program holds the NFLOG group",
			st: tables.DNSQueryState{
				Applied: true, Release: "4.9-ndm-5", KeeneticOS: true, Affected: true,
				Placements: map[string]string{"iptables": "mangle"},
				CopyError:  "b4 could not bind its NFLOG listener: the NFLOG group is bound by another program",
			},
			want: []DiagCapability{
				{
					Name:    "dns_query_copy",
					Reasons: []string{"b4 could not bind its NFLOG listener: the NFLOG group is bound by another program"},
					Detail:  "DNS queries are held after conntrack on iptables, and this kernel (4.9-ndm-5) drops the second of two queries a device sends at once from one socket",
				},
			},
		},
		{
			name: "KeeneticOS held after conntrack",
			st: tables.DNSQueryState{
				Applied: true, Release: "4.9-ndm-5", KeeneticOS: true, Affected: true,
				Placements:  map[string]string{"iptables": "mangle", "ip6tables": "mangle"},
				CopyMissing: copyMissing,
				CopyError:   listenerErr,
			},
			want: []DiagCapability{
				{
					Name:    "dns_query_copy",
					Missing: copyMissing,
					Reasons: keeneticReasons,
					Detail:  "DNS queries are held after conntrack on ip6tables, iptables, and this kernel (4.9-ndm-5) drops the second of two queries a device sends at once from one socket",
					Hint:    "keenetic_netfilter",
				},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := dnsQueryCapabilities(tc.st)
			markKeeneticHints(got, tc.st.KeeneticOS)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("rows =\n%+v\nwant\n%+v", got, tc.want)
			}
		})
	}
}

func TestDNSQueryCopyDiagnostics(t *testing.T) {
	for _, st := range []tables.DNSQueryState{
		{},
		{Applied: true, Release: "6.8.0", Placements: map[string]string{"iptables": "raw", "ip6tables": "mangle"}},
		{Applied: true, Release: "4.9.337", Placements: map[string]string{"nftables": "mangle"}},
	} {
		if got := dnsQueryCopyDiag(st, nfq.DNSQueryCopyCounters{}); got != nil {
			t.Fatalf("%+v: a router that never copies got a dns_query_copy section: %+v", st, got)
		}
	}

	copied := tables.DNSQueryState{
		Applied: true, Release: "4.9-ndm-5", KeeneticOS: true, Affected: true, Group: 537,
		Placements: map[string]string{"iptables": "copy", "ip6tables": "copy"},
	}
	counters := nfq.DNSQueryCopyCounters{
		Group: 537, Bound: true, Alive: true,
		Copies: 120, CopiesLost: 1, CopiesIgnored: 3,
		ClaimsAnswer: 40, ClaimsDrop: 5, ClaimsQuiet: 7, ClaimsRefused: 2, ClaimsLive: 9,
		RepliesReplaced: 38, RepliesReplaceFallback: 1, RepliesDropped: 6, RepliesQuiet: 7, RepliesFailedOpen: 2,
		Injections: 4, ExpiredUnanswered: 3,
	}
	want := &DiagDNSQueryCopy{
		Group: 537, Placements: copied.Placements, Listening: true,
		Copies: 120, CopiesLost: 1, CopiesIgnored: 3,
		ClaimsAnswer: 40, ClaimsDrop: 5, ClaimsQuiet: 7, ClaimsRefused: 2, ClaimsLive: 9,
		RepliesReplaced: 38, RepliesReplaceFallback: 1, RepliesDropped: 6, RepliesQuiet: 7, RepliesFailedOpen: 2,
		Injections: 4, ExpiredUnanswered: 3,
	}
	got := dnsQueryCopyDiag(copied, counters)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("section =\n%+v\nwant\n%+v", got, want)
	}
	raw, err := json.Marshal(DiagFirewall{DNSQueryCopy: got})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"dns_query_copy", "group", "placements", "listening", "copies", "copies_lost", "copies_ignored",
		"claims_answer", "claims_drop", "claims_quiet", "claims_refused", "claims_live",
		"replies_replaced", "replies_replace_fallback", "replies_dropped", "replies_quiet", "replies_failed_open",
		"injections", "expired_unanswered",
	} {
		if !strings.Contains(string(raw), `"`+key+`":`) {
			t.Errorf("the diagnostics JSON has no %q: %s", key, raw)
		}
	}

	held := tables.DNSQueryState{
		Applied: true, Release: "4.9-ndm-5", KeeneticOS: true, Affected: true,
		Placements: map[string]string{"iptables": "mangle"},
		CopyError:  "bind NFLOG group 537: operation not permitted",
	}
	got = dnsQueryCopyDiag(held, nfq.DNSQueryCopyCounters{Error: "the NFLOG group is bound by another program"})
	if got == nil || got.Listening || got.Error != "bind NFLOG group 537: operation not permitted; the NFLOG group is bound by another program" {
		t.Fatalf("section for a router that holds queries = %+v", got)
	}
	got = dnsQueryCopyDiag(held, nfq.DNSQueryCopyCounters{Error: held.CopyError})
	if got.Error != held.CopyError {
		t.Fatalf("error %q, want the shared error once", got.Error)
	}
	taken := held
	taken.CopyError = "b4 could not bind its NFLOG listener: the NFLOG group is bound by another program"
	got = dnsQueryCopyDiag(taken, nfq.DNSQueryCopyCounters{Error: "the NFLOG group is bound by another program"})
	if got.Error != taken.CopyError {
		t.Fatalf("error %q, want the bind failure once, not followed by the listener's own copy of it", got.Error)
	}
}

func TestNFLOGModuleRowsOnlyWhereDNSQueriesAreCopiedOrHeldAfterConntrack(t *testing.T) {
	base := []string{"xt_NFQUEUE", "nfnetlink_queue", "xt_connbytes", "xt_multiport", "nf_conntrack"}
	withNFLOG := append(slices.Clone(base), "xt_NFLOG", "nfnetlink_log")
	for _, tc := range []struct {
		name    string
		backend string
		st      tables.DNSQueryState
		want    []string
	}{
		{"before the first apply", "iptables", tables.DNSQueryState{}, base},
		{"unaffected kernel", "iptables", tables.DNSQueryState{Applied: true, Release: "6.8.0", Placements: map[string]string{"iptables": "raw", "ip6tables": "mangle"}}, base},
		{"affected kernel whose raw table queues every query", "iptables", tables.DNSQueryState{Applied: true, Release: "4.1.52", Affected: true, Placements: map[string]string{"iptables": "raw", "ip6tables": "raw"}}, base},
		{"affected kernel copying IPv6", "iptables", tables.DNSQueryState{Applied: true, Release: "4.1.52", Affected: true, Placements: map[string]string{"iptables": "raw", "ip6tables": "copy"}}, withNFLOG},
		{"affected kernel holding after conntrack", "iptables-legacy", tables.DNSQueryState{Applied: true, Release: "4.9-ndm-5", KeeneticOS: true, Affected: true, Placements: map[string]string{"iptables-legacy": "mangle"}}, withNFLOG},
		{"nftables", "nftables", tables.DNSQueryState{Applied: true, Release: "4.9.337", Placements: map[string]string{"nftables": "mangle"}}, []string{"nft_queue", "nfnetlink_queue", "nft_ct", "nf_conntrack"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := kernelModuleNames(tc.backend, tc.st); !slices.Equal(got, tc.want) {
				t.Errorf("modules %v, want %v: a module b4 has no use for shows as missing with a warning", got, tc.want)
			}
		})
	}
}

func TestSystemInfoListsTheRawTableOnlyWhereItExists(t *testing.T) {
	orig := rawTablePresent
	t.Cleanup(func() { rawTablePresent = orig })

	rawTablePresent = func(string) bool { return true }
	chains, tableNames := diagRuleSources("iptables")
	if !slices.Equal(chains, diagB4Chains) || !slices.Equal(tableNames, []string{"mangle", "nat", "raw", "filter"}) {
		t.Fatalf("with a raw table the listing changed: %v %v", chains, tableNames)
	}

	var asked []string
	rawTablePresent = func(bin string) bool {
		asked = append(asked, bin)
		return false
	}
	chains, tableNames = diagRuleSources("iptables-legacy")
	if !slices.Equal(asked, []string{"iptables-legacy"}) {
		t.Errorf("asked about %v, want the binary that lists the rules", asked)
	}
	if slices.Contains(tableNames, "raw") || slices.ContainsFunc(chains, func(c diagChainRef) bool { return c.table == "raw" }) {
		t.Errorf("without iptable_raw each raw listing makes the KeeneticOS kernel try to load it on every System Info request: %v %v", chains, tableNames)
	}
	if len(chains) != len(diagB4Chains)-1 || !slices.Equal(tableNames, []string{"mangle", "nat", "filter"}) {
		t.Errorf("only the raw table may be left out, got %v %v", chains, tableNames)
	}
}

func TestKeeneticHintMarksEveryUnavailableRow(t *testing.T) {
	caps := []DiagCapability{
		{Name: "packet_queue", Available: true},
		{Name: "transparent_proxy"},
		{Name: "reply_side_bypass"},
	}
	markKeeneticHints(caps, false)
	for _, c := range caps {
		if c.Hint != "" {
			t.Errorf("%s: a router that is not KeeneticOS got the KeeneticOS hint", c.Name)
		}
	}
	markKeeneticHints(caps, true)
	for _, c := range caps {
		if want := map[bool]string{true: "", false: "keenetic_netfilter"}[c.Available]; c.Hint != want {
			t.Errorf("%s (available %v): hint %q, want %q", c.Name, c.Available, c.Hint, want)
		}
	}
}
