package geodat

import (
	"bufio"
	"net/netip"
	"os"
	"strings"
)

type cidrFunc func(tag string, prefix netip.Prefix) error

func toPrefix(ip []byte, bits int) (netip.Prefix, bool) {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return netip.Prefix{}, false
	}
	prefix, err := addr.Prefix(bits)
	if err != nil {
		return netip.Prefix{}, false
	}
	return prefix, true
}

func streamGeoIP(file string, filters []string, emit cidrFunc) error {
	want := make(map[string]struct{}, len(filters))
	for _, tag := range filters {
		want[strings.ToLower(tag)] = struct{}{}
	}
	if len(want) == 0 {
		return nil
	}

	got := make(map[string]struct{}, len(want))
	var scratch []byte

	return scanEntries(file, func(tag string, body *entryBody) error {
		if _, ok := want[tag]; !ok {
			return nil
		}
		err := scanRecords(body, &scratch, func(rec []byte) error {
			ip, bits, err := parseCIDR(rec)
			if err != nil {
				return err
			}
			prefix, ok := toPrefix(ip, bits)
			if !ok {
				return nil
			}
			return emit(tag, prefix)
		})
		if err != nil {
			return err
		}
		got[tag] = struct{}{}
		if len(got) == len(want) {
			return errStopScan
		}
		return nil
	})
}

func streamAllGeoIP(file string, emit cidrFunc) error {
	var scratch []byte
	return scanEntries(file, func(tag string, body *entryBody) error {
		return scanRecords(body, &scratch, func(rec []byte) error {
			ip, bits, err := parseCIDR(rec)
			if err != nil {
				return err
			}
			prefix, ok := toPrefix(ip, bits)
			if !ok {
				return nil
			}
			return emit(tag, prefix)
		})
	})
}

func UnpackGeoIP(args *UnpackArgs) error {
	w := bufio.NewWriterSize(os.Stdout, 64*1024)

	emit := func(_ string, prefix netip.Prefix) error {
		if _, err := w.WriteString(prefix.String()); err != nil {
			return err
		}
		_, err := w.WriteString("\n")
		return err
	}

	var err error
	if len(args.Filters) != 0 {
		err = streamGeoIP(args.File, args.Filters, emit)
	} else {
		err = streamAllGeoIP(args.File, emit)
	}
	if err != nil {
		return err
	}
	return w.Flush()
}
