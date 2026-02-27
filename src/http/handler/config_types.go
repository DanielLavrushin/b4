package handler

import "github.com/daniellavrushin/b4/config"

type ConfigRequest struct {
	*config.Config
}

type ConfigResponse struct {
	*config.Config
	Success             bool           `json:"success"`
	Message             string         `json:"message"`
	Sets                []SetWithStats `json:"sets"`
	Warnings            []string       `json:"warnings,omitempty"`
	AvailableInterfaces []string       `json:"available_ifaces"`
}
