package hubwire

import (
	"errors"
	"reflect"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const l7Ports = "19294-19344,50000-50100"

func l7OnlySet(classes ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = "Discord voice"
	set.UDP.DPortFilter = l7Ports
	set.UDP.FakePayloadFile = config.FakePayloadAutoSTUN
	set.UDP.FilterL7 = classes
	return set
}

func withoutKnownPath(t *testing.T, path string) {
	t.Helper()
	known, ok := setPaths[path]
	if !ok {
		t.Fatalf("%s is not a set path", path)
	}
	delete(setPaths, path)
	t.Cleanup(func() { setPaths[path] = known })
}

func validationFields(t *testing.T, err error) []config.ValidationField {
	t.Helper()
	var invalid *config.ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected a validation error, got %v", err)
	}
	return invalid.Fields
}

func TestScrubCountsUDPProtocolSelectionAsATarget(t *testing.T) {
	set := l7OnlySet(config.UDPL7Discord, config.UDPL7STUN)
	projection, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := warningCodes(report.Warnings)["no_targets"]; ok {
		t.Errorf("a set that selects UDP traffic by protocol selects something: %+v", report.Warnings)
	}
	if got := UDPL7(projection); !reflect.DeepEqual(got, []string{config.UDPL7Discord, config.UDPL7STUN}) {
		t.Errorf("the projection must carry the protocol classes, got %v", got)
	}
	if v := MinVersion(projection); v != "1.88.0" {
		t.Errorf("a set that selects UDP protocols needs the release that reads them, got %s", v)
	}

	for name, classes := range map[string][]string{"no classes": nil, "blank classes": {" ", ""}} {
		plain := l7OnlySet(classes...)
		_, report, err := Scrub(&plain)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := warningCodes(report.Warnings)["no_targets"]; !ok {
			t.Errorf("%s: a set with a UDP port filter and nothing else targets nothing: %+v", name, report.Warnings)
		}
	}
}

func TestScrubCountsOnlyTheProtocolsThisBuildMatches(t *testing.T) {
	unknown := l7OnlySet("wireguard")
	_, report, err := Scrub(&unknown)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := warningCodes(report.Warnings)["no_targets"]; !ok {
		t.Errorf("a set that selects only protocols this build does not match targets nothing: %+v", report.Warnings)
	}

	mixed := l7OnlySet("wireguard", config.UDPL7Discord)
	projection, report, err := Scrub(&mixed)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := warningCodes(report.Warnings)["no_targets"]; ok {
		t.Errorf("a known protocol next to an unknown one still selects something: %+v", report.Warnings)
	}
	if got := UDPL7(projection); !reflect.DeepEqual(got, []string{config.UDPL7Discord, "wireguard"}) {
		t.Errorf("the projection must keep every protocol the set names, got %v", got)
	}
}

func TestBuildOfAGeneratedSTUNDecoyAsksForTheReleaseThatSendsIt(t *testing.T) {
	for _, c := range []struct {
		payload string
		want    string
	}{
		{config.FakePayloadAutoSTUN, "1.88.0"},
		{"", BaselineVersion},
		{config.FakePayloadAutoQUIC, BaselineVersion},
	} {
		set := config.NewSetConfig()
		set.Name = "Calls"
		set.Targets.SNIDomains = []string{"example.com"}
		set.UDP.Mode = config.UDPModeFake
		set.UDP.FakePayloadFile = c.payload
		env, _, err := Build(&set, BuildOptions{B4Version: "1.88.0"})
		if err != nil {
			t.Fatal(err)
		}
		if env.MinB4 != c.want {
			t.Errorf("a set whose UDP fake payload is %q needs b4 %s, got %s", c.payload, c.want, env.MinB4)
		}
	}
}

func TestBuildOfAnL7SetAsksForTheReleaseThatReadsIt(t *testing.T) {
	set := l7OnlySet(config.UDPL7Discord)
	env, report, err := Build(&set, BuildOptions{B4Version: "1.88.0"})
	if err != nil {
		t.Fatal(err)
	}
	if env.MinB4 != "1.88.0" {
		t.Errorf("an envelope that selects UDP protocols must ask for 1.88.0, got %s", env.MinB4)
	}
	if len(report.Warnings) != 0 {
		t.Errorf("a valid protocol selection needs no warning: %+v", report.Warnings)
	}

	imp, err := Open(env, OpenOptions{B4Version: "1.88.0"})
	if err != nil {
		t.Fatal(err)
	}
	codes := warningCodes(imp.Warnings)
	for _, code := range []string{"unknown_fields", "values_changed", "no_targets", "version_too_old", "fingerprint_mismatch"} {
		if _, ok := codes[code]; ok {
			t.Errorf("unexpected %s on a clean round trip: %+v", code, imp.Warnings)
		}
	}
	if !imp.Set.SelectsL7Class(config.UDPL7Discord) || imp.Set.UDP.DPortFilter != l7Ports {
		t.Errorf("the protocol selection and its ports must cross: %+v", imp.Set.UDP)
	}
	if imp.Fingerprint != env.Fingerprint {
		t.Errorf("fingerprint drifted: %s vs %s", imp.Fingerprint, env.Fingerprint)
	}

	old, err := Open(env, OpenOptions{B4Version: "1.87.0"})
	if err != nil {
		t.Fatal(err)
	}
	if w, ok := warningCodes(old.Warnings)["version_too_old"]; !ok || w.Params["min"] != "1.88.0" {
		t.Errorf("a release before protocol selection must be told it is too old: %+v", old.Warnings)
	}
}

