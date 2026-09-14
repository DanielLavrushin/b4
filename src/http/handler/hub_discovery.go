package handler

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/hub"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
)

const (
	communityCandidateLimit = 3
	communitySearchDepth    = 10
	communityBlobTimeout    = 8 * time.Second
	communityFreshFor       = 10 * time.Minute
	communitySyncTimeout    = 10 * time.Second
)

type communityQueue struct {
	results []hub.Result
	next    int
	taken   int
}

func hubDomainOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "://") {
		if u, err := url.Parse(raw); err == nil {
			raw = u.Hostname()
		}
	}
	if i := strings.IndexAny(raw, "/?#"); i >= 0 {
		raw = raw[:i]
	}
	if h, _, err := splitHostPortLoose(raw); err == nil {
		raw = h
	}
	return strings.ToLower(strings.TrimSuffix(raw, "."))
}

func splitHostPortLoose(hostport string) (string, string, error) {
	i := strings.LastIndex(hostport, ":")
	if i < 0 || strings.Contains(hostport[i+1:], "]") {
		return hostport, "", fmt.Errorf("no port")
	}
	return hostport[:i], hostport[i+1:], nil
}

func (api *API) communityPresets(urls []string, skip bool) []discovery.ConfigPreset {
	svc := globalHubService
	if skip || svc == nil || !svc.Enabled() || !svc.Configured() {
		return nil
	}
	if !svc.SyncedWithin(communityFreshFor) {
		ctx, cancel := context.WithTimeout(context.Background(), communitySyncTimeout)
		if _, err := svc.Sync(ctx); err != nil {
			log.Warnf("discovery: community catalogue not refreshed, using the stored copy: %v", err)
		}
		cancel()
	}
	var queues []*communityQueue
	for _, raw := range urls {
		domain := hubDomainOf(raw)
		if domain == "" {
			continue
		}
		results, _ := svc.Search(domain, communitySearchDepth)
		if len(results) > 0 {
			queues = append(queues, &communityQueue{results: results})
		}
	}
	seen := map[string]bool{}
	var out []discovery.ConfigPreset
	for progress := true; progress; {
		progress = false
		for _, q := range queues {
			for q.taken < communityCandidateLimit && q.next < len(q.results) {
				r := q.results[q.next]
				q.next++
				if r.Set == nil || seen[r.Set.FP] {
					continue
				}
				seen[r.Set.FP] = true
				preset, ok := api.communityPreset(r.Set)
				if !ok {
					continue
				}
				out = append(out, preset)
				q.taken++
				progress = true
				break
			}
		}
	}
	if len(out) > 0 {
		log.Infof("discovery: %d community strategies queued ahead of the presets", len(out))
	}
	return out
}

func (api *API) communityPreset(cs *hubwire.CatalogueSet) (discovery.ConfigPreset, bool) {
	svc := globalHubService
	env := cs.ToEnvelope()
	for _, ref := range cs.Payloads {
		ctx, cancel := context.WithTimeout(context.Background(), communityBlobTimeout)
		data, err := svc.FetchBlob(ctx, ref)
		cancel()
		if err != nil {
			log.Warnf("discovery: community set %s skipped, payload %s unavailable: %v", cs.ID, ref.SHA256, err)
			return discovery.ConfigPreset{}, false
		}
		env.Payloads = append(env.Payloads, hubwire.Payload{SHA256: ref.SHA256, Protocol: ref.Protocol, Domain: ref.Domain, Size: len(data), Data: data})
	}
	imp, err := hubwire.Open(env, hubwire.OpenOptions{B4Version: Version})
	if err != nil {
		log.Warnf("discovery: community set %s skipped: %v", cs.ID, err)
		return discovery.ConfigPreset{}, false
	}
	set := imp.Set
	if _, err := api.installHubPayloads(&set, imp.Payloads); err != nil {
		log.Warnf("discovery: community set %s skipped: %v", cs.ID, err)
		return discovery.ConfigPreset{}, false
	}
	set.Targets = config.TargetsConfig{}
	title := strings.ToLower(strings.Join(strings.Fields(cs.Title), "-"))
	if title == "" {
		title = cs.ID
	}
	return discovery.ConfigPreset{
		Name:         fmt.Sprintf("community-%s-v%d", title, cs.Version),
		Description:  cs.Title,
		Family:       discovery.FamilyCommunity,
		Phase:        discovery.PhaseCached,
		Config:       set,
		FixedPayload: set.Faking.SNIType == config.FakePayloadCapture,
	}, true
}
