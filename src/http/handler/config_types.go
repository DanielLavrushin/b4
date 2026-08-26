package handler

import "github.com/daniellavrushin/b4/nfq"

import "github.com/daniellavrushin/b4/config"

type ConfigRequest struct {
	*config.Config
}

type ConfigResponse struct {
	*config.Config
	Success             bool                       `json:"success"`
	Message             string                     `json:"message"`
	Sets                []SetWithStats             `json:"sets"`
	Warnings            []string                   `json:"warnings,omitempty"`
	AvailableInterfaces []string                   `json:"available_ifaces"`
	TunnelInterfaces    []string                   `json:"tunnel_ifaces"`
	EncapsulatedIfaces  []string                   `json:"encapsulated_ifaces"`
	IfaceTraffic        map[string]nfq.IfaceCounts `json:"iface_traffic,omitempty"`
}
