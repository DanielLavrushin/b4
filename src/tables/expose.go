package tables

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	exposeChain      = "B4_EXPOSE"
	exposeCommentTag = "b4-expose"
	exposeKickSettle = 3 * time.Second
	exposeOwnedRetry = 10 * time.Minute
)

type ExposeStatus struct {
	SkipSetup bool                 `json:"skip_setup"`
	Ports     []config.ExposedPort `json:"ports"`
	Blocked   []config.ExposeBlock `json:"blocked"`
	Chains    []string             `json:"chains"`
	Error     string               `json:"error,omitempty"`
}

var (
	exposeMu        sync.Mutex
	exposeWanted    []config.ExposedPort
	exposeBlocked   []config.ExposeBlock
	exposeChains    []string
	exposeFailures  = map[string]string{}
	exposeOwned     = map[string]time.Time{}
	exposeSkip      bool
	exposeSynced    bool
	exposeInstalled bool
	exposeClosed    bool

	exposeStatus    atomic.Pointer[ExposeStatus]
	exposeKick      = make(chan struct{}, 1)
	exposeSettle    = exposeKickSettle
	exposeNow       = time.Now
	readProcNetFile = os.ReadFile
	xtVariantCache  sync.Map
)

var (
	nftExposeNameRe      = regexp.MustCompile(`^[A-Za-z_.][A-Za-z0-9/_.\-]*$`)
	errExposeTableOwned  = errors.New("the table belongs to another program, which lets no other program change its rules")
	nftNamedHookPriority = map[string]int{"raw": -300, "mangle": -150, "dstnat": -100, "filter": 0, "security": 50, "srcnat": 100}
)

func SyncExposure(ports []config.ExposedPort, blocked []config.ExposeBlock, skipSetup bool) {
	exposeMu.Lock()
	defer exposeMu.Unlock()
	exposeSyncLocked(ports, blocked, skipSetup)
}

func exposeSyncLocked(ports []config.ExposedPort, blocked []config.ExposeBlock, skipSetup bool) {
	if exposeClosed {
		return
	}
	first := !exposeSynced
	exposeSynced = true
	changed := first || skipSetup != exposeSkip || !reflect.DeepEqual(ports, exposeWanted)
	if first || !reflect.DeepEqual(blocked, exposeBlocked) {
		logExposeBlocked(blocked)
	}
	exposeWanted, exposeBlocked, exposeSkip = ports, blocked, skipSetup

	switch {
	case skipSetup:
		if exposeInstalled {
			if changed {
				log.Infof("Expose: Skip IPTables/NFTables Setup is on, so b4 removes the rules it added to open its ports")
			}
			exposeRemoveLocked()
			if !changed && !exposeInstalled {
				log.Infof("Expose: removed the rules that opened b4's ports to the internet")
			}
		} else if changed && len(ports) > 0 {
			log.Warnf("Expose: Skip IPTables/NFTables Setup is on, so b4 adds no firewall rule to open %s to the internet", describeExposedPorts(ports))
		}
	case !changed:
	case len(ports) == 0:
		if exposeInstalled || first {
			exposeRemoveLocked()
			if !first && !exposeInstalled {
				log.Infof("Expose: removed the rules that opened b4's ports to the internet")
			}
		}
	default:
		exposeApplyLocked()
	}
	exposePublishLocked()
}

func ShrinkExposure(keep []config.ExposedPort) {
	exposeMu.Lock()
	defer exposeMu.Unlock()
	if exposeClosed || exposeSkip || !exposeSynced || len(exposeWanted) == 0 {
		return
	}
	var kept []config.ExposedPort
	for _, p := range exposeWanted {
		for _, k := range keep {
			if p == k {
				kept = append(kept, p)
				break
			}
		}
	}
	if len(kept) == len(exposeWanted) {
		return
	}
	var closed []config.ExposedPort
	for _, p := range exposeWanted {
		gone := true
		for _, k := range kept {
			if p == k {
				gone = false
				break
			}
		}
		if gone {
			closed = append(closed, p)
		}
	}
	log.Infof("Expose: closing %s", describeExposedPorts(closed))
	exposeWanted = kept
	if len(kept) == 0 {
		exposeRemoveLocked()
	} else {
		exposeApplyLocked()
	}
	exposePublishLocked()
}

