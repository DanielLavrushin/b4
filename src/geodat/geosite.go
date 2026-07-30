package geodat

import (
	"bufio"
	"io"
	"os"
	"strings"
)

type domainFunc func(tag string, kind uint64, value string) error

func streamGeoSite(file string, filters []string, emit domainFunc) error {
	want := make(map[string]struct{}, len(filters))
	for _, s := range filters {
		tag, _ := splitAttrs(s)
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
			kind, value, err := parseDomain(rec)
			if err != nil {
				return err
			}
			return emit(tag, kind, value)
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

func streamAllGeoSite(file string, emit domainFunc) error {
	var scratch []byte
	return scanEntries(file, func(tag string, body *entryBody) error {
		return scanRecords(body, &scratch, func(rec []byte) error {
			kind, value, err := parseDomain(rec)
			if err != nil {
				return err
			}
			return emit(tag, kind, value)
		})
	})
}

func UnpackGeoSite(args *UnpackArgs) error {
	w := bufio.NewWriterSize(os.Stdout, 64*1024)

	emit := func(_ string, kind uint64, value string) error {
		return writeDomainLine(w, kind, value)
	}

	var err error
	if len(args.Filters) != 0 {
		err = streamGeoSite(args.File, args.Filters, emit)
	} else {
		err = streamAllGeoSite(args.File, emit)
	}
	if err != nil {
		return err
	}
	return w.Flush()
}

func writeDomainLine(w io.StringWriter, kind uint64, value string) error {
	switch kind {
	case domainTypePlain:
		if _, err := w.WriteString("keyword:"); err != nil {
			return err
		}
	case domainTypeRegex:
		if _, err := w.WriteString("regexp:"); err != nil {
			return err
		}
	case domainTypeFull:
		if _, err := w.WriteString("full:"); err != nil {
			return err
		}
	}
	if _, err := w.WriteString(value); err != nil {
		return err
	}
	_, err := w.WriteString("\n")
	return err
}
