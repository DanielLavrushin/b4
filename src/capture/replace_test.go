package capture

import (
	"bytes"
	"errors"
	"testing"
)

func TestAnUploadReplacesAnExistingNameOnlyWhenAsked(t *testing.T) {
	m := testManager(t)
	const name = "replace.upload.example"
	first, second := []byte{0x16, 0x03, 0x01, 0x01}, []byte{0x16, 0x03, 0x01, 0x02}
	if err := m.SaveUploadedCapture("tls", name, first, false); err != nil {
		t.Fatalf("a first upload under a new name failed: %v", err)
	}
	c, _ := m.GetCapture("tls", name)

	if err := m.SaveUploadedCapture("tls", name, second, false); !errors.Is(err, ErrPayloadExists) {
		t.Errorf("an upload over an existing name without replace answered %v, want ErrPayloadExists", err)
	}
	if got, _ := m.LoadCaptureData(c); !bytes.Equal(got, first) {
		t.Errorf("a refused upload changed the file to % x", got)
	}
	if err := m.SaveUploadedCapture("quic", name, second, false); err != nil {
		t.Errorf("the same name under the other protocol is a payload of its own, got %v", err)
	}
	if err := m.SaveUploadedCapture("tls", name, second, true); err != nil {
		t.Errorf("an upload asked to replace failed: %v", err)
	}
	if got, _ := m.LoadCaptureData(c); !bytes.Equal(got, second) {
		t.Errorf("an upload asked to replace left % x in the file", got)
	}
}
