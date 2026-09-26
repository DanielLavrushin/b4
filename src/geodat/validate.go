package geodat

import (
	"errors"
	"fmt"
)

type Kind int

const (
	KindSite Kind = iota
	KindIP
)

func (k Kind) String() string {
	if k == KindIP {
		return "GeoIP"
	}
	return "GeoSite"
}

func (k Kind) FileName() string {
	if k == KindIP {
		return "geoip.dat"
	}
	return "geosite.dat"
}

const validateSampleRecords = 64

var ErrUnusable = errors.New("not a usable geodata file")

func Validate(path string, kind Kind) error {
	sampled := 0
	usable := false
	var scratch []byte
	err := scanEntries(path, func(_ string, body *entryBody) error {
		return scanRecords(body, &scratch, func(rec []byte) error {
			if !usable && sampled < validateSampleRecords {
				sampled++
				usable = recordUsable(kind, rec)
			}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnusable, err)
	}
	if !usable {
		return fmt.Errorf("%w: no %s records found in %s", ErrUnusable, kind, path)
	}
	return nil
}

func recordUsable(kind Kind, rec []byte) bool {
	if kind == KindIP {
		ip, bits, err := parseCIDR(rec)
		if err != nil {
			return false
		}
		_, ok := toPrefix(ip, bits)
		return ok
	}
	_, value, err := parseDomain(rec)
	return err == nil && value != ""
}
