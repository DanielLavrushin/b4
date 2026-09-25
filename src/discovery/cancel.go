package discovery

import (
	"context"
	"time"
)

func (ds *DiscoverySuite) canceled() bool {
	select {
	case <-ds.cancel:
		return true
	default:
		return false
	}
}

func (ds *DiscoverySuite) finishing() bool {
	select {
	case <-ds.finish:
		return true
	default:
		return false
	}
}

func (ds *DiscoverySuite) interrupted() bool {
	return ds.canceled() || ds.finishing()
}

func (ds *DiscoverySuite) initCancelContext() {
	ds.ctx, ds.ctxCancel = watchedContext(ds.cancel, ds.finish)
}

func (ds *DiscoverySuite) resetFetchContext() {
	ds.ctxCancel()
	ds.ctx, ds.ctxCancel = watchedContext(ds.cancel, nil)
}

func watchedContext(stops ...chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	for _, stop := range stops {
		if stop == nil {
			continue
		}
		go func(stop chan struct{}) {
			select {
			case <-stop:
				cancel()
			case <-ctx.Done():
			}
		}(stop)
	}
	return ctx, cancel
}

func (ds *DiscoverySuite) fetchContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ds.ctx, timeout)
}
