package tun

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

var stateFileCandidates = []string{"/var/run/b4-tun.state", "/run/b4-tun.state"}

type persistedState struct {
	Capture       string `json:"capture"`
	SavedDefault  string `json:"saved_default"`
	OutIface      string `json:"out_iface"`
	SavedRPFilter string `json:"saved_rp_filter"`
	TunName       string `json:"tun_name"`
	RouteTable    int    `json:"route_table"`
	Mark          uint   `json:"mark"`
	SrcIP         string `json:"src_ip"`
	SkipTables    bool   `json:"skip_tables"`
}

func writeStateFile(st persistedState) {
	data, err := json.Marshal(st)
	if err != nil {
		log.Tracef("TUN: could not marshal teardown state: %v", err)
		return
	}
	for _, path := range stateFileCandidates {
		if err := os.WriteFile(path, append(data, '\n'), 0600); err == nil {
			return
		}
	}
	log.Tracef("TUN: could not persist teardown state to any of %v", stateFileCandidates)
}

func readStateFile() (persistedState, string, bool) {
	for _, path := range stateFileCandidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var st persistedState
		if err := json.Unmarshal(data, &st); err != nil {
			log.Warnf("TUN: ignoring corrupt teardown state %s: %v", path, err)
			os.Remove(path)
			continue
		}
		return st, path, true
	}
	return persistedState{}, "", false
}

func removeStateFile() {
	for _, path := range stateFileCandidates {
		os.Remove(path)
	}
}

func (r *routeManager) saveState() {
	writeStateFile(persistedState{
		Capture:       r.resolvedCapture,
		SavedDefault:  r.savedDefault,
		OutIface:      r.outIface,
		SavedRPFilter: r.savedRPFilter,
		TunName:       r.tunName,
		RouteTable:    r.routeTable,
		Mark:          r.mark,
		SrcIP:         r.srcIP,
		SkipTables:    r.skipTables,
	})
}

func RestoreFromState() bool {
	st, path, ok := readStateFile()
	if !ok {
		return false
	}

	restored := false
	if st.Capture == "default" && st.SavedDefault != "" {
		cur, _ := run("ip", "-4", "route", "show", "default")
		hijacked := st.TunName != "" && strings.Contains(cur, "dev "+st.TunName)
		if strings.TrimSpace(cur) == "" || hijacked {
			args := append([]string{"ip", "route", "replace"}, strings.Fields(st.SavedDefault)...)
			if _, err := run(args...); err != nil {
				log.Errorf("TUN: failed to restore default route from stale state: %v", err)
			} else {
				log.Infof("TUN: restored default route left by a previous run: %s", st.SavedDefault)
				restored = true
			}
		}
	}

	if st.SavedRPFilter != "" && st.OutIface != "" {
		if err := os.WriteFile(rpFilterPath(st.OutIface), []byte(st.SavedRPFilter+"\n"), 0644); err != nil {
			log.Tracef("TUN: could not restore rp_filter for %s from stale state: %v", st.OutIface, err)
		}
	}

	os.Remove(path)
	return restored
}
