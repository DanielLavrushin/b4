package geodat

import (
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

var (
	ErrEmpty = errors.New("file is empty")
	ErrBusy  = errors.New("another geodata download is already running")
)

type DamageError struct {
	Path string
	Err  error
}

func (e *DamageError) Error() string {
	return fmt.Sprintf("%s is damaged: %v", e.Path, e.Err)
}

func (e *DamageError) Unwrap() error { return e.Err }

type DownloadError struct {
	Kind Kind
	Err  error
}

func (e *DownloadError) Error() string {
	return fmt.Sprintf("failed to download %s: %v", e.Kind.FileName(), e.Err)
}

func (e *DownloadError) Unwrap() error { return e.Err }

func isRecordDamage(err error) bool {
	return errors.Is(err, errMalformed) || errors.Is(err, errOverflow) || errors.Is(err, io.ErrUnexpectedEOF)
}

var damagedFiles sync.Map

func track(path, before string, stamped bool, err error) error {
	var damage *DamageError
	if !stamped || !errors.As(err, &damage) {
		return err
	}
	if after, ok := fileStamp(path); !ok || after != before {
		return err
	}
	if prev, loaded := damagedFiles.Swap(path, before); !loaded || prev != before {
		log.Errorf("%v", err)
	}
	return err
}

func IsDamaged(path string) bool {
	seen, ok := damagedFiles.Load(path)
	if !ok {
		return false
	}
	if stamp, ok := fileStamp(path); ok && stamp == seen {
		return true
	}
	damagedFiles.CompareAndDelete(path, seen)
	return false
}
