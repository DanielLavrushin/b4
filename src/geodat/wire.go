package geodat

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const (
	wireVarint  = 0
	wireFixed64 = 1
	wireBytes   = 2
	wireFixed32 = 5
)

const (
	keyEntry       = 0x0A
	keyCountryCode = 0x0A
	fieldRecord    = 2
)

const (
	domainTypePlain = iota
	domainTypeRegex
	domainTypeDomain
	domainTypeFull
)

const (
	scanBufferSize    = 64 * 1024
	maxCountryCodeLen = 256
	maxRecordLen      = 1 << 20
)

var (
	errStopScan  = errors.New("geodat: stop scan")
	errMalformed = errors.New("geodat: malformed record")
	errOverflow  = errors.New("geodat: varint overflows 64 bits")
)

type entryBody struct {
	br *bufio.Reader
	n  int64
}

func (e *entryBody) remaining() int64 { return e.n }

func (e *entryBody) ReadByte() (byte, error) {
	if e.n <= 0 {
		return 0, io.ErrUnexpectedEOF
	}
	b, err := e.br.ReadByte()
	if err != nil {
		return 0, err
	}
	e.n--
	return b, nil
}

func (e *entryBody) readFull(p []byte) error {
	if int64(len(p)) > e.n {
		return io.ErrUnexpectedEOF
	}
	if _, err := io.ReadFull(e.br, p); err != nil {
		return err
	}
	e.n -= int64(len(p))
	return nil
}

func (e *entryBody) skip(n int64) error {
	if n > e.n {
		return io.ErrUnexpectedEOF
	}
	for n > 0 {
		chunk := n
		if chunk > math.MaxInt32 {
			chunk = math.MaxInt32
		}
		got, err := e.br.Discard(int(chunk))
		e.n -= int64(got)
		n -= int64(got)
		if err != nil {
			return err
		}
	}
	return nil
}

func (e *entryBody) drain() error { return e.skip(e.n) }

func readUvarint(r io.ByteReader) (uint64, int, error) {
	var x uint64
	var s uint
	for i := 0; i < binary.MaxVarintLen64; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, i, err
		}
		if b < 0x80 {
			if i == binary.MaxVarintLen64-1 && b > 1 {
				return 0, i + 1, errOverflow
			}
			return x | uint64(b)<<s, i + 1, nil
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, binary.MaxVarintLen64, errOverflow
}

func skipWireValue(body *entryBody, wire byte) error {
	switch wire {
	case wireVarint:
		_, _, err := readUvarint(body)
		return err
	case wireFixed64:
		return body.skip(8)
	case wireBytes:
		size, _, err := readUvarint(body)
		if err != nil {
			return err
		}
		return body.skip(int64(size))
	case wireFixed32:
		return body.skip(4)
	default:
		return errMalformed
	}
}

func scanEntries(path string, fn func(tag string, body *entryBody) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	br := bufio.NewReaderSize(f, scanBufferSize)
	left := fi.Size()
	var ccBuf [maxCountryCodeLen]byte

	for {
		b, err := br.ReadByte()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		left--
		if b != keyEntry {
			return log.Errorf("unexpected wire tag %02X", b)
		}

		size, n, err := readUvarint(br)
		if err != nil {
			return log.Errorf("failed to read varint: %w", err)
		}
		left -= int64(n)
		if size > uint64(left) {
			return log.Errorf("entry size %d exceeds %d bytes left in %s", size, left, path)
		}
		left -= int64(size)

		body := &entryBody{br: br, n: int64(size)}
		tag, err := readCountryCode(body, ccBuf[:])
		if err != nil {
			return err
		}

		if err := fn(tag, body); err != nil {
			if errors.Is(err, errStopScan) {
				return nil
			}
			return err
		}
		if err := body.drain(); err != nil {
			return err
		}
	}
}

