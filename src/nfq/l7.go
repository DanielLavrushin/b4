package nfq

import (
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discord"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/stun"
)

func udpFakePayload(udpCfg *config.UDPConfig) []byte {
	switch udpCfg.FakePayloadFile {
	case config.FakePayloadAutoQUIC:
		return sock.BuildQUICInitial(udpCfg.FakeLen)
	case config.FakePayloadAutoSTUN:
		return sock.BuildSTUNRequest(udpCfg.FakeLen)
	}
	return udpCfg.FakePayloadData
}

func udpL7Class(payload []byte) string {
	switch {
	case discord.IsIPDiscovery(payload):
		return config.UDPL7Discord
	case stun.IsSTUNMessage(payload):
		return config.UDPL7STUN
	}
	return ""
}
