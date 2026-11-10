package nfq

import (
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"golang.org/x/net/dns/dnsmessage"
)

const pinAliasDeviceMAC = "aa:bb:cc:dd:ee:01"

func clearDNSPinNotes(t *testing.T) {
	t.Helper()
	reset := func() {
		dnsPinNotesMu.Lock()
		clear(dnsPinNotes)
		dnsPinNotesSwept = time.Time{}
		dnsPinNotesMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func pinAliasDeviceWorker(t *testing.T, sets ...*config.SetConfig) *Worker {
	t.Helper()
	clearDNSPinNotes(t)
	cfg := config.NewConfig()
	cfg.Sets = sets
	w := passiveDNSWorker(t, &cfg)
	w.ipToMac.Store(map[string]string{rigDevice: pinAliasDeviceMAC})
	return w
}

func theDeviceAsksMX(t *testing.T, w *Worker) {
	t.Helper()
	query := lanQuery(rigDevice, rigOutside)
	query.srcMac = pinAliasDeviceMAC
	q := newFakeModQueue()
	w.processDnsPacket(&verdictCtx{id: 1, q: q}, query, 40000, 53, dns.BuildQuery(pinAliasName, 0x5151, uint16(dnsmessage.TypeMX)))
	passedUnchanged(t, q.now(t), "the device's MX query goes on to its resolver")
}

func TestAReplyToADeviceThePinningSetExcludesKeepsItsAnswer(t *testing.T) {
	set := pinAliasSet("instagram", true)
	set.Targets.SourceDevices = []string{pinAliasDeviceMAC}
	set.Targets.SourceDevicesExclude = true
	w := pinAliasDeviceWorker(t, set)
	theDeviceAsksMX(t, w)
	q := newFakeModQueue()

	handleReply(t, w, &verdictCtx{id: 2, q: q}, masqueradedReply(pinAliasMXReply(t, 0x5151)))

	passedUnchanged(t, q.now(t), "the set excludes this device, so its pins must not rewrite the device's answer")
}

func TestAReplyToADeviceWithItsOwnSetIsNotRewrittenByAnotherSetsPins(t *testing.T) {
	general := pinAliasSet("general", true)
	own := pinAliasSet("own", false)
	own.Targets.SourceDevices = []string{pinAliasDeviceMAC}
	w := pinAliasDeviceWorker(t, own, general)
	theDeviceAsksMX(t, w)
	q := newFakeModQueue()

	handleReply(t, w, &verdictCtx{id: 2, q: q}, masqueradedReply(pinAliasMXReply(t, 0x5151)))

	passedUnchanged(t, q.now(t), "the device's own set pins nothing, so another set's pins must not rewrite its answer")
}

func TestAReplyToADeviceOfASetLimitedToDevicesLosesTheAlias(t *testing.T) {
	set := pinAliasSet("instagram", true)
	set.Targets.SourceDevices = []string{pinAliasDeviceMAC}
	w := pinAliasDeviceWorker(t, set)
	theDeviceAsksMX(t, w)
	q := newFakeModQueue()
	raw := masqueradedReply(pinAliasMXReply(t, 0x5151))

	handleReply(t, w, &verdictCtx{id: 2, q: q}, raw)

	v := q.now(t)
	if v.packet == nil {
		t.Fatal("the masqueraded reply to a device of a set limited to devices passed unchanged, alias included")
	}
	emptyPinAnswer(t, keepsItsPacket(t, v, raw), "the masqueraded reply to a device of a set limited to devices")
}

func TestAnAttributedReplyKeepsTheSetItsQueryUsedDespiteAnEscalation(t *testing.T) {
	primary := pinAliasSet("primary", true)
	primary.Escalate.To = "backup"
	backup := pinAliasSet("backup", false)
	backup.Targets.SourceDevices = []string{pinAliasDeviceMAC}
	backup.Targets.SourceDevicesExclude = true
	w := pinAliasDeviceWorker(t, primary, backup)
	if !w.destState.SetEscalation(pinAliasName, backup.Id, escalateReasonDNS, time.Minute) {
		t.Fatal("the escalation was not recorded")
	}
	theDeviceAsksMX(t, w)
	q := newFakeModQueue()
	raw := masqueradedReply(pinAliasMXReply(t, 0x5151))

	handleReply(t, w, &verdictCtx{id: 2, q: q}, raw)

	v := q.now(t)
	if v.packet == nil {
		t.Fatal("the reply passed with its alias: the backup set excludes this device, so its query stayed on the pinning primary set, and the reply must follow the query")
	}
	emptyPinAnswer(t, keepsItsPacket(t, v, raw), "the reply to a device the backup set excludes")
}

func TestAnUnattributedReplyIsNotRewrittenForAnUnknownDeviceOfASetWithDevices(t *testing.T) {
	set := pinAliasSet("instagram", true)
	set.Targets.SourceDevices = []string{pinAliasDeviceMAC}
	set.Targets.SourceDevicesExclude = true
	w := pinAliasDeviceWorker(t, set)
	q := newFakeModQueue()

	handleReply(t, w, &verdictCtx{id: 2, q: q}, masqueradedReply(pinAliasMXReply(t, 0x5151)))

	passedUnchanged(t, q.now(t), "without its query b4 cannot tell which device a masqueraded reply goes to, so a set that lists devices leaves it alone")
}

func TestPinNotesStopGrowingWhenFull(t *testing.T) {
	clearDNSPinNotes(t)
	for i := range dnsPinNoteMax + 10 {
		noteDNSPinSet(dnsPinNoteKey{ver: IPv4, port: uint16(i), txid: uint16(i >> 16), qtype: 15, name: pinAliasName}, "instagram")
	}
	dnsPinNotesMu.Lock()
	n := len(dnsPinNotes)
	dnsPinNotesMu.Unlock()
	if n != dnsPinNoteMax {
		t.Fatalf("%d notes, want the cap of %d", n, dnsPinNoteMax)
	}
	if _, ok := takeDNSPinSet(dnsPinNoteKey{ver: IPv4, port: 3, qtype: 15, name: pinAliasName}); !ok {
		t.Fatal("a note taken before the cap was lost")
	}
	if _, ok := takeDNSPinSet(dnsPinNoteKey{ver: IPv4, port: 3, qtype: 15, name: pinAliasName}); ok {
		t.Fatal("a note was taken twice")
	}
}
