package sni

import (
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

type DomainRelation string

const (
	RelationNone    DomainRelation = ""
	RelationExact   DomainRelation = "exact"
	RelationCovered DomainRelation = "covered"
	RelationRegexp  DomainRelation = "regexp"
	RelationCovers  DomainRelation = "covers"
)

const regexEntryPrefix = "regexp:"

const entryRegexCacheLimit = 2000

var (
	entryRegexCache     sync.Map
	entryRegexCacheSize int32
)

func (r DomainRelation) Priority() int {
	switch r {
	case RelationExact:
		return 4
	case RelationCovered:
		return 3
	case RelationRegexp:
		return 2
	case RelationCovers:
		return 1
	}
	return 0
}

func NormalizeDomain(domain string) string {
	return strings.TrimRight(strings.ToLower(strings.TrimSpace(domain)), ".")
}

func ParseDomainEntry(entry string) (value string, isRegex bool) {
	e := strings.ToLower(strings.TrimSpace(entry))
	if strings.HasPrefix(e, regexEntryPrefix) {
		return strings.TrimPrefix(e, regexEntryPrefix), true
	}
	return strings.TrimRight(e, "."), false
}

func CanonicalDomainEntry(entry string) string {
	value, isRegex := ParseDomainEntry(entry)
	if value == "" {
		return ""
	}
	if isRegex {
		return regexEntryPrefix + value
	}
	return value
}

func MatchDomainEntry(entry, domain string) (DomainRelation, string) {
	domain = NormalizeDomain(domain)
	if domain == "" {
		return RelationNone, ""
	}

	value, isRegex := ParseDomainEntry(entry)
	if value == "" {
		return RelationNone, ""
	}

	if isRegex {
		if re := compileEntryRegex(value); re != nil && re.MatchString(domain) {
			return RelationRegexp, regexEntryPrefix + value
		}
		return RelationNone, ""
	}

	switch {
	case value == domain:
		return RelationExact, value
	case strings.HasSuffix(domain, "."+value):
		return RelationCovered, value
	case strings.HasSuffix(value, "."+domain):
		return RelationCovers, value
	}

	return RelationNone, ""
}

func compileEntryRegex(pattern string) *regexp.Regexp {
	if cached, ok := entryRegexCache.Load(pattern); ok {
		re, _ := cached.(*regexp.Regexp)
		return re
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		re = nil
	}

	if atomic.LoadInt32(&entryRegexCacheSize) < entryRegexCacheLimit {
		entryRegexCache.Store(pattern, re)
		atomic.AddInt32(&entryRegexCacheSize, 1)
	}

	return re
}
