package config

import (
	"slices"
	"strconv"
	"strings"
)

const MaxSetGroupRunes = 64

func NormalizeSetGroup(group string) string {
	group = strings.Join(strings.Fields(group), " ")
	if runes := []rune(group); len(runes) > MaxSetGroupRunes {
		group = strings.TrimSpace(string(runes[:MaxSetGroupRunes]))
	}
	return group
}

func GatherSetGroup(sets []*SetConfig, group, anchorID string) []*SetConfig {
	if group == "" {
		return sets
	}
	inGroup := func(set *SetConfig) bool { return set != nil && set.Group == group }
	anchor := slices.IndexFunc(sets, func(set *SetConfig) bool { return inGroup(set) && set.Id == anchorID })
	if anchor < 0 {
		anchor = slices.IndexFunc(sets, inGroup)
	}
	var members []*SetConfig
	for _, set := range sets {
		if inGroup(set) {
			members = append(members, set)
		}
	}
	if len(members) < 2 {
		return sets
	}
	out := make([]*SetConfig, 0, len(sets))
	for i, set := range sets {
		switch {
		case i == anchor:
			out = append(out, members...)
		case !inGroup(set):
			out = append(out, set)
		}
	}
	return out
}

func UniqueSetGroup(group string, sets []*SetConfig) string {
	taken := make(map[string]bool)
	for _, set := range sets {
		if set != nil && set.Group != "" {
			taken[strings.ToLower(set.Group)] = true
		}
	}
	name := group
	for n := 2; taken[strings.ToLower(name)]; n++ {
		suffix := " " + strconv.Itoa(n)
		base := []rune(group)
		if room := MaxSetGroupRunes - len(suffix); len(base) > room {
			base = base[:room]
		}
		name = strings.TrimSpace(string(base)) + suffix
	}
	return name
}

func SettleSetGroup(sets []*SetConfig, at int) {
	if at < 0 || at >= len(sets) || sets[at] == nil {
		return
	}
	set := sets[at]
	groupAt := func(i int) string {
		if i < 0 || i >= len(sets) || sets[i] == nil {
			return ""
		}
		return sets[i].Group
	}
	before, after := groupAt(at-1), groupAt(at+1)
	switch {
	case before != "" && before == after:
		set.Group = before
	case set.Group == "" || set.Group == before || set.Group == after:
	case slices.ContainsFunc(sets, func(other *SetConfig) bool {
		return other != nil && other != set && other.Group == set.Group
	}):
		set.Group = ""
	}
}
