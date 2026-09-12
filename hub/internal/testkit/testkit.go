package testkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/asn"
)

const PayloadFile = "captures/tls_www_google_com.bin"

var ErrNoPayload = errors.New("no such payload")

func ReadPayload(name string) ([]byte, error) {
	if name == PayloadFile {
		return config.FakeSNI1, nil
	}
	return nil, ErrNoPayload
}

func SampleSet(name string, domains ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = name
	set.Targets.SNIDomains = append([]string{}, domains...)
	set.Faking.SNI = true
	set.Faking.TTL = 7
	set.Faking.SNIType = config.FakePayloadCapture
	set.Faking.PayloadFile = PayloadFile
	set.Fragmentation.Strategy = "tls"
	return set
}

func BuildEnvelope(t *testing.T, set *config.SetConfig) *hubwire.Envelope {
	t.Helper()
	env, _, err := hubwire.Build(set, hubwire.BuildOptions{B4Version: "1.82.0", Engine: "nfqueue", ReadPayload: ReadPayload})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func SignShare(t *testing.T, id *hubwire.Identity, env *hubwire.Envelope, now time.Time) []byte {
	t.Helper()
	body := hubwire.ShareBody{Envelope: *env, ASNHint: "hint", CountryHint: "xx", Engine: "nfqueue", B4Version: "1.82.0"}
	return Sign(t, id, hubwire.RecordShare, body, now)
}

func Sign(t *testing.T, id *hubwire.Identity, kind string, body interface{}, now time.Time) []byte {
	t.Helper()
	rec, err := hubwire.SignRecord(id, kind, body, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func Identity(t *testing.T) *hubwire.Identity {
	t.Helper()
	id, err := hubwire.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type Origin struct {
	ASN     string
	Country string
	Name    string
}

func CymruLookup(byIP map[string]Origin) asn.LookupTXT {
	return func(_ context.Context, name string) ([]string, error) {
		switch {
		case strings.HasSuffix(name, ".origin.asn.cymru.com"):
			labels := strings.Split(strings.TrimSuffix(name, ".origin.asn.cymru.com"), ".")
			if len(labels) != 4 {
				return nil, fmt.Errorf("unexpected name %s", name)
			}
			ip := labels[3] + "." + labels[2] + "." + labels[1] + "." + labels[0]
			o, ok := byIP[ip]
			if !ok {
				return nil, fmt.Errorf("no origin for %s", ip)
			}
			return []string{fmt.Sprintf("%s | %s/24 | %s | ripencc | 2020-01-01", o.ASN, ip, o.Country)}, nil
		case strings.HasPrefix(name, "AS") && strings.HasSuffix(name, ".asn.cymru.com"):
			number := strings.TrimSuffix(strings.TrimPrefix(name, "AS"), ".asn.cymru.com")
			for _, o := range byIP {
				if o.ASN == number {
					return []string{fmt.Sprintf("%s | %s | ripencc | 2020-01-01 | %s", o.ASN, o.Country, o.Name)}, nil
				}
			}
		}
		return nil, fmt.Errorf("no answer for %s", name)
	}
}
