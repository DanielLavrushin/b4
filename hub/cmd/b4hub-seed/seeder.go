package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"slices"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	scriptDays = 45.0
	debounce   = 2 * time.Minute
)

type event struct {
	at time.Time
	fn func() error
}

type seeder struct {
	ctx     context.Context
	st      *store.Store
	secret  []byte
	clock   *clock
	rng     *rand.Rand
	world   *world
	ingest  *ingest.Service
	mod     *moderation.Service
	builder *catalogue.Builder
	start   time.Time
	end     time.Time
	days    int
	port    string
	console moderation.Actor
	cli     moderation.Actor

	events    []event
	pending   string
	pendingAt time.Time
	voters    []*router
	voted     map[string]map[*router]bool
	banned    map[*router]bool
	stats     map[string]int
}

func (s *seeder) Request(trigger string) {
	if s.pending == "" {
		s.pending, s.pendingAt = trigger, s.clock.now()
	}
}

func (s *seeder) at(day float64) time.Time {
	return s.start.Add(time.Duration(day / scriptDays * float64(s.end.Sub(s.start))))
}

func (s *seeder) on(day float64, fn func() error) {
	s.onTime(s.at(day), fn)
}

func (s *seeder) onTime(t time.Time, fn func() error) {
	s.events = append(s.events, event{at: t, fn: fn})
}

func (s *seeder) between(from, to float64) float64 {
	return from + s.rng.Float64()*(to-from)
}

func (s *seeder) run() error {
	slices.SortStableFunc(s.events, func(a, b event) int { return a.at.Compare(b.at) })
	for i, e := range s.events {
		s.clock.set(e.at)
		if err := e.fn(); err != nil {
			return fmt.Errorf("%s: %w", e.at.Format(time.RFC3339), err)
		}
		if s.pending == "" {
			continue
		}
		due := s.pendingAt.Add(debounce)
		if i+1 < len(s.events) && s.events[i+1].at.Before(due) {
			continue
		}
		s.clock.set(due)
		trigger := s.pending
		s.pending = ""
		if err := s.build(trigger); err != nil {
			return err
		}
	}
	s.clock.set(s.end)
	return s.build(catalogue.TriggerSchedule)
}

func (s *seeder) build(trigger string) error {
	if _, err := s.builder.BuildFor(s.ctx, trigger); err != nil {
		return fmt.Errorf("build: %w", err)
	}
	s.stats["builds"]++
	return nil
}

func (s *seeder) daily() {
	for t := s.start.Add(27 * time.Hour); t.Before(s.end); t = t.Add(24 * time.Hour) {
		s.onTime(t, func() error {
			if _, built, err := s.builder.BuildIfNeeded(s.ctx); err != nil {
				return fmt.Errorf("scheduled build: %w", err)
			} else if built {
				s.stats["builds"]++
			}
			return nil
		})
	}
}

func (s *seeder) keyOf(r *router) string {
	return hubdata.KeyHMAC(s.secret, r.id.KeyID())
}

func (s *seeder) send(r *router, kind string, body interface{}, want ...int) (ingest.Response, error) {
	rec, err := hubwire.SignRecord(r.id, kind, body, s.clock.now())
	if err != nil {
		return ingest.Response{}, err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return ingest.Response{}, err
	}
	resp := s.ingest.HandleFrom(s.ctx, raw, ingest.Source{IP: r.ip})
	if len(want) == 0 {
		want = []int{http.StatusAccepted}
	}
	if !slices.Contains(want, resp.Status) {
		return resp, fmt.Errorf("%s from %s: hub answered %d %v", kind, r.ip, resp.Status, resp.Body["error"])
	}
	s.stats[kind]++
	return resp, nil
}

func (s *seeder) share(day float64, author *router, p *setSpec, from *setSpec) {
	s.on(day, func() error {
		_, err := s.publish(author, p, from, http.StatusAccepted)
		return err
	})
}

func (s *seeder) duplicate(day float64, author *router, p *setSpec) {
	s.on(day, func() error {
		copied := *p
		copied.pub = nil
		_, err := s.publish(author, &copied, nil, http.StatusConflict)
		return err
	})
}

