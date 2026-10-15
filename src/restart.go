package main

import (
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/daniellavrushin/b4/log"
)

type restartKind int32

const (
	restartNone restartKind = iota
	restartManual
	restartEngineRetry
)

var (
	restartRequests = make(chan restartKind, 1)
	pendingRestart  atomic.Int32
	execSelf        = syscall.Exec
)

func requestRestart(kind restartKind) {
	select {
	case restartRequests <- kind:
	default:
	}
}

func restartIfRequested() error {
	kind := restartKind(pendingRestart.Swap(int32(restartNone)))
	if kind == restartNone {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		exe = "/proc/self/exe"
	}
	env := restartEnv(os.Environ(), kind, engineAttempt)
	log.Flush()
	_ = log.SetErrorFile("")
	signal.Ignore(syscall.SIGUSR1)
	return fmt.Errorf("b4 could not restart itself: %w", execSelf(exe, os.Args, env))
}

func restartEnv(environ []string, kind restartKind, attempt int) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, engineRetryEnv+"=") {
			out = append(out, kv)
		}
	}
	if kind == restartEngineRetry {
		out = append(out, fmt.Sprintf("%s=%d", engineRetryEnv, attempt+1))
	}
	return out
}
