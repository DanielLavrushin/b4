package hub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
)

const (
	outboxDirName = "outbox"
	OutboxTTL     = 30 * 24 * time.Hour
)

func (s *Service) outboxDir() string {
	return filepath.Join(s.Dir(), outboxDirName)
}

func (s *Service) enqueue(rec *hubwire.Record) error {
	if rec.Kind == hubwire.RecordShare {
		return ErrShareNotQueued
	}
	id := rec.ID()
	if id == "" {
		return ErrRecordID
	}
	if err := os.MkdirAll(s.outboxDir(), 0700); err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(s.outboxDir(), id+".json"), raw, 0600)
}

func (s *Service) outboxFiles() []string {
	entries, err := os.ReadDir(s.outboxDir())
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

func (s *Service) OutboxCount() int {
	return len(s.outboxFiles())
}

func (s *Service) FlushOutbox(ctx context.Context) {
	dir := s.outboxDir()
	cutoff := s.now().Add(-OutboxTTL).Unix()
	for _, name := range s.outboxFiles() {
		if ctx.Err() != nil {
			return
		}
		path := filepath.Join(dir, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var rec hubwire.Record
		if err := json.Unmarshal(raw, &rec); err != nil || rec.TS < cutoff {
			log.Debugf("hub: dropping outbox record %s (stale or unreadable)", name)
			_ = os.Remove(path)
			continue
		}
		_, err = s.Send(ctx, &rec)
		if err == nil {
			log.Infof("hub: queued %s record delivered", rec.Kind)
			_ = os.Remove(path)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if Retryable(err) {
			log.Debugf("hub: outbox delivery postponed: %v", err)
			return
		}
		log.Warnf("hub: queued %s record refused by the hub and dropped: %v", rec.Kind, err)
		_ = os.Remove(path)
	}
}