func (s *seeder) publish(author *router, p *setSpec, from *setSpec, want int) (*published, error) {
	set := p.config()
	env, _, err := hubwire.Build(&set, hubwire.BuildOptions{B4Version: author.version, Engine: author.engine, ReadPayload: readPayload, Description: p.description})
	if err != nil {
		return nil, fmt.Errorf("share %q: %w", p.title, err)
	}
	if from != nil {
		env.DerivedFrom = &hubwire.Origin{ID: from.pub.setID, Version: from.pub.version}
	}
	body := hubwire.ShareBody{Envelope: *env, ASNHint: author.net.ASN, CountryHint: author.net.Country, Engine: author.engine, B4Version: author.version}
	resp, err := s.send(author, hubwire.RecordShare, body, want)
	if err != nil {
		return nil, fmt.Errorf("share %q: %w", p.title, err)
	}
	setID, _ := resp.Body["set_id"].(string)
	version, _ := resp.Body["version"].(int)
	v, err := s.st.GetVersion(s.ctx, setID, version)
	if err != nil {
		return nil, fmt.Errorf("share %q: %w", p.title, err)
	}
	if want != http.StatusAccepted {
		return nil, nil
	}
	p.pub = &published{author: author, setID: setID, version: version, fp: v.FP}
	return p.pub, nil
}

func (s *seeder) refs(specs []*setSpec) ([]moderation.Ref, error) {
	refs := make([]moderation.Ref, 0, len(specs))
	for _, p := range specs {
		if p.pub == nil {
			return nil, fmt.Errorf("%q was never published", p.title)
		}
		refs = append(refs, moderation.Ref{SetID: p.pub.setID, Version: p.pub.version})
	}
	return refs, nil
}

func (s *seeder) moderate(day float64, action, reason string, specs ...*setSpec) {
	s.on(day, func() error {
		refs, err := s.refs(specs)
		if err != nil {
			return err
		}
		if _, err := s.mod.Moderate(s.ctx, s.console, action, refs, moderation.Options{Reason: reason}); err != nil {
			return fmt.Errorf("%s %s: %w", action, specs[0].ref(), err)
		}
		return nil
	})
}

func (s *seeder) votes(p *setSpec, n int, works, from, to float64) {
	for range n {
		kind := hubwire.VoteBroken
		if s.rng.Float64() < works {
			kind = hubwire.VoteWorks
		}
		s.on(s.between(from, to), func() error {
			return s.vote(p, kind)
		})
	}
}

func (s *seeder) vote(p *setSpec, kind string) error {
	if p.pub == nil {
		return fmt.Errorf("vote on %q before it was published", p.title)
	}
	slot := p.ref()
	if s.voted[slot] == nil {
		s.voted[slot] = make(map[*router]bool)
	}
	var voter *router
	for range 50 {
		r := s.voters[s.rng.IntN(len(s.voters))]
		if r != p.pub.author && !s.banned[r] && !s.voted[slot][r] {
			voter = r
			break
		}
	}
	if voter == nil {
		return nil
	}
	s.voted[slot][voter] = true
	return s.voteAs(voter, p, kind)
}

func (s *seeder) voteAs(voter *router, p *setSpec, kind string) error {
	domain := ""
	if len(p.domains) > 0 && s.rng.IntN(5) < 3 {
		domain = p.domains[s.rng.IntN(len(p.domains))]
	}
	_, err := s.send(voter, hubwire.RecordVote, hubwire.VoteBody{
		SetID:       p.pub.setID,
		Version:     p.pub.version,
		FP:          p.pub.fp,
		Kind:        kind,
		Domain:      domain,
		ASNHint:     voter.net.ASN,
		CountryHint: voter.net.Country,
		Engine:      voter.engine,
		B4Version:   voter.version,
	})
	return err
}

