package ai

import "testing"

func TestFakePayloadTopicDescribesTheVKPreset(t *testing.T) {
	checkTopicWording(t, "faking.sni_type",
		[]string{"3 'vk' (a pre-baked 517-byte ClientHello for vk.com)"},
		[]string{"duckduckgo", "staticcdn"})
}

func TestFakePayloadTopicNamesOnlyTheWaysAPayloadFileIsMade(t *testing.T) {
	checkTopicWording(t, "faking.sni_type",
		[]string{"a generated ClientHello, an uploaded file, or the fake data of an imported zapret or byedpi command line"},
		[]string{"captured"})
}

func TestTLSModTopicSaysRndRedrawsTheSessionID(t *testing.T) {
	checkTopicWording(t, "faking.tls_mod",
		[]string{
			"'rnd' gives each fake a new random 32-byte Random and a new random session ID of the same length",
			"'dupsid' copies the session ID of the real ClientHello",
			"rnd runs first and the real session ID wins",
			"'random', 'zero', 'inverted' and 'stun'",
		},
		[]string{"bypass"})
}

func TestFakeLengthTopicKeepsRndOnAMirroredFake(t *testing.T) {
	checkTopicWording(t, "faking.fake_len_mode",
		[]string{"a mirrored fake still gets a new Random"},
		[]string{"tls_mod is ignored for a mirrored fake"})
}

func TestUDPFakeLengthTopicStatesTheOnePacketCap(t *testing.T) {
	checkTopicWording(t, "udp.fake_len",
		[]string{"32-1472", "lowers a larger one to 1472", "refuses a negative value", "1452"},
		[]string{"32-1500", "nothing in validation clamps it"})
}