func ClearExposure() {
	exposeMu.Lock()
	defer exposeMu.Unlock()
	exposeClosed = true
	if !exposeInstalled {
		return
	}
	exposeRemoveLocked()
	exposePublishLocked()
}

func SweepExposure() {
	exposeMu.Lock()
	defer exposeMu.Unlock()
	exposeClosed = true
	exposeRemoveLocked()
	exposePublishLocked()
}

func ExposureStatus() ExposeStatus {
	if s := exposeStatus.Load(); s != nil {
		return *s
	}
	return ExposeStatus{Ports: []config.ExposedPort{}, Blocked: []config.ExposeBlock{}, Chains: []string{}}
}

func KickExposure() {
	select {
	case exposeKick <- struct{}{}:
	default:
	}
}

func StartExposureWatch(interval time.Duration, resync func()) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go exposeWatchLoop(interval, resync, stop, done)
	var once sync.Once
	return func() {
		once.Do(func() {
			close(stop)
			<-done
		})
	}
}

func exposeWatchLoop(interval time.Duration, resync func(), stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	var tick <-chan time.Time
	if interval > 0 {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		tick = ticker.C
	}
	pass := func() {
		if resync != nil {
			resync()
		}
		exposeCheck()
	}
	for {
		select {
		case <-stop:
			return
		case <-tick:
			pass()
		case <-exposeKick:
			settle := time.NewTimer(exposeSettle)
		waiting:
			for {
				select {
				case <-stop:
					settle.Stop()
					return
				case <-exposeKick:
				case <-settle.C:
					break waiting
				}
			}
			pass()
		}
	}
}

func exposeCheck() {
	exposeMu.Lock()
	defer exposeMu.Unlock()
	if exposeClosed || exposeSkip {
		return
	}
	if len(exposeWanted) == 0 {
		if exposeInstalled {
			exposeRemoveLocked()
			if !exposeInstalled {
				log.Infof("Expose: removed the rules that opened b4's ports to the internet")
			}
			exposePublishLocked()
		}
		return
	}
	IPTablesLockBudgetReset()
	var chains, restored []string
	failures := map[string]string{}
	removals := map[string]string{}
	for _, t := range discoverExposeTargets() {
		if t.ruleCount(exposeWanted) == 0 {
			if _, pending := exposeFailures[t.name()]; pending {
				if err := t.remove(); err != nil {
					removals[t.name()] = err.Error()
				}
			}
			continue
		}
		if !t.present(exposeWanted) {
			if err := t.apply(exposeWanted); err != nil {
				failures[t.name()] = err.Error()
				continue
			}
			restored = append(restored, t.name())
		}
		chains = append(chains, t.name())
	}
	exposeReportFailuresLocked(failures, "the accept rules are missing from %s and could not be restored: %s")
	exposeReportFailuresLocked(removals, "could not remove b4's accept rules from %s, will retry: %s")
	for name, msg := range removals {
		failures[name] = msg
	}
	if len(restored) > 0 {
		log.Warnf("Expose: the rules opening %s were missing from %s, usually after the firewall was reloaded; restored them",
			describeExposedPorts(exposeWanted), strings.Join(restored, ", "))
	}
	exposeInstalled = true
	exposeChains = chains
	exposeFailures = failures
	exposePublishLocked()
}

func exposeApplyLocked() {
	IPTablesLockBudgetReset()
	var chains []string
	failures := map[string]string{}
	removals := map[string]string{}
	for _, t := range discoverExposeTargets() {
		if t.ruleCount(exposeWanted) == 0 {
			if err := t.remove(); err != nil {
				removals[t.name()] = err.Error()
			}
			continue
		}
		if err := t.apply(exposeWanted); err != nil {
			failures[t.name()] = err.Error()
			continue
		}
		chains = append(chains, t.name())
	}
	exposeReportFailuresLocked(failures, "could not add the accept rules to %s: %s")
	exposeReportFailuresLocked(removals, "could not remove b4's accept rules from %s, will retry: %s")
	for name, msg := range removals {
		failures[name] = msg
	}
	exposeInstalled = true
	exposeChains = chains
	exposeFailures = failures
	switch {
	case len(chains) > 0:
		log.Infof("Expose: %s open to the internet through %s", describeExposedPorts(exposeWanted), strings.Join(chains, ", "))
	case len(failures) > 0:
		log.Warnf("Expose: no accept rule could be added, so a firewall that drops incoming connections keeps %s closed", describeExposedPorts(exposeWanted))
	default:
		log.Infof("Expose: no firewall chain on this host filters incoming connections, so no rule is needed to reach %s", describeExposedPorts(exposeWanted))
	}
}

