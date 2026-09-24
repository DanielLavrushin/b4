package config

import "slices"

func (s *SetConfig) AdoptStrategy(from *SetConfig) {
	if s == nil || from == nil {
		return
	}

	tcp := from.TCP
	tcp.Win.Values = slices.Clone(from.TCP.Win.Values)
	tcp.DPortFilter = s.TCP.DPortFilter
	tcp.RSTProtection = s.TCP.RSTProtection
	if !from.TCP.IPBlockDetect.Enabled {
		tcp.IPBlockDetect = s.TCP.IPBlockDetect
	}
	s.TCP = tcp

	frag := from.Fragmentation
	frag.StrategyPool = slices.Clone(from.Fragmentation.StrategyPool)
	frag.SeqOverlapPattern = slices.Clone(from.Fragmentation.SeqOverlapPattern)
	frag.SeqOverlapBytes = slices.Clone(from.Fragmentation.SeqOverlapBytes)
	s.Fragmentation = frag

	faking := from.Faking
	faking.PayloadData = slices.Clone(from.Faking.PayloadData)
	faking.TLSMod = slices.Clone(from.Faking.TLSMod)
	faking.SNIMutation.FakeSNIs = slices.Clone(from.Faking.SNIMutation.FakeSNIs)
	s.Faking = faking

	if from.DNS.Enabled {
		pins := s.DNS.Pins
		s.DNS = from.DNS
		s.DNS.Pins = pins
	}
}
