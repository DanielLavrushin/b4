package capture

import (
	"crypto/ecdh"
	"encoding/binary"
	"slices"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/tlsgen"
	"golang.org/x/crypto/cryptobyte"
)

const (
	KindTLSClientHello  = "tls_client_hello"
	KindQUICInitial     = "quic_initial"
	KindSTUN            = "stun"
	KindDTLSClientHello = "dtls_client_hello"
	KindOther           = "other"
)

type Facts struct {
	Kind          string   `json:"kind"`
	QUICVersion   int      `json:"quic_version,omitempty"`
	SNI           string   `json:"sni"`
	Extensions    *int     `json:"extensions,omitempty"`
	ECH           *bool    `json:"ech,omitempty"`
	FitsOnePacket bool     `json:"fits_one_packet"`
	Faults        []string `json:"faults,omitempty"`
}

func FitsOnePacket(protocol string, size int) bool {
	if protocol == "quic" {
		return size <= config.MaxUDPFakeLen
	}
	return size <= config.MaxTCPFakeLenInOnePacket
}

func FactsOf(protocol string, data []byte) *Facts {
	f := &Facts{Kind: KindOther, FitsOnePacket: FitsOnePacket(protocol, len(data))}
	switch {
	case isTLSClientHelloRecord(data):
		h := walkTLSRecord(data)
		f.Kind = KindTLSClientHello
		f.SNI = h.sni
		f.Extensions = &h.extensions
		f.ECH = &h.ech
		f.Faults = h.faults
	case isDTLSClientHelloRecord(data):
		f.Kind = KindDTLSClientHello
		f.SNI = dtlsServerName(data)
	case quic.IsInitial(data):
		f.Kind = KindQUICInitial
		f.QUICVersion = 1
		if binary.BigEndian.Uint32(data[1:5]) != 1 {
			f.QUICVersion = 2
		}
		f.SNI = quicServerName(data)
	case isSTUNMessage(data):
		f.Kind = KindSTUN
	}
	return f
}

func isTLSClientHelloRecord(data []byte) bool {
	return len(data) >= 6 && data[0] == 0x16 && data[1] == 0x03 && data[2] <= 0x04 && data[5] == 0x01
}

func isDTLSClientHelloRecord(data []byte) bool {
	return len(data) >= 25 && data[0] == 0x16 && data[1] == 0xfe && (data[2] == 0xfd || data[2] == 0xff) && data[13] == 0x01
}

func isSTUNMessage(data []byte) bool {
	return len(data) >= 20 &&
		binary.BigEndian.Uint16(data[0:2])&0xc000 == 0 &&
		binary.BigEndian.Uint32(data[4:8]) == 0x2112a442 &&
		len(data) == int(binary.BigEndian.Uint16(data[2:4]))+20
}

func quicServerName(data []byte) string {
	name, ok := sni.ParseQUICClientHelloSNI(data)
	quic.ClearDCID(quic.ParseDCID(data))
	if !ok {
		return ""
	}
	return name
}

func dtlsServerName(data []byte) string {
	body := data[25:]
	if fragment := int(data[22])<<16 | int(data[23])<<8 | int(data[24]); fragment < len(body) {
		body = body[:fragment]
	}
	s := cryptobyte.String(body)
	var sid, cookie cryptobyte.String
	if !s.Skip(2+32) || !s.ReadUint8LengthPrefixed(&sid) {
		return ""
	}
	cookieAt := len(body) - len(s)
	if !s.ReadUint8LengthPrefixed(&cookie) {
		return ""
	}
	tlsShaped := append(slices.Clone(body[:cookieAt]), s...)
	name, ok := sni.ParseTLSClientHelloBodySNI(tlsShaped)
	if !ok {
		return ""
	}
	return name
}

type helloWalk struct {
	sni          string
	extensions   int
	ech          bool
	faults       []string
	cipherSuites []uint16
	extTypes     []uint16
}

func madeByTheGenerator(data []byte, name string) bool {
	if name == "" || !isTLSClientHelloRecord(data) {
		return false
	}
	reference, err := tlsgen.GenerateTLSClientHello(name)
	if err != nil {
		return false
	}
	got, want := walkTLSRecord(data), walkTLSRecord(reference)
	return len(got.cipherSuites) > 0 && len(got.extTypes) > 0 &&
		slices.Equal(got.cipherSuites, want.cipherSuites) &&
		slices.Equal(got.extTypes, want.extTypes)
}

func (h *helloWalk) fault(code string) {
	if !slices.Contains(h.faults, code) {
		h.faults = append(h.faults, code)
	}
}

func walkTLSRecord(data []byte) *helloWalk {
	h := &helloWalk{}
	rec := data[5:]
	if recLen := int(binary.BigEndian.Uint16(data[3:5])); recLen != len(rec) {
		h.fault("record")
		if recLen < len(rec) {
			rec = rec[:recLen]
		}
	}
	if len(rec) < 4 || rec[0] != 0x01 {
		h.fault("handshake")
		return h
	}
	body := rec[4:]
	if hsLen := int(rec[1])<<16 | int(rec[2])<<8 | int(rec[3]); hsLen != len(body) {
		h.fault("handshake")
		if hsLen < len(body) {
			body = body[:hsLen]
		}
	}
	if name, ok := sni.ParseTLSClientHelloBodySNI(body); ok {
		h.sni = name
	}
	h.walkBody(body)
	return h
}

