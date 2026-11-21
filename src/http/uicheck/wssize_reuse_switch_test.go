package uicheck

import (
	"strings"
	"testing"
)

func TestReuseSwitchShowsWhileReuseIsOnWithWSSizeOff(t *testing.T) {
	src := readUI(t, "components/sets/tcp/TcpGeneral.tsx")
	const (
		reuseSwitch = `label={t("sets.tcp.general.wssizeReuse")}`
		gate        = `{(ws.enabled || reuse.enabled) && (`
	)
	at := strings.Index(src, reuseSwitch)
	if at < 0 {
		t.Fatal("TcpGeneral.tsx has no Reuse WSSIZE connection switch")
	}
	head := src[:at]
	opened := strings.LastIndex(head, gate)
	if opened < 0 {
		t.Fatalf("the reuse switch is not shown under %s, so a set with reuse on and WSSIZE off hides the switch and the reason b4 refuses to save it", gate)
	}
	if strings.Contains(head[opened:], ")}") {
		t.Errorf("the reuse switch no longer sits directly under %s", gate)
	}
	tail := src[at:]
	if end := strings.Index(tail, "/>"); end < 0 || !strings.Contains(tail[:end], "reuseMessages") {
		t.Error("the reuse switch does not show the reuse issues as its message")
	}
}
