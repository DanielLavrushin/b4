package tables

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

const (
	rpFilterLoose  = "2"
	rpFilterStrict = 1
)

var (
	rpFilterMu     sync.Mutex
	rpFilterSaved  = make(map[string]string)
	rpFilterUsedBy = make(map[string]map[string]bool)
)

func rpFilterPath(iface string) string {
	return "/proc/sys/net/ipv4/conf/" + iface + "/rp_filter"
}

var routeReadRPFilter = routeReadRPFilterExec
var routeWriteRPFilter = routeWriteRPFilterExec

func routeReadRPFilterExec(iface string) (string, bool) {
	b, err := os.ReadFile(rpFilterPath(iface))
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}

func routeWriteRPFilterExec(iface, value string) bool {
	return os.WriteFile(rpFilterPath(iface), []byte(value+"\n"), 0644) == nil
}

func rpFilterEffective(iface string) (int, bool) {
	dev, ok := routeReadRPFilter(iface)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(dev)
	if err != nil {
		return 0, false
	}
	if all, ok := routeReadRPFilter("all"); ok {
		if a, err := strconv.Atoi(all); err == nil && a > n {
			n = a
		}
	}
	return n, true
}

func routeLoosenRPFilter(iface, setID string) {
	if iface == "" || setID == "" {
		return
	}

	rpFilterMu.Lock()
	defer rpFilterMu.Unlock()

	users := rpFilterUsedBy[iface]
	if users == nil {
		users = make(map[string]bool)
		rpFilterUsedBy[iface] = users
	}
	users[setID] = true

	effective, ok := rpFilterEffective(iface)
	if !ok {
		log.Warnf("Routing: %s has no reverse-path filtering control, so b4 cannot tell whether replies for a set routed there are being dropped before any rule sees them", iface)
		return
	}
	if effective != rpFilterStrict {
		return
	}

	cur, _ := routeReadRPFilter(iface)
	if _, remembered := rpFilterSaved[iface]; !remembered {
		rpFilterSaved[iface] = cur
	}
	if !routeWriteRPFilter(iface, rpFilterLoose) {
		log.Warnf("Routing: could not relax strict reverse-path filtering on %s; a reply arriving there is dropped by the kernel before any rule sees it, so a set routed out that interface answers nothing", iface)
		return
	}
	log.Infof("Routing: reverse-path filtering on %s relaxed to loose, because a set routes traffic out it and the reply comes back the same way", iface)
}

func routeReleaseRPFilter(iface, setID string) {
	if iface == "" || setID == "" {
		return
	}

	rpFilterMu.Lock()
	defer rpFilterMu.Unlock()

	users := rpFilterUsedBy[iface]
	if users == nil {
		return
	}
	delete(users, setID)
	if len(users) > 0 {
		return
	}
	delete(rpFilterUsedBy, iface)

	prev, ok := rpFilterSaved[iface]
	if !ok {
		return
	}
	delete(rpFilterSaved, iface)
	if routeWriteRPFilter(iface, prev) {
		log.Infof("Routing: reverse-path filtering on %s restored to %s", iface, prev)
	}
}

func routeForgetRPFilterState() {
	rpFilterMu.Lock()
	rpFilterSaved = make(map[string]string)
	rpFilterUsedBy = make(map[string]map[string]bool)
	rpFilterMu.Unlock()
}