func TestARouterWithoutTheL7FieldSeesATargetlessSet(t *testing.T) {
	set := l7OnlySet(config.UDPL7Discord)
	env, _, err := Build(&set, BuildOptions{B4Version: "1.88.0"})
	if err != nil {
		t.Fatal(err)
	}
	withoutKnownPath(t, "udp.filter_l7")

	imp, err := Open(env, OpenOptions{B4Version: "1.87.0"})
	if err != nil {
		t.Fatal(err)
	}
	codes := warningCodes(imp.Warnings)
	unknown, ok := codes["unknown_fields"]
	if !ok || !reflect.DeepEqual(unknown.Params["paths"], []string{"udp.filter_l7"}) {
		t.Errorf("a router that does not know the field must drop it: %+v", imp.Warnings)
	}
	if _, ok := codes["no_targets"]; !ok {
		t.Errorf("without the field the set targets nothing, which the apply path refuses: %+v", imp.Warnings)
	}
	if imp.Set.SelectsL7() || imp.Set.DeclaresDestinationTargets() || imp.Set.UDP.DPortFilter != l7Ports {
		t.Errorf("what remains is a targetless set with UDP ports: %+v %+v", imp.Set.UDP, imp.Set.Targets)
	}
}

func TestOpenRefusesL7SetsARouterWouldNotSave(t *testing.T) {
	noPorts := l7OnlySet(config.UDPL7Discord)
	noPorts.UDP.DPortFilter = ""
	targeted := l7OnlySet(config.UDPL7STUN)
	targeted.Targets.SNIDomains = []string{"discord.com"}
	withTCP := l7OnlySet(config.UDPL7Discord)
	withTCP.TCP.DPortFilter = "443,2053"

	for _, c := range []struct {
		name string
		set  config.SetConfig
		path string
		code string
	}{
		{"without ports", noPorts, "sets[0].udp.dport_filter", "required"},
		{"with targets", targeted, "sets[0].udp.filter_l7", "conflict"},
		{"with TCP ports", withTCP, "sets[0].tcp.dport_filter", "conflict"},
	} {
		t.Run(c.name, func(t *testing.T) {
			env, _, err := Build(&c.set, BuildOptions{B4Version: "1.88.0"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = Open(env, OpenOptions{B4Version: "1.88.0"})
			if err == nil {
				t.Fatal("an envelope every router refuses to save must not open")
			}
			fields := validationFields(t, err)
			if len(fields) != 1 || fields[0].Path != c.path || fields[0].Code != c.code {
				t.Errorf("expected %s on %s, got %+v", c.code, c.path, fields)
			}
		})
	}
}

func TestScrubFlagsAProtocolSelectionThatAlsoFiltersTCPPorts(t *testing.T) {
	set := l7OnlySet(config.UDPL7Discord)
	set.TCP.DPortFilter = "443,2053"
	_, report, err := Scrub(&set)
	if err != nil {
		t.Fatal(err)
	}
	codes := warningCodes(report.Warnings)
	if w, ok := codes["udp_l7_tcp_ports"]; !ok || w.Params["ports"] != "443,2053" {
		t.Errorf("without targets the TCP ports of a protocol selection catch every TCP connection on them: %+v", report.Warnings)
	}
	if _, ok := codes["no_targets"]; ok {
		t.Errorf("the protocol selection is still a selector, the TCP ports are the problem: %+v", report.Warnings)
	}

	plain := l7OnlySet()
	plain.TCP.DPortFilter = "443"
	_, report, err = Scrub(&plain)
	if err != nil {
		t.Fatal(err)
	}
	codes = warningCodes(report.Warnings)
	if _, ok := codes["no_targets"]; !ok {
		t.Errorf("a set with port filters and nothing else targets nothing, as before: %+v", report.Warnings)
	}
	if _, ok := codes["udp_l7_tcp_ports"]; ok {
		t.Errorf("a set without protocol selection is not flagged for it: %+v", report.Warnings)
	}
}

func TestTheOrderOfUDPProtocolsDoesNotChangeTheSharedSet(t *testing.T) {
	a := l7OnlySet(config.UDPL7Discord, config.UDPL7STUN)
	b := l7OnlySet(config.UDPL7STUN, config.UDPL7Discord)
	envA, _, err := Build(&a, BuildOptions{B4Version: "1.88.0"})
	if err != nil {
		t.Fatal(err)
	}
	envB, _, err := Build(&b, BuildOptions{B4Version: "1.88.0"})
	if err != nil {
		t.Fatal(err)
	}
	if envA.Fingerprint != envB.Fingerprint {
		t.Errorf("the same protocols in another order are the same strategy: %s vs %s", envA.Fingerprint, envB.Fingerprint)
	}
	want := []string{config.UDPL7Discord, config.UDPL7STUN}
	if got := UDPL7(envB.Set); !reflect.DeepEqual(got, want) {
		t.Errorf("the shared set must carry the protocols in one order, got %v", got)
	}
	if !reflect.DeepEqual(b.UDP.FilterL7, []string{config.UDPL7STUN, config.UDPL7Discord}) {
		t.Errorf("building an envelope must not reorder the local set: %v", b.UDP.FilterL7)
	}

	imp, err := Open(envB, OpenOptions{B4Version: "1.88.0"})
	if err != nil {
		t.Fatal(err)
	}
	if imp.Fingerprint != envA.Fingerprint || !reflect.DeepEqual(imp.Set.UDP.FilterL7, want) {
		t.Errorf("the imported set must keep the one order and fingerprint: %s %v", imp.Fingerprint, imp.Set.UDP.FilterL7)
	}
	live, err := LiveFingerprint(&b, nil)
	if err != nil {
		t.Fatal(err)
	}
	if live != imp.Fingerprint {
		t.Errorf("a local set that lists the protocols in another order is unchanged: %s vs %s", live, imp.Fingerprint)
	}
}