func (s *seeder) report(day float64, p *setSpec, r *router, reason string) {
	s.on(day, func() error {
		if p.pub == nil {
			return fmt.Errorf("report on %q before it was published", p.title)
		}
		_, err := s.send(r, hubwire.RecordReport, hubwire.ReportBody{SetID: p.pub.setID, Version: p.pub.version, Reason: reason})
		return err
	})
}

func (s *seeder) settleReport(day float64, p *setSpec, r *router, action, note string) {
	s.on(day, func() error {
		reports, err := s.st.ReportsForVersion(s.ctx, p.pub.setID, p.pub.version)
		if err != nil {
			return err
		}
		key := s.keyOf(r)
		for _, rep := range reports {
			if rep.KeyHMAC == key {
				_, err := s.mod.Reports(s.ctx, s.console, []int64{rep.ID}, action, note)
				return err
			}
		}
		return fmt.Errorf("no report on %s from %s", p.ref(), r.ip)
	})
}

func (s *seeder) settleVersionReports(day float64, p *setSpec, action, note string) {
	s.on(day, func() error {
		_, err := s.mod.VersionReports(s.ctx, s.console, p.pub.setID, p.pub.version, action, note)
		return err
	})
}

func (s *seeder) key(day float64, r *router, action, reason string) {
	s.on(day, func() error {
		if _, err := s.mod.Key(s.ctx, s.console, s.keyOf(r), action, moderation.KeyOptions{Reason: reason, Create: true}); err != nil {
			return fmt.Errorf("%s key: %w", action, err)
		}
		switch action {
		case moderation.ActionBan:
			s.banned[r] = true
		case moderation.ActionUnban:
			delete(s.banned, r)
		}
		return nil
	})
}

func (s *seeder) profile(day float64, r *router, name, note, tag string) {
	s.on(day, func() error {
		_, err := s.mod.KeyProfile(s.ctx, s.console, s.keyOf(r), store.KeyProfile{Name: name, Note: note, Tag: tag})
		return err
	})
}

type mirrorNode struct {
	*router
	url     string
	version string
}

func (s *seeder) announce(m *mirrorNode, from, to float64) {
	for day := from; day < to; day++ {
		at := day
		if day > from {
			at += s.between(0, 0.2)
		}
		s.on(at, func() error {
			_, err := s.send(m.router, hubwire.RecordMirror, hubwire.MirrorBody{URL: m.url, Version: m.version})
			return err
		})
	}
}

func (s *seeder) mirrorAction(day float64, m *mirrorNode, a moderation.Actor, action, reason string) {
	s.on(day, func() error {
		row, err := s.st.MirrorByURL(s.ctx, m.url)
		if err != nil {
			return fmt.Errorf("mirror %s: %w", m.url, err)
		}
		_, _, err = s.mod.Mirror(s.ctx, a, row.ID, action, reason)
		return err
	})
}

func (s *seeder) checks(m *mirrorNode, from, failFrom float64, code, failure string) {
	until := s.at(failFrom)
	for t := s.at(from); t.Before(s.end); t = t.Add(12 * time.Hour) {
		ok := t.Before(until)
		s.onTime(t, func() error {
			row, err := s.st.MirrorByURL(s.ctx, m.url)
			if err != nil {
				return fmt.Errorf("mirror %s: %w", m.url, err)
			}
			c := store.MirrorCheck{At: s.clock.now(), OK: ok, Millis: int64(40 + s.rng.IntN(360))}
			if !ok {
				c.Code, c.Error, c.Millis = code, failure, 5000
			} else if latest := s.builder.Latest(); latest != nil {
				c.Epoch, c.Seq, c.GeneratedAt = latest.Manifest.Epoch, latest.Manifest.Seq, latest.Manifest.GeneratedAt
			}
			return s.st.RecordMirrorCheck(s.ctx, row.ID, c)
		})
	}
}

func (s *seeder) presets(day float64, presets []store.ReasonPreset) {
	s.on(day, func() error {
		for _, p := range presets {
			if _, err := s.st.CreateReasonPreset(s.ctx, p, s.clock.now()); err != nil {
				return err
			}
		}
		return nil
	})
}
