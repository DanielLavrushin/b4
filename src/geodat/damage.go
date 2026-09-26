package geodat

import (
	"errors"
	"fmt"
	"io"
	"sync"
)

var ErrEmpty = errors.New("file is empty")

type DamageError struct {
	Path string
	Err  error
}

func (e *DamageError) Error() string {
	return fmt.Sprintf("%s is damaged: %v", e.Path, e.Err)
}

func (e *DamageError) Unwrap() error { return e.Err }

func isRecordDamage(err error) bool {
	return errors.Is(err, errMalformed) || errors.Is(err, errOverflow) || errors.Is(err, io.ErrUnexpectedEOF)
}

var damagedFiles sync.Map

func track(path string, err error) error {
	var damage *DamageError
	if errors.As(err, &damage) {
		if stamp, ok := fileStamp(path); ok {
			damagedFiles.Store(path, stamp)
		}
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
