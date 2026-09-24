package handler

import (
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

func (api *API) engineMatcher() *sni.SuffixSet {
	if globalPool != nil {
		if matcher := globalPool.GetMatcher(); matcher != nil {
			return matcher
		}
	}
	return sni.NewSuffixSet(api.getCfg().Sets)
}

func (api *API) engineSetFor(host string) *config.SetConfig {
	return setMatchedBy(api.engineMatcher(), host)
}

func setMatchedBy(matcher *sni.SuffixSet, host string) *config.SetConfig {
	if matcher == nil || host == "" {
		return nil
	}
	if matched, set := matcher.MatchSNI(host); matched && set != nil {
		return set
	}
	return nil
}
