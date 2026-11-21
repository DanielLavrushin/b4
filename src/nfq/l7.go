package nfq

import (
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discord"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/stun"
)

const maxUDPFakeLenV6 = config.MaxUDPFakeLen - (IPv6HeaderLen - IPv4HeaderMinLen)

func udpFakePayload(udpCfg *config.UDPConfig, size int) []byte {
	switch udpCfg.FakePayloadFile {
	case config.FakePayloadAutoQUIC:
		return sock.BuildQUICInitial(size)
	case config.FakePayloadAutoSTUN:
		return sock.BuildSTUNRequest(size)
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
