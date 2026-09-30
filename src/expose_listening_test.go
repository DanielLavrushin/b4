package main

import (
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestExposeListeningOnlyKeepsPortsB4ActuallyHolds(t *testing.T) {
	mtproto := config.ExposedPort{Service: config.ExposeMTProto, Port: 3128, V4: true, V6: true}
	socks := config.ExposedPort{Service: config.ExposeSocks5, Port: 1080, V4: true, V6: true}
	prior := []config.ExposeBlock{{Service: config.ExposeWebServer, Reason: config.ExposeBlockedNoAuth}}

	ports, blocked := exposeListeningOnly([]config.ExposedPort{mtproto, socks}, prior, func(service string) bool {
		return service == config.ExposeMTProto
	})

	if !reflect.DeepEqual(ports, []config.ExposedPort{mtproto}) {
		t.Fatalf("a port whose listener failed to bind may belong to another program and must stay closed, got %+v", ports)
	}
	want := append(prior, config.ExposeBlock{Service: config.ExposeSocks5, Reason: config.ExposeBlockedNotListening})
	if !reflect.DeepEqual(blocked, want) {
		t.Fatalf("the closed port must be reported, got %+v", blocked)
	}
}