func exposeRemoveLocked() {
	IPTablesLockBudgetReset()
	failures := map[string]string{}
	for _, t := range discoverExposeTargets() {
		if err := t.remove(); err != nil {
			failures[t.name()] = err.Error()
		}
	}
	exposeReportFailuresLocked(failures, "could not remove b4's accept rules from %s, will retry: %s")
	exposeInstalled = len(failures) > 0
	exposeChains = nil
	exposeFailures = failures
}

func exposeReportFailuresLocked(failures map[string]string, format string) {
	for name, msg := range failures {
		if exposeFailures[name] != msg {
			log.Warnf("Expose: "+format, name, msg)
		}
	}
}

func exposePublishLocked() {
	names := make([]string, 0, len(exposeFailures))
	for name := range exposeFailures {
		names = append(names, name)
	}
	sort.Strings(names)
	msgs := make([]string, 0, len(names))
	for _, name := range names {
		msgs = append(msgs, name+": "+exposeFailures[name])
	}
	s := &ExposeStatus{
		SkipSetup: exposeSkip,
		Ports:     append([]config.ExposedPort{}, exposeWanted...),
		Blocked:   append([]config.ExposeBlock{}, exposeBlocked...),
		Chains:    append([]string{}, exposeChains...),
		Error:     strings.Join(msgs, "; "),
	}
	if exposeSkip {
		s.Chains = []string{}
	}
	exposeStatus.Store(s)
}

type exposeTarget interface {
	name() string
	ruleCount(ports []config.ExposedPort) int
	present(ports []config.ExposedPort) bool
	apply(ports []config.ExposedPort) error
	remove() error
}

func discoverExposeTargets() []exposeTarget {
	var targets []exposeTarget
	xtOwned := make(map[string]bool)
	for _, v6 := range []bool{false, true} {
		family := "ip"
		if v6 {
			family = "ip6"
		}
		legacy, nft := xtExposeBinaries(v6)
		if legacy != "" && legacyFilterTableExists(v6) {
			targets = append(targets, xtExposeTarget{bin: legacy, v6: v6})
		}
		if nft != "" {
			xtOwned[family] = true
			if xtNftInputChainExists(family, nft) {
				targets = append(targets, xtExposeTarget{bin: nft, v6: v6})
			}
		}
	}
	return append(targets, discoverNftExposeChains(xtOwned)...)
}

func xtExposeBinaries(v6 bool) (legacy, nft string) {
	plain, legacyName, nftName := backendIPTables, backendIPTablesLegacy, "iptables-nft"
	if v6 {
		plain, legacyName, nftName = backendIP6Tables, backendIP6TablesLegacy, "ip6tables-nft"
	}
	if hasBinary(legacyName) {
		legacy = legacyName
	}
	if hasBinary(nftName) {
		nft = nftName
	}
	if hasBinary(plain) {
		if xtIsNftVariant(plain) {
			if nft == "" {
				nft = plain
			}
		} else if legacy == "" {
			legacy = plain
		}
	}
	return legacy, nft
}

func xtIsNftVariant(bin string) bool {
	if v, ok := xtVariantCache.Load(bin); ok {
		return v.(bool)
	}
	out, _ := run(bin, "--version")
	isNft := strings.Contains(out, "nf_tables")
	xtVariantCache.Store(bin, isNft)
	return isNft
}

