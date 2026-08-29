package handler

import (
	"net/http"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/geodat"
)

type API struct {
	cfgPtr         *atomic.Pointer[config.Config]
	mux            *http.ServeMux
	geodataManager *geodat.GeodataManager
	discoveryRT    *discovery.Runtime
	asnStore       *config.AsnStore

	overrideServiceManager  func() string
	overrideLaunchInstaller func(installerRun)
}

func (a *API) getCfg() *config.Config {
	return a.cfgPtr.Load()
}
