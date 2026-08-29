package handler

import (
	"fmt"
	"os"
)

// executableFingerprint identifies the file this process was started from. Version is
// stamped in at build time, so a running b4 keeps reporting the version it was compiled
// as no matter what replaces its file on disk. Comparing the file against what it was at
// start-up is what tells the two apart.
func executableFingerprint() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}

	fi, err := os.Stat(exe)
	if err != nil {
		return ""
	}

	return fmt.Sprintf("%d:%d", fi.Size(), fi.ModTime().UnixNano())
}

var executableFingerprintAtStart = executableFingerprint()

// binaryReplaced reports whether the file this process was started from has changed since.
// It stays false when the path cannot be read at all, which is the case in a container
// without /proc, because then there is nothing to compare and a guess would be worse.
func binaryReplaced() bool {
	if executableFingerprintAtStart == "" {
		return false
	}

	now := executableFingerprint()
	return now != "" && now != executableFingerprintAtStart
}