func legacyFilterTableExists(v6 bool) bool {
	path := "/proc/net/ip_tables_names"
	if v6 {
		path = "/proc/net/ip6_tables_names"
	}
	raw, err := readProcNetFile(path)
	if err != nil {
		return false
	}
	for _, name := range strings.Fields(string(raw)) {
		if name == "filter" {
			return true
		}
	}
	return false
}

func xtNftInputChainExists(family, bin string) bool {
	if hasBinary("nft") {
		_, err := run("nft", "list", "chain", family, "filter", "INPUT")
		return err == nil
	}
	out, err := run(bin, "-w", "-t", "filter", "-nL", "INPUT")
	if err != nil {
		return false
	}
	return xtListingCanDrop(out)
}

func xtListingCanDrop(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if fields[0] == "Chain" {
			if !strings.Contains(line, "(policy ACCEPT") {
				return true
			}
			continue
		}
		if fields[0] != "target" {
			return true
		}
	}
	return false
}

var xtCompatTables = map[string]bool{"filter": true, "mangle": true, "raw": true, "security": true, "nat": true}

func discoverNftExposeChains(xtOwned map[string]bool) []exposeTarget {
	if !hasBinary("nft") {
		return nil
	}
	out, err := run("nft", "list", "chains")
	if err != nil {
		return nil
	}
	return parseNftInputChains(out, xtOwned)
}

func parseNftInputChains(out string, xtOwned map[string]bool) []exposeTarget {
	var targets []exposeTarget
	var family, table, chain string
	for _, raw := range strings.Split(out, "\n") {
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		switch {
		case fields[0] == "table" && len(fields) >= 3:
			family, table, chain = fields[1], fields[2], ""
		case fields[0] == "chain" && len(fields) >= 2:
			chain = fields[1]
		case chain != "" && strings.Contains(raw, "hook input") && strings.Contains(raw, "type filter"):
			if exposeNftChainEligible(family, table, chain, xtOwned) && nftChainCanDrop(raw) {
				targets = append(targets, nftExposeTarget{family: family, table: table, chain: chain})
			}
			chain = ""
		}
	}
	return targets
}

func exposeNftChainEligible(family, table, chain string, xtOwned map[string]bool) bool {
	switch family {
	case "inet", "ip", "ip6":
	default:
		return false
	}
	if strings.HasPrefix(table, "b4_") || strings.HasPrefix(table, "_b4") {
		return false
	}
	if family != "inet" && xtOwned[family] && xtCompatTables[table] && chain == "INPUT" {
		return false
	}
	return nftExposeNameRe.MatchString(table) && nftExposeNameRe.MatchString(chain)
}

func nftChainCanDrop(hookLine string) bool {
	if nftHookPolicy(hookLine) != "accept" {
		return true
	}
	prio, ok := nftHookPriority(hookLine)
	return !ok || prio >= 0
}

func nftHookPolicy(hookLine string) string {
	idx := strings.Index(hookLine, "policy ")
	if idx < 0 {
		return "accept"
	}
	return strings.TrimSpace(strings.SplitN(hookLine[idx+len("policy "):], ";", 2)[0])
}

func nftHookPriority(hookLine string) (int, bool) {
	idx := strings.Index(hookLine, "priority ")
	if idx < 0 {
		return 0, false
	}
	fields := strings.Fields(strings.SplitN(hookLine[idx+len("priority "):], ";", 2)[0])
	if len(fields) == 0 {
		return 0, false
	}
	base, err := strconv.Atoi(fields[0])
	if err != nil {
		named, ok := nftNamedHookPriority[fields[0]]
		if !ok {
			return 0, false
		}
		base = named
	}
	if len(fields) == 3 {
		n, err := strconv.Atoi(fields[2])
		if err != nil {
			return 0, false
		}
		switch fields[1] {
		case "+":
			base += n
		case "-":
			base -= n
		default:
			return 0, false
		}
	}
	return base, true
}

type xtExposeTarget struct {
	bin string
	v6  bool
}

func (t xtExposeTarget) name() string {
	return t.bin + " filter INPUT"
}

func (t xtExposeTarget) family(ports []config.ExposedPort) []config.ExposedPort {
	var out []config.ExposedPort
	for _, p := range ports {
		if (t.v6 && p.V6) || (!t.v6 && p.V4) {
			out = append(out, p)
		}
	}
	return out
}

