package tables

import "testing"

const iptDumpSample = `Chain PREROUTING (policy ACCEPT 3507 packets, 1274883 bytes)
    pkts      bytes target     prot opt in     out     source               destination
    4940  1836737 B4_PREROUTING  all  --  *      *       0.0.0.0/0            0.0.0.0/0
   17289  4410206 SKIPLOG    tcp  --  br0    *       0.0.0.0/0            0.0.0.0/0            tcp dpt:8443
    3507  1274883 b4r_x_pre  all  --  *      *       0.0.0.0/0            0.0.0.0/0

Chain OUTPUT (policy ACCEPT 5560 packets, 3042876 bytes)
    pkts      bytes target     prot opt in     out     source               destination

Chain b4r_x_pre (1 references)
    pkts      bytes target     prot opt in     out     source               destination
       4      212 RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            mark match 0x8000/0x8000
       0        0 RETURN     all  --  *      *       0.0.0.0/0            0.0.0.0/0            mark match 0x40000/0x40000
      18    14478 MARK       all  --  br0    *       0.0.0.0/0            0.0.0.0/0            ctstate NEW match-set b4r_x_v4 dst MARK xset 0x5179/0x27fff

Chain b4r_x_nat (1 references)
    pkts      bytes target     prot opt in     out     source               destination
`

func TestOneDumpAnswersEveryQuestionTheCheckAsks(t *testing.T) {
	chains := parseIptDump(iptDumpSample)

	if _, ok := chains["b4r_x_pre"]; !ok {
		t.Fatal("chain presence must come out of the dump")
	}
	if _, ok := chains["b4r_x_nat"]; !ok {
		t.Fatal("a chain with no rules of its own still exists and must be seen")
	}
	if got := len(chains["b4r_x_pre"].rules); got != 3 {
		t.Fatalf("want the three rules of the chain and neither the header nor the column titles, got %d", got)
	}

	if !iptDumpJumpsTo(chains, "PREROUTING", "b4r_x_pre") {
		t.Fatal("the jump lives in the parent chain's own section, and reading it there is what tells b4 the " +
			"firmware rebuilt the firewall and took the jump with it")
	}
	if iptDumpJumpsTo(chains, "OUTPUT", "b4r_x_pre") {
		t.Fatal("a jump in one parent must not be read as a jump in another, or a missing OUTPUT jump goes unseen")
	}

	for _, m := range []uint32{0x8000, 0x40000} {
		if !iptDumpReturnsOn(chains, "b4r_x_pre", m) {
			t.Fatalf("the bypass return on 0x%x is rendered as 'mark match 0x%x/0x%x' by -L, not as the -S "+
				"spelling, and the check has to read it either way", m, m, m)
		}
	}
	if iptDumpReturnsOn(chains, "b4r_x_pre", 0x5179) {
		t.Fatal("the set's own mark is set by a MARK rule, not returned on, and must not read as a bypass")
	}
}

func TestAForeignTargetInTheDumpDoesNotBreakParsing(t *testing.T) {
	chains := parseIptDump(iptDumpSample)
	if !iptDumpJumpsTo(chains, "PREROUTING", "SKIPLOG") {
		t.Fatal("the SKIPLOG rule this router carries sits between b4's own entries, so the parser has to walk " +
			"past a target it does not know rather than stop at it the way iptables -S does")
	}
}
