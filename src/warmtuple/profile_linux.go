package warmtuple

import (
	"net/netip"
	"strconv"

	"github.com/daniellavrushin/b4/config"
)

func QueueConfig(target netip.AddrPort, queue uint16) config.Config {
	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads, cfg.Queue.IsDiscovery = int(queue), 1, true
	cfg.System.DNS.TCPDisabled = true
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "warm-tuple-poc", "Warm tuple PoC", true
	set.Targets.DomainOnly = false
	set.Targets.IpsToMatch = []string{target.Addr().String()}
	set.TCP.DPortFilter = strconv.Itoa(int(target.Port()))
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1, Cutoff: "client_hello"}
	set.TCP.RSTProtection.Enabled = false
	set.Faking.SNI = false
	set.Fragmentation.Strategy = "combo"
	set.Fragmentation.MiddleSNI = true
	set.Fragmentation.SeqOverlapLength = 1
	set.Fragmentation.SeqOverlapBytes = []byte{0}
	set.Fragmentation.SeqOverlapPattern = []string{"0x00"}
	set.Fragmentation.Combo.FirstByteSplit = true
	set.Fragmentation.Combo.ExtensionSplit = false
	set.Fragmentation.Combo.ShuffleMode = "reverse"
	set.Fragmentation.Combo.FirstDelayMs = 1
	set.Fragmentation.Combo.JitterMaxUs = 1
	set.Fragmentation.Combo.DecoyEnabled = false
	set.Fragmentation.Combo.FakePerSegment = false
	cfg.Sets = []*config.SetConfig{&set}
	return cfg
}
