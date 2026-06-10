package netprobe

import (
	"testing"
	"time"
)

func TestMarkControlZero(t *testing.T) {
	if MarkControl(0) != nil {
		t.Fatal("MarkControl(0) must be nil so no SO_MARK is set")
	}
	if MarkControl(42) == nil {
		t.Fatal("MarkControl(non-zero) must install a control func")
	}
}

func TestDialerControl(t *testing.T) {
	if Dialer(0, time.Second, 0).Control != nil {
		t.Fatal("Dialer(0,...) must have nil Control")
	}
	if Dialer(7, time.Second, time.Second).Control == nil {
		t.Fatal("Dialer(non-zero,...) must have Control")
	}
}
