package tables

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const fakeNftScript = `#!/bin/sh
d="$(dirname "$0")"
printf '%s\n' "$*" >> "$d/calls"
case "$1 $2" in
"list tables") cat "$d/tables" ;;
"list chain")
	f="$d/chain.$3.$4.$5"
	[ -f "$f" ] || { echo "Error: No such file or directory" >&2; exit 1; }
	cat "$f"
	;;
esac
exit 0
`

func fakeNft(t *testing.T, tables string, chains map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string, mode os.FileMode) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), mode); err != nil {
			t.Fatal(err)
		}
	}
	write("tables", tables, 0o644)
	for name, body := range chains {
		write("chain."+name, body, 0o644)
	}
	write("nft", fakeNftScript, 0o755)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

func nftNatChain(family, table, chain, hook string, rules ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table %s %s {\n\tchain %s {\n\t\ttype nat hook %s priority dstnat; policy accept;\n", family, table, chain, hook)
	for _, r := range rules {
		fmt.Fprintf(&b, "\t\t%s\n", r)
	}
	b.WriteString("\t}\n}\n")
	return b.String()
}

const (
	nftPaddedReturn = "meta mark & 0x00008000 == 0x00008000 return"
	nftRedirect     = "tcp dport 53 redirect to :5453"
)

func dnsTCPChains(family, table string, pre, out []string) map[string]string {
	return map[string]string{
		family + "." + table + "." + nftDNSTCPPreChain: nftNatChain(family, table, nftDNSTCPPreChain, "prerouting", pre...),
		family + "." + table + "." + nftDNSTCPOutChain: nftNatChain(family, table, nftDNSTCPOutChain, "output", out...),
	}
}

func TestTheNftMonitorWantsTheReturnAheadOfTheRedirectInBothDNSTCPChains(t *testing.T) {
	intact := []string{nftPaddedReturn, nftRedirect}
	both := "table inet b4_mangle\ntable ip b4_dnsnat\ntable ip6 b4_dnsnat6\n"
	for _, tc := range []struct {
		name     string
		tables   string
		pre, out []string
		want     bool
	}{
		{"both chains as nft lists them", both, intact, intact, true},
		{"the return gone from the prerouting chain", both, []string{nftRedirect}, intact, false},
		{"the redirect gone from the output chain", both, intact, []string{nftPaddedReturn}, false},
		{"the return below the redirect", both, []string{nftRedirect, nftPaddedReturn}, intact, false},
		{"a return for another mark", both, []string{"meta mark & 0x00400000 == 0x00400000 return", nftRedirect}, intact, false},
		{"a redirect to another port", both, intact, []string{nftPaddedReturn, "tcp dport 53 redirect to :5454"}, false},
		{"another port's redirect to the listener", both, []string{nftPaddedReturn, "tcp dport 853 redirect to :5453"}, intact, false},
		{"a UDP redirect to the listener", both, intact, []string{nftPaddedReturn, "udp dport 53 redirect to :5453"}, false},
		{"only the IPv6 table", "table inet b4_mangle\ntable ip6 b4_dnsnat6\n", intact, intact, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeNft(t, tc.tables, dnsTCPChains("ip", nftDNSTCPTableName, tc.pre, tc.out))
			c := config.NewConfig()
			if got := NewNFTablesManager(&c).dnsTCPFamilyIntact(false, 0x8000, 5453); got != tc.want {
				t.Errorf("the IPv4 DNS-over-TCP rules read as intact = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTheIPv6DNSTCPTableIsNotTakenForTheIPv4One(t *testing.T) {
	intact := []string{nftPaddedReturn, nftRedirect}
	dir := fakeNft(t, "table inet b4_mangle\ntable ip6 b4_dnsnat6\n", dnsTCPChains("ip6", nftDNSTCPTableName6, intact, intact))
	c := config.NewConfig()
	n := NewNFTablesManager(&c)
	if n.dnsTCPFamilyExists(false) {
		t.Error("the ip6 table b4_dnsnat6 was taken for the ip table b4_dnsnat, so a deleted IPv4 table goes unnoticed")
	}
	if !n.dnsTCPFamilyIntact(true, 0x8000, 5453) {
		t.Error("the IPv6 DNS-over-TCP rules read as broken")
	}
	n.clearDNSTCPFamily(false)
	calls, err := os.ReadFile(filepath.Join(dir, "calls"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(calls), "\n") {
		if strings.Contains(line, "table ip "+nftDNSTCPTableName) {
			t.Errorf("clearing the IPv4 family touched a table that does not exist: nft %s", line)
		}
	}
}