func xtExposeSpec(p config.ExposedPort) []string {
	var spec []string
	if p.Address != "" {
		spec = append(spec, "-d", p.Address)
	}
	return append(spec, "-p", "tcp", "--dport", strconv.Itoa(p.Port), "-j", "ACCEPT")
}

func (t xtExposeTarget) ruleCount(ports []config.ExposedPort) int {
	return len(t.family(ports))
}

func (t xtExposeTarget) present(ports []config.ExposedPort) bool {
	out, err := run(t.bin, "-w", "-t", "filter", "-nL", exposeChain, "--line-numbers")
	if err != nil {
		return false
	}
	var want []int
	for _, p := range t.family(ports) {
		want = append(want, p.Port)
	}
	if !sameInts(listedDestPorts(out), want) {
		return false
	}
	jumps, lastBan, listed := xtInputLayout(t.bin)
	if !listed {
		return xtChainReferences(out) == 1
	}
	return len(jumps) == 1 && jumps[0] > lastBan
}

func xtChainReferences(out string) int {
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[0] == "Chain" && strings.HasPrefix(fields[2], "(") && strings.HasPrefix(fields[3], "reference") {
			n, err := strconv.Atoi(strings.TrimPrefix(fields[2], "("))
			if err == nil {
				return n
			}
		}
	}
	return -1
}

func (t xtExposeTarget) apply(ports []config.ExposedPort) error {
	if err := runEnsure(t.bin, "-w", "-t", "filter", "-N", exposeChain); err != nil {
		return err
	}
	if _, err := run(t.bin, "-w", "-t", "filter", "-F", exposeChain); err != nil {
		return err
	}
	for _, p := range t.family(ports) {
		if _, err := run(append([]string{t.bin, "-w", "-t", "filter", "-A", exposeChain}, xtExposeSpec(p)...)...); err != nil {
			return err
		}
	}
	jumps, lastBan, listed := xtInputLayout(t.bin)
	if listed && len(jumps) == 1 && jumps[0] > lastBan {
		return nil
	}
	if !listed || len(jumps) > 0 {
		iptDeleteJumpsTo(t.bin, "filter", "INPUT", exposeChain)
		_, lastBan, _ = xtInputLayout(t.bin)
	}
	_, err := run(t.bin, "-w", "-t", "filter", "-I", "INPUT", strconv.Itoa(lastBan+1), "-j", exposeChain)
	return err
}

func (t xtExposeTarget) remove() error {
	out, err := run(t.bin, "-w", "-t", "filter", "-nL", exposeChain)
	if err != nil {
		if xtChainMissing(out, err) {
			return nil
		}
		return err
	}
	iptDeleteJumpsTo(t.bin, "filter", "INPUT", exposeChain)
	if _, err := run(t.bin, "-w", "-t", "filter", "-F", exposeChain); err != nil {
		return err
	}
	_, err = run(t.bin, "-w", "-t", "filter", "-X", exposeChain)
	return err
}

func xtChainMissing(out string, err error) bool {
	msg := out + " " + err.Error()
	return strings.Contains(msg, "No chain/target/match by that name") || strings.Contains(msg, "does not exist")
}

func xtInputLayout(bin string) (jumps []int, lastBan int, listed bool) {
	out, err := run(bin, "-w", "-t", "filter", "-L", "INPUT", "-n", "--line-numbers")
	if err != nil {
		return nil, 0, false
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, convErr := strconv.Atoi(fields[0])
		if convErr != nil {
			continue
		}
		switch {
		case fields[1] == exposeChain:
			jumps = append(jumps, n)
		case xtBanRule(fields[1], line):
			lastBan = n
		}
	}
	return jumps, lastBan, true
}

func xtBanRule(target, line string) bool {
	switch {
	case strings.HasPrefix(target, "f2b-"), strings.HasPrefix(strings.ToUpper(target), "CROWDSEC"), strings.EqualFold(target, "sshguard"):
		return true
	case target == "ufw-before-input" || target == "ufw6-before-input":
		return true
	case target == "DROP" || target == "REJECT":
		return strings.Contains(line, "match-set") && strings.Contains(line, " src")
	}
	return false
}

