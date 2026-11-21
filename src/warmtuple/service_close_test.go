package warmtuple

import (
	"testing"
	"time"
)

func TestServiceCloseEndsAnActivePass(t *testing.T) {
	s, _, set, _ := serviceFixture(t)
	c := serviceDial(t, s, set)
	read := make(chan error, 1)
	go func() {
		_, err := c.Read(make([]byte, 1))
		read <- err
	}()
	select {
	case err := <-read:
		t.Fatalf("the pass ended before shutdown: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	s.Close()
	select {
	case err := <-read:
		if err == nil {
			t.Fatal("a pass read succeeded after shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("Service.Close left an active pass open")
	}
}