func readCountryCode(body *entryBody, buf []byte) (string, error) {
	b, err := body.ReadByte()
	if err != nil || b != keyCountryCode {
		return "", log.Errorf("bad key")
	}
	size, _, err := readUvarint(body)
	if err != nil {
		return "", log.Errorf("bad varint")
	}
	if size > uint64(len(buf)) || int64(size) > body.remaining() {
		return "", log.Errorf("string truncated")
	}
	p := buf[:size]
	if err := body.readFull(p); err != nil {
		return "", log.Errorf("string truncated")
	}
	return strings.ToLower(string(p)), nil
}

func scanRecords(body *entryBody, scratch *[]byte, fn func(rec []byte) error) error {
	for body.remaining() > 0 {
		key, _, err := readUvarint(body)
		if err != nil {
			return err
		}
		field := key >> 3
		wire := byte(key & 0x07)

		if field != fieldRecord || wire != wireBytes {
			if err := skipWireValue(body, wire); err != nil {
				return err
			}
			continue
		}

		size, _, err := readUvarint(body)
		if err != nil {
			return err
		}
		if size > maxRecordLen {
			return log.Errorf("record size %d exceeds limit %d", size, maxRecordLen)
		}
		if int64(size) > body.remaining() {
			return io.ErrUnexpectedEOF
		}

		if cap(*scratch) < int(size) {
			*scratch = make([]byte, size)
		}
		rec := (*scratch)[:size]
		if err := body.readFull(rec); err != nil {
			return err
		}
		if err := fn(rec); err != nil {
			return err
		}
	}
	return nil
}

func skipBytesValue(b []byte, wire byte) ([]byte, error) {
	switch wire {
	case wireVarint:
		_, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, errMalformed
		}
		return b[n:], nil
	case wireFixed64:
		if len(b) < 8 {
			return nil, errMalformed
		}
		return b[8:], nil
	case wireBytes:
		size, n := binary.Uvarint(b)
		if n <= 0 || uint64(len(b)-n) < size {
			return nil, errMalformed
		}
		return b[n+int(size):], nil
	case wireFixed32:
		if len(b) < 4 {
			return nil, errMalformed
		}
		return b[4:], nil
	default:
		return nil, errMalformed
	}
}

func parseDomain(b []byte) (uint64, string, error) {
	var kind uint64
	var value string

	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return 0, "", errMalformed
		}
		b = b[n:]
		field := key >> 3
		wire := byte(key & 0x07)

		switch {
		case field == 1 && wire == wireVarint:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return 0, "", errMalformed
			}
			kind = v
			b = b[n:]
		case field == 2 && wire == wireBytes:
			size, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < size {
				return 0, "", errMalformed
			}
			value = string(b[n : n+int(size)])
			b = b[n+int(size):]
		default:
			rest, err := skipBytesValue(b, wire)
			if err != nil {
				return 0, "", err
			}
			b = rest
		}
	}
	return kind, value, nil
}

func parseCIDR(b []byte) ([]byte, int, error) {
	var ip []byte
	var prefix int

	for len(b) > 0 {
		key, n := binary.Uvarint(b)
		if n <= 0 {
			return nil, 0, errMalformed
		}
		b = b[n:]
		field := key >> 3
		wire := byte(key & 0x07)

		switch {
		case field == 1 && wire == wireBytes:
			size, n := binary.Uvarint(b)
			if n <= 0 || uint64(len(b)-n) < size {
				return nil, 0, errMalformed
			}
			ip = b[n : n+int(size)]
			b = b[n+int(size):]
		case field == 2 && wire == wireVarint:
			v, n := binary.Uvarint(b)
			if n <= 0 {
				return nil, 0, errMalformed
			}
			prefix = int(v)
			b = b[n:]
		default:
			rest, err := skipBytesValue(b, wire)
			if err != nil {
				return nil, 0, err
			}
			b = rest
		}
	}
	return ip, prefix, nil
}