func (h *helloWalk) walkBody(body []byte) {
	s := cryptobyte.String(body)
	if !s.Skip(2 + 32) {
		h.fault("handshake")
		return
	}
	var sid cryptobyte.String
	if !s.ReadUint8LengthPrefixed(&sid) || len(sid) > 32 {
		h.fault("session_id")
		return
	}
	var suites cryptobyte.String
	if !s.ReadUint16LengthPrefixed(&suites) || len(suites) == 0 || len(suites)%2 != 0 {
		h.fault("cipher_suites")
		return
	}
	for !suites.Empty() {
		var suite uint16
		suites.ReadUint16(&suite)
		h.cipherSuites = append(h.cipherSuites, suite)
	}
	var compression cryptobyte.String
	if !s.ReadUint8LengthPrefixed(&compression) || compression.Empty() {
		h.fault("handshake")
		return
	}
	if s.Empty() {
		return
	}
	var declared uint16
	if !s.ReadUint16(&declared) {
		h.fault("extensions")
		return
	}
	exts := s
	if int(declared) != len(s) {
		h.fault("extensions")
		if int(declared) < len(s) {
			exts = s[:declared]
		}
	}
	for !exts.Empty() {
		var typ uint16
		var data cryptobyte.String
		if !exts.ReadUint16(&typ) || !exts.ReadUint16LengthPrefixed(&data) {
			h.fault("extensions")
			return
		}
		h.extensions++
		h.extTypes = append(h.extTypes, typ)
		if typ == extECH {
			h.ech = true
		}
		if code, ok := extensionCodes[typ]; ok && !extensionWellFormed(typ, data) {
			h.fault(code)
		}
	}
}

const (
	extServerName           uint16 = 0x0000
	extSupportedGroups      uint16 = 0x000a
	extSignatureAlgorithms  uint16 = 0x000d
	extALPN                 uint16 = 0x0010
	extDelegatedCredentials uint16 = 0x0022
	extSupportedVersions    uint16 = 0x002b
	extKeyShare             uint16 = 0x0033
	extECH                  uint16 = 0xfe0d
)

var extensionCodes = map[uint16]string{
	extServerName:           "server_name",
	extSupportedGroups:      "supported_groups",
	extSignatureAlgorithms:  "signature_algorithms",
	extALPN:                 "alpn",
	extDelegatedCredentials: "delegated_credentials",
	extSupportedVersions:    "supported_versions",
	extKeyShare:             "key_share",
	extECH:                  "ech",
}

func extensionWellFormed(typ uint16, data cryptobyte.String) bool {
	switch typ {
	case extServerName:
		return serverNameWellFormed(data)
	case extSupportedGroups, extSignatureAlgorithms, extDelegatedCredentials:
		return uint16ListWellFormed(data)
	case extSupportedVersions:
		var list cryptobyte.String
		return data.ReadUint8LengthPrefixed(&list) && data.Empty() && !list.Empty() && len(list)%2 == 0
	case extALPN:
		return alpnWellFormed(data)
	case extKeyShare:
		return keyShareWellFormed(data)
	case extECH:
		return echWellFormed(data)
	}
	return true
}

func uint16ListWellFormed(data cryptobyte.String) bool {
	var list cryptobyte.String
	return data.ReadUint16LengthPrefixed(&list) && data.Empty() && !list.Empty() && len(list)%2 == 0
}

func serverNameWellFormed(data cryptobyte.String) bool {
	var list cryptobyte.String
	if !data.ReadUint16LengthPrefixed(&list) || !data.Empty() || list.Empty() {
		return false
	}
	for !list.Empty() {
		var nameType uint8
		var name cryptobyte.String
		if !list.ReadUint8(&nameType) || !list.ReadUint16LengthPrefixed(&name) || name.Empty() {
			return false
		}
	}
	return true
}

func alpnWellFormed(data cryptobyte.String) bool {
	var list cryptobyte.String
	if !data.ReadUint16LengthPrefixed(&list) || !data.Empty() || list.Empty() {
		return false
	}
	for !list.Empty() {
		var proto cryptobyte.String
		if !list.ReadUint8LengthPrefixed(&proto) || proto.Empty() {
			return false
		}
	}
	return true
}

var keyShareLengths = map[uint16]int{
	0x001d: 32,
	0x0017: 65,
	0x0018: 97,
	0x11ec: 1216,
}

func keyShareWellFormed(data cryptobyte.String) bool {
	var shares cryptobyte.String
	if !data.ReadUint16LengthPrefixed(&shares) || !data.Empty() {
		return false
	}
	for !shares.Empty() {
		var group uint16
		var key cryptobyte.String
		if !shares.ReadUint16(&group) || !shares.ReadUint16LengthPrefixed(&key) {
			return false
		}
		if want, known := keyShareLengths[group]; known && len(key) != want {
			return false
		}
		if group == 0x0017 {
			if _, err := ecdh.P256().NewPublicKey(key); err != nil {
				return false
			}
		}
	}
	return true
}

func echWellFormed(data cryptobyte.String) bool {
	var echType uint8
	if !data.ReadUint8(&echType) {
		return false
	}
	switch echType {
	case 0:
		var enc, payload cryptobyte.String
		return data.Skip(2+2+1) &&
			data.ReadUint16LengthPrefixed(&enc) &&
			data.ReadUint16LengthPrefixed(&payload) &&
			data.Empty()
	case 1:
		return data.Empty()
	}
	return false
}
