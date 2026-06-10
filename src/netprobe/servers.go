package netprobe

import (
	_ "embed"
	"encoding/json"

	"github.com/daniellavrushin/b4/log"
)

//go:embed servers.json
var serversJSON []byte

type DoHFormat string

const (
	DoHJSON DoHFormat = "json"
	DoHWire DoHFormat = "wire"
)

type DoHServer struct {
	Name   string    `json:"name"`
	URL    string    `json:"url"`
	Format DoHFormat `json:"format"`
}

var (
	DefaultDoHServers []DoHServer
	DefaultUDPServers []string
	WireDoHServers    []string
)

type serversData struct {
	DoHServers     []DoHServer `json:"doh_servers"`
	UDPServers     []string    `json:"udp_dns_servers"`
	WireDoHServers []string    `json:"wire_doh_servers"`
}

func init() {
	var data serversData
	if err := json.Unmarshal(serversJSON, &data); err != nil {
		log.Errorf("Failed to parse embedded netprobe servers.json: %v", err)
		return
	}
	for i := range data.DoHServers {
		if data.DoHServers[i].Format == "" {
			data.DoHServers[i].Format = DoHJSON
		}
	}
	DefaultDoHServers = data.DoHServers
	DefaultUDPServers = data.UDPServers
	WireDoHServers = data.WireDoHServers
}
