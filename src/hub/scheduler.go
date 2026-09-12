package hub

import (
	"context"
	"errors"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	syncInterval  = time.Hour
	startupDelay  = 20 * time.Second
	pollInterval  = 15 * time.Second
	stopWaitLimit = 2 * time.Second
)

var (
	ErrShareNotQueued = errors.New("a share is never queued")
	ErrRecordID       = errors.New("record has no id")
)

func (s *Service) Start() {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	if s.stop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	s.kick = make(chan struct{}, 1)
	log.Infof("[HUB] scheduler starting")
	go s.run(ctx, s.stop, s.stopped, s.kick)
}

func (s *Service) Stop() {
	s.runMu.Lock()
	stop, stopped, cancel := s.stop, s.stopped, s.cancel
	s.stop, s.stopped, s.kick, s.cancel = nil, nil, nil, nil
	s.runMu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	cancel()
	select {
	case <-stopped:
		log.Infof("[HUB] scheduler stopped")
	case <-time.After(stopWaitLimit):
		log.Warnf("[HUB] scheduler did not stop within %s, leaving it to the exit", stopWaitLimit)
	}
}

func (s *Service) Kick() {
	s.runMu.Lock()
	kick := s.kick
	s.runMu.Unlock()
	if kick == nil {
		return
	}
	select {
	case kick <- struct{}{}:
	default:
	}
}

func (s *Service) run(ctx context.Context, stop <-chan struct{}, stopped chan<- struct{}, kick <-chan struct{}) {
	defer close(stopped)

	poll := time.NewTicker(pollInterval)
	defer poll.Stop()

	armedAt := s.now().Add(startupDelay)
	var lastRun time.Time
	wasReady := false

	for {
		select {
		case <-stop:
			return
		case <-kick:
			if !s.ready() {
				continue
			}
			s.tick(ctx)
			lastRun = s.now()
			wasReady = true
		case <-poll.C:
			now := s.now()
			if !s.ready() {
				wasReady = false
				continue
			}
			if now.Before(armedAt) {
				continue
			}
			if wasReady && now.Sub(lastRun) < syncInterval {
				continue
			}
			s.tick(ctx)
			lastRun = now
			wasReady = true
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	if _, err := s.Sync(ctx); err != nil {
		if ctx.Err() == nil {
			log.Warnf("[HUB] sync failed: %v", err)
		}
		if errors.Is(err, ErrUnreachable) || ctx.Err() != nil {
			return
		}
	}
	s.FlushOutbox(ctx)
}
