package config

import "sync"

var writeMu sync.Mutex

func LockWrites() (unlock func()) {
	writeMu.Lock()
	var once sync.Once
	return func() { once.Do(writeMu.Unlock) }
}