func isExposeChain(target string) bool {
	return target == exposeChain
}

func listedDestPorts(out string) []int {
	var ports []int
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if _, err := strconv.Atoi(fields[0]); err != nil {
			continue
		}
		port := -1
		for _, f := range fields {
			if strings.HasPrefix(f, "dpt:") {
				if n, err := strconv.Atoi(strings.TrimPrefix(f, "dpt:")); err == nil {
					port = n
				}
			}
		}
		ports = append(ports, port)
	}
	return ports
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]int{}, a...)
	y := append([]int{}, b...)
	sort.Ints(x)
	sort.Ints(y)
	return reflect.DeepEqual(x, y)
}

type nftExposeTarget struct {
	family string
	table  string
	chain  string
}

func (t nftExposeTarget) name() string {
	return fmt.Sprintf("nft %s %s %s", t.family, t.table, t.chain)
}

func (t nftExposeTarget) tableKey() string {
	return t.family + " " + t.table
}

func (t nftExposeTarget) rules(ports []config.ExposedPort) []string {
	var rules []string
	for _, p := range ports {
		match, ok := nftExposeMatch(t.family, p)
		if !ok {
			continue
		}
		rules = append(rules, fmt.Sprintf("%stcp dport %d accept comment %q", match, p.Port, exposeComment(p)))
	}
	return rules
}

func exposeComment(p config.ExposedPort) string {
	return exposeCommentTag + ":" + p.Service
}

func nftExposeMatch(family string, p config.ExposedPort) (string, bool) {
	switch family {
	case "ip":
		if !p.V4 {
			return "", false
		}
		if p.Address != "" {
			return "ip daddr " + p.Address + " ", true
		}
		return "", true
	case "ip6":
		if !p.V6 {
			return "", false
		}
		if p.Address != "" {
			return "ip6 daddr " + p.Address + " ", true
		}
		return "", true
	}
	switch {
	case p.Address != "" && p.V6 && !p.V4:
		return "ip6 daddr " + p.Address + " ", true
	case p.Address != "" && p.V4 && !p.V6:
		return "ip daddr " + p.Address + " ", true
	case p.Address != "":
		return "", false
	case p.V4 && p.V6:
		return "", true
	case p.V4:
		return "meta nfproto ipv4 ", true
	case p.V6:
		return "meta nfproto ipv6 ", true
	}
	return "", false
}

func (t nftExposeTarget) ruleCount(ports []config.ExposedPort) int {
	return len(t.rules(ports))
}

func (t nftExposeTarget) ownedByAnother() bool {
	since, ok := exposeOwned[t.tableKey()]
	return ok && exposeNow().Sub(since) < exposeOwnedRetry
}

func (t nftExposeTarget) owned() (map[string]string, []string, error) {
	out, err := run("nft", "-a", "list", "chain", t.family, t.table, t.chain)
	if err != nil {
		return nil, nil, err
	}
	byComment := make(map[string]string)
	var handles []string
	for _, line := range strings.Split(out, "\n") {
		comment := nftRuleComment(line)
		if comment != exposeCommentTag && !strings.HasPrefix(comment, exposeCommentTag+":") {
			continue
		}
		if h := nftHandleFromLine(line); h != "" {
			handles = append(handles, h)
		}
		byComment[comment] = line
	}
	return byComment, handles, nil
}

func (t nftExposeTarget) present(ports []config.ExposedPort) bool {
	if t.ownedByAnother() {
		return false
	}
	byComment, handles, err := t.owned()
	if err != nil {
		return false
	}
	want := 0
	for _, p := range ports {
		if _, ok := nftExposeMatch(t.family, p); !ok {
			continue
		}
		want++
		line, ok := byComment[exposeComment(p)]
		if !ok || !strings.Contains(line, fmt.Sprintf("dport %d ", p.Port)) {
			return false
		}
	}
	return len(handles) == want
}

func (t nftExposeTarget) apply(ports []config.ExposedPort) error {
	if t.ownedByAnother() {
		return t.ownedError(ports)
	}
	err := t.replace(t.rules(ports))
	if errors.Is(err, errExposeTableOwned) {
		return t.ownedError(ports)
	}
	return err
}

