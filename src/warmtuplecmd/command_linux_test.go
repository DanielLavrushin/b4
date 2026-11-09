package warmtuplecmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/leaktest"
	"github.com/spf13/cobra"
)

func TestMain(m *testing.M) {
	leaktest.VerifyTestMain(m)
}

func validArgs() []string {
	return []string{"--host=warm.example.org", "--target=203.0.113.2:443", "--source=203.0.113.1", "--first-port=40000", "--last-port=40000", "--queue=541"}
}

func execute(args ...string) error {
	c := NewCommand()
	c.SetOut(io.Discard)
	c.SetErr(io.Discard)
	c.SetArgs(args)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return c.ExecuteContext(ctx)
}

func TestCommandRefusesMissingAndInvalidArgumentsBeforeKernelSetup(t *testing.T) {
	if err := execute(); err == nil || !strings.Contains(err.Error(), "required flag") {
		t.Fatal("required flags missing:", err)
	}
	for _, invalid := range []string{
		"--queue=0", "--queue=65536", "--first-port=65536", "--last-port=39999", "--last-port=40032",
		"--duration=0", "--duration=6m", "--listen=0.0.0.0:1089", "--listen=[::1]:1089", "--listen=127.0.0.1:0",
		"--target=[::1]:443", "--target=203.0.113.2:0", "--source=::1", "--host=203.0.113.2", "--host=bad\r\nhost",
		"unexpected-position-argument", "--unknown=true",
	} {
		if err := execute(append(validArgs(), invalid)...); err == nil || strings.Contains(err.Error(), "namespace") || strings.Contains(err.Error(), "canceled") {
			t.Errorf("%q accepted or reached kernel setup: %v", invalid, err)
		}
	}
}

func TestCommandRefusesInitialNamespace(t *testing.T) {
	self, _ := os.Readlink("/proc/self/ns/net")
	init, _ := os.Readlink("/proc/1/ns/net")
	if self == "" || self != init {
		t.Skip("already outside the initial namespace")
	}
	if err := execute(validArgs()...); err == nil || !strings.Contains(err.Error(), "isolated Linux network namespace") {
		t.Fatal("command did not stop before opening sockets/firewall state:", err)
	}
}

func TestCommandHasIndependentFlagsAndNoParentServiceSideEffects(t *testing.T) {
	for range 2 {
		var output bytes.Buffer
		root := &cobra.Command{Use: "b4", RunE: func(*cobra.Command, []string) error { t.Fatal("normal service started"); return nil }}
		root.AddCommand(NewCommand())
		root.SetOut(&output)
		root.SetErr(&output)
		root.SetArgs([]string{"warm-tuple", "--help"})
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "--first-port") || !strings.Contains(output.String(), "--queue") {
			t.Fatal("command help missing selector/queue flags")
		}
	}
	o := options{host: "warm.example.org", target: "203.0.113.2:443", source: "203.0.113.1", listen: "127.0.0.1:1089", first: 40000, last: 40000, queue: 541, duration: time.Minute}
	profile, proxy, err := o.parse()
	if err != nil || !proxy.Enabled || proxy.BindAddress != "127.0.0.1" || proxy.Port != 1089 || profile.FirstPort != 40000 || profile.PassLifetime != time.Minute {
		t.Fatalf("unexpected native command config: %+v %+v %v", profile, proxy, err)
	}
}
