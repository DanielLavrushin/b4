package watchdog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

var errNoDiscoveryRuntime = errors.New("no discovery runtime is available")

type discoveryDriver interface {
	IsActive() bool
	Start(cfg *config.Config, urls []string, opts discovery.StartSuiteOptions) (string, error)
	Snapshot(id string) (*discovery.CheckSuite, bool)
	Finish(id string)
	Cancel(id string)
}

type runtimeDriver struct {
	rt *discovery.Runtime
}

func (d runtimeDriver) IsActive() bool {
	return d.rt != nil && d.rt.IsActive()
}

func (d runtimeDriver) Start(cfg *config.Config, urls []string, opts discovery.StartSuiteOptions) (string, error) {
	if d.rt == nil {
		return "", errNoDiscoveryRuntime
	}
	suite, err := d.rt.StartSuite(cfg, urls, opts)
	if err != nil {
		return "", err
	}
	return suite.Id, nil
}

func (d runtimeDriver) Snapshot(id string) (*discovery.CheckSuite, bool) {
	return discovery.SnapshotCheckSuite(id)
}

func (d runtimeDriver) Finish(id string) {
	_ = discovery.FinishCheckSuite(id)
}

func (d runtimeDriver) Cancel(id string) {
	_ = discovery.CancelCheckSuite(id)
	if d.rt != nil {
		d.rt.Stop(id)
	}
}

func SetRevision(set *config.SetConfig) string {
	if set == nil {
		return ""
	}
	return revisionOf(set)
}

func ConfigRevision(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return revisionOf(cfg)
}

func revisionOf(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16]
}
