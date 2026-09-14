package geo

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/sni"
)

type fileStamp struct {
	size    int64
	modTime time.Time
}

type category struct {
	plain   map[string]struct{}
	regexps []string
}

type Index struct {
	path       string
	mu         sync.Mutex
	stamp      fileStamp
	categories map[string]*category
}

type Coverage struct {
	Category string
	Entry    string
	Relation sni.DomainRelation
}

func NewIndex(path string) *Index {
	return &Index{path: path, categories: make(map[string]*category)}
}

func (i *Index) Path() string {
	return i.path
}

func (i *Index) currentStamp() (fileStamp, bool) {
	info, err := os.Stat(i.path)
	if err != nil {
		return fileStamp{}, false
	}
	return fileStamp{size: info.Size(), modTime: info.ModTime()}, true
}

func (i *Index) Available() bool {
	_, ok := i.currentStamp()
	return ok
}

func (i *Index) refreshLocked() bool {
	stamp, ok := i.currentStamp()
	if !ok {
		i.categories = make(map[string]*category)
		i.stamp = fileStamp{}
		return false
	}
	if stamp != i.stamp {
		i.categories = make(map[string]*category)
		i.stamp = stamp
	}
	return true
}

func normaliseCategory(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func (i *Index) loadLocked(name string) (*category, error) {
	if c, ok := i.categories[name]; ok {
		return c, nil
	}
	entries, err := geodat.LoadDomainsFromCategories(i.path, []string{name})
	if err != nil {
		return nil, err
	}
	c := &category{plain: make(map[string]struct{}, len(entries))}
	for _, entry := range entries {
		value, isRegex := sni.ParseDomainEntry(entry)
		if value == "" {
			continue
		}
		if isRegex {
			c.regexps = append(c.regexps, entry)
			continue
		}
		c.plain[value] = struct{}{}
	}
	i.categories[name] = c
	return c, nil
}

func (i *Index) Warm(categories []string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.refreshLocked() {
		return nil
	}
	for _, name := range categories {
		name = normaliseCategory(name)
		if name == "" {
			continue
		}
		if _, err := i.loadLocked(name); err != nil {
			return err
		}
	}
	return nil
}

func (i *Index) Loaded() []string {
	i.mu.Lock()
	defer i.mu.Unlock()
	out := make([]string, 0, len(i.categories))
	for name := range i.categories {
		out = append(out, name)
	}
	return out
}

func (i *Index) Size(name string) int {
	i.mu.Lock()
	defer i.mu.Unlock()
	c, ok := i.categories[normaliseCategory(name)]
	if !ok {
		return 0
	}
	return len(c.plain) + len(c.regexps)
}

func suffixes(domain string) []string {
	out := []string{domain}
	for {
		dot := strings.IndexByte(domain, '.')
		if dot < 0 {
			return out
		}
		domain = domain[dot+1:]
		if domain == "" {
			return out
		}
		out = append(out, domain)
	}
}

func (i *Index) Match(name, domain string) (Coverage, bool) {
	domain = sni.NormalizeDomain(domain)
	name = normaliseCategory(name)
	if domain == "" || name == "" {
		return Coverage{}, false
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.refreshLocked() {
		return Coverage{}, false
	}
	c, err := i.loadLocked(name)
	if err != nil {
		return Coverage{}, false
	}
	for _, candidate := range suffixes(domain) {
		if _, ok := c.plain[candidate]; !ok {
			continue
		}
		rel, matched := sni.MatchDomainEntry(candidate, domain)
		if rel == sni.RelationExact || rel == sni.RelationCovered {
			return Coverage{Category: name, Entry: matched, Relation: rel}, true
		}
	}
	for _, entry := range c.regexps {
		if rel, matched := sni.MatchDomainEntry(entry, domain); rel == sni.RelationRegexp {
			return Coverage{Category: name, Entry: matched, Relation: rel}, true
		}
	}
	return Coverage{}, false
}