func (t nftExposeTarget) ownedError(ports []config.ExposedPort) error {
	if t.table != "firewalld" {
		return errExposeTableOwned
	}
	var opts []string
	for _, p := range ports {
		opts = append(opts, fmt.Sprintf("--add-port=%d/tcp", p.Port))
	}
	return fmt.Errorf("%w; firewalld 2.2 and later owns its table by default, so open the port with firewall-cmd --permanent %s followed by firewall-cmd --reload, or set NftablesTableOwner=no in /etc/firewalld/firewalld.conf",
		errExposeTableOwned, strings.Join(opts, " "))
}

func (t nftExposeTarget) remove() error {
	_, handles, err := t.owned()
	if err != nil {
		if strings.Contains(err.Error(), "No such file or directory") {
			return nil
		}
		return err
	}
	if len(handles) == 0 {
		return nil
	}
	if t.ownedByAnother() {
		return errExposeTableOwned
	}
	err = t.replace(nil)
	if err != nil && strings.Contains(err.Error(), "No such file or directory") {
		return nil
	}
	return err
}

func (t nftExposeTarget) replace(rules []string) error {
	_, handles, err := t.owned()
	if err != nil {
		return err
	}
	var script strings.Builder
	for _, h := range handles {
		fmt.Fprintf(&script, "delete rule %s %s %s handle %s\n", t.family, t.table, t.chain, h)
	}
	for i := len(rules) - 1; i >= 0; i-- {
		fmt.Fprintf(&script, "insert rule %s %s %s %s\n", t.family, t.table, t.chain, rules[i])
	}
	if script.Len() == 0 {
		return nil
	}
	out, err := runNftStdin(script.String())
	if err != nil && strings.Contains(out+" "+err.Error(), "Operation not permitted") {
		exposeOwned[t.tableKey()] = exposeNow()
		return errExposeTableOwned
	}
	if err == nil {
		delete(exposeOwned, t.tableKey())
	}
	return err
}

func nftRuleComment(line string) string {
	idx := strings.Index(line, `comment "`)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(`comment "`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func describeExposedPorts(ports []config.ExposedPort) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		var fams []string
		if p.V4 {
			fams = append(fams, "IPv4")
		}
		if p.V6 {
			fams = append(fams, "IPv6")
		}
		where := ""
		if p.Address != "" {
			where = " on " + p.Address
		}
		parts = append(parts, fmt.Sprintf("the %s (TCP %d%s, %s)", exposeServiceLabel(p.Service), p.Port, where, strings.Join(fams, "+")))
	}
	if len(parts) < 2 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

func logExposeBlocked(blocked []config.ExposeBlock) {
	for _, b := range blocked {
		log.Warnf("Expose: Expose to internet is on for the %s, but %s, so b4 does not open its port",
			exposeServiceLabel(b.Service), exposeBlockReason(b.Reason))
	}
}

func exposeServiceLabel(service string) string {
	switch service {
	case config.ExposeWebServer:
		return "web interface"
	case config.ExposeMTProto:
		return "MTProto proxy"
	case config.ExposeMTProtoWebProxy:
		return "Telegram WEB relay"
	case config.ExposeSocks5:
		return "SOCKS5 server"
	}
	return service
}

func exposeBlockReason(reason string) string {
	switch reason {
	case config.ExposeBlockedNoAuth:
		return "it has no username and password"
	case config.ExposeBlockedWebNoAuth:
		return "the web interface has no username and password, and a SOCKS5 client can reach it through the proxy"
	case config.ExposeBlockedOpenRelay:
		return "it has neither a username and password nor an allowed sources list, which would make it an open proxy"
	case config.ExposeBlockedSharedPort:
		return "it has no port of its own and is served on the web interface port"
	case config.ExposeBlockedLoopback:
		return "it is bound to a loopback address that nobody outside can reach"
	case config.ExposeBlockedInvalidBind:
		return "its bind address is not an IP address"
	case config.ExposeBlockedNotListening:
		return "it is not listening on its port, which may belong to another program"
	}
	return reason
}
