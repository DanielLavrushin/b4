package handler

import "github.com/daniellavrushin/b4/nfq"

import "github.com/daniellavrushin/b4/config"

type ConfigRequest struct {
	*config.Config
}

type ConfigSet struct {
	SetWithStats
	Revision string `json:"revision,omitempty"`
}

type ConfigResponse struct {
	*config.Config
	Revision            string                     `json:"revision,omitempty"`
	Success             bool                       `json:"success"`
	Message             string                     `json:"message"`
	Sets                []ConfigSet                `json:"sets"`
	Warnings            []string                   `json:"warnings,omitempty"`
	AvailableInterfaces []string                   `json:"available_ifaces"`
	TunnelInterfaces    []string                   `json:"tunnel_ifaces"`
	EncapsulatedIfaces  []string                   `json:"encapsulated_ifaces"`
	IfaceTraffic        map[string]nfq.IfaceCounts `json:"iface_traffic,omitempty"`
}
