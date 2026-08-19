package tables

import (
	"fmt"
	"strconv"
	"strings"
)

const discoveryChainNFT = "b4_discovery"

type discoveryNftBackend struct{}

func (b *discoveryNftBackend) name() string    { return backendNFTables }
func (b *discoveryNftBackend) available() bool { return hasBinary("nft") }

func (b *discoveryNftBackend) ensureBase() error {
	loadKernelModules()
	if err := runEnsure("nft", "add", "table", "inet", nftTableName); err != nil {
		return fmt.Errorf("failed to create table %s: %w", nftTableName, err)
	}
	for _, hook := range []string{"prerouting", "output"} {
		spec := fmt.Sprintf("{ type filter hook %s priority %d ; policy accept ; }", hook, nftBaseChainPriority)
		if err := runEnsure("nft", "add", "chain", "inet", nftTableName, hook, spec); err != nil {
			return fmt.Errorf("failed to create %s chain: %w", hook, err)
		}
	}
	return nil
}

func (b *discoveryNftBackend) apply(flowMark uint, injectedMark uint, queueStart int, threads int) error {
	if err := b.ensureBase(); err != nil {
		return err
	}
	if err := runEnsure("nft", "add", "chain", "inet", nftTableName, discoveryChainNFT); err != nil {
		return fmt.Errorf("failed to create discovery chain: %w", err)
	}
	if _, err := run("nft", "flush", "chain", "inet", nftTableName, discoveryChainNFT); err != nil {
		return fmt.Errorf("failed to flush discovery chain: %w", err)
	}

	queueExpr := fmt.Sprintf("queue num %d bypass", queueStart)
	if threads > 1 {
		queueExpr = fmt.Sprintf("queue num %d-%d bypass", queueStart, queueStart+threads-1)
	}

	flowHex := fmt.Sprintf("0x%x", flowMark)
	injectedHex := fmt.Sprintf("0x%x", injectedMark)
	queueTokens := strings.Fields(queueExpr)

	rules := [][]string{
		{"add", "rule", "inet", nftTableName, discoveryChainNFT, "meta", "mark", injectedHex, "accept"},
		{"add", "rule", "inet", nftTableName, discoveryChainNFT, "ct", "mark", flowHex, "meta", "mark", "set", "ct", "mark"},
		{"add", "rule", "inet", nftTableName, discoveryChainNFT, "meta", "mark", flowHex, "ct", "mark", "set", "mark"},
	}
	queueRule := append([]string{"add", "rule", "inet", nftTableName, discoveryChainNFT, "meta", "mark", flowHex}, queueTokens...)
	rules = append(rules, queueRule)

	for _, r := range rules {
		if _, err := run(append([]string{"nft"}, r...)...); err != nil {
			return err
		}
	}

	b.deleteDiscoveryRulesFromChain("output", flowMark, injectedMark)
	b.deleteDiscoveryRulesFromChain("prerouting", flowMark, injectedMark)

	if _, err := run("nft", "insert", "rule", "inet", nftTableName, "output", "meta", "mark", injectedHex, "accept"); err != nil {
		return err
	}
	if _, err := run("nft", "insert", "rule", "inet", nftTableName, "output", "meta", "mark", flowHex, "accept"); err != nil {
		return err
	}
	if _, err := run("nft", "insert", "rule", "inet", nftTableName, "output", "jump", discoveryChainNFT); err != nil {
		return err
	}
	if _, err := run("nft", "insert", "rule", "inet", nftTableName, "prerouting", "meta", "mark", injectedHex, "accept"); err != nil {
		return err
	}
	if _, err := run("nft", "insert", "rule", "inet", nftTableName, "prerouting", "meta", "mark", flowHex, "accept"); err != nil {
		return err
	}
	if _, err := run("nft", "insert", "rule", "inet", nftTableName, "prerouting", "jump", discoveryChainNFT); err != nil {
		return err
	}
	return nil
}

func (b *discoveryNftBackend) clear(flowMark uint, injectedMark uint) {
	b.deleteDiscoveryRulesFromChain("output", flowMark, injectedMark)
	b.deleteDiscoveryRulesFromChain("prerouting", flowMark, injectedMark)
	_, _ = run("nft", "flush", "chain", "inet", nftTableName, discoveryChainNFT)
	_, _ = run("nft", "delete", "chain", "inet", nftTableName, discoveryChainNFT)
	b.dropTableIfEmpty()
}

func (b *discoveryNftBackend) dropTableIfEmpty() {
	out, err := run("nft", "list", "table", "inet", nftTableName)
	if err != nil {
		return
	}
	if !nftTableIsEmpty(out) {
		return
	}
	_, _ = run("nft", "delete", "table", "inet", nftTableName)
}

func nftTableIsEmpty(listing string) bool {
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "", line == "}", line == "{":
			continue
		case strings.HasPrefix(line, "table "):
			continue
		case strings.HasPrefix(line, "chain "):
			if isDiscoveryBaseChain(line) {
				continue
			}
			return false
		case strings.HasPrefix(line, "type ") && strings.Contains(line, " hook "):
			continue
		}
		return false
	}
	return true
}

func isDiscoveryBaseChain(declaration string) bool {
	fields := strings.Fields(declaration)
	if len(fields) < 2 {
		return false
	}
	return fields[1] == "prerouting" || fields[1] == "output"
}

func (b *discoveryNftBackend) deleteDiscoveryRulesFromChain(chain string, flowMark uint, injectedMark uint) {
	out, err := run("nft", "-a", "list", "chain", "inet", nftTableName, chain)
	if err != nil {
		return
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		isDiscovery := strings.Contains(line, "jump "+discoveryChainNFT) ||
			(strings.Contains(line, "accept") && nftLineHasMark(line, flowMark)) ||
			(strings.Contains(line, "accept") && nftLineHasMark(line, injectedMark))
		if !isDiscovery {
			continue
		}
		idx := strings.Index(line, "# handle ")
		if idx < 0 {
			continue
		}
		handle := strings.TrimSpace(line[idx+len("# handle "):])
		if handle == "" {
			continue
		}
		_, _ = run("nft", "delete", "rule", "inet", nftTableName, chain, "handle", handle)
	}
}

func nftLineHasMark(line string, mark uint) bool {
	rest := line
	for {
		idx := strings.Index(rest, "mark ")
		if idx < 0 {
			return false
		}
		rest = rest[idx+len("mark "):]
		field := rest
		if cut := strings.IndexAny(field, " \t"); cut >= 0 {
			field = field[:cut]
		}
		if parseNftNumber(field) == uint64(mark) {
			return true
		}
	}
}

func parseNftNumber(field string) uint64 {
	base := 10
	if lower := strings.ToLower(field); strings.HasPrefix(lower, "0x") {
		field = field[2:]
		base = 16
	}
	v, err := strconv.ParseUint(field, base, 64)
	if err != nil {
		return ^uint64(0)
	}
	return v
}
