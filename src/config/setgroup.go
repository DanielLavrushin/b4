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
