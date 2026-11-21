package sock

import (
	"bytes"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func TestAMirroredFakeNeverCarriesTheRealHostName(t *testing.T) {
	cases := []struct {
		host     string
		template []byte
	}{
		{"google.com", config.FakeSNI1},
		{"duckduckgo.com", realHello(t, "staticcdn.duckduckgo.com")},
		{"vk.com", config.FakeSNI2},
	}
	for _, c := range cases {
		t.Run(c.host, func(t *testing.T) {
			real := realHello(t, c.host)
			for range 300 {
				fake := MatchPayloadLength(cloneBytes(c.template), real, "match")
				if len(fake) != len(real) {
					t.Fatalf("the mirrored fake is %d bytes, the real hello %d", len(fake), len(real))
				}
				got, _, ok := sni.ParseTLSClientHelloSNI(fake)
				if !ok {
					t.Fatal("the mirrored fake has no SNI")
				}
				if strings.EqualFold(got, c.host) {
					t.Fatalf("the match-mode fake names the real host %q, identical to the real hello: %v", got, bytes.Equal(fake, real))
				}
				assertHostShape(t, got, len(c.host))
			}
		})
	}
}
