package config

const (
	maxDashboardPanels = 64
	maxDashboardIDLen  = 64
	minDashboardSpan   = 1
	maxDashboardSpan   = 12
	dashboardSpanLimit = 64
)

type UIConfig struct {
	Dashboard DashboardLayout `json:"dashboard"`
}

type DashboardLayout struct {
	Order  []string       `json:"order,omitempty"`
	Hidden []string       `json:"hidden,omitempty"`
	Spans  map[string]int `json:"spans,omitempty"`
}

func (l DashboardLayout) Sanitized() DashboardLayout {
	out := DashboardLayout{
		Order:  sanitizePanelIDs(l.Order),
		Hidden: sanitizePanelIDs(l.Hidden),
	}

	if len(l.Spans) > 0 {
		out.Spans = make(map[string]int, len(l.Spans))
		for id, span := range l.Spans {
			if len(out.Spans) >= dashboardSpanLimit {
				break
			}
			if !validPanelID(id) {
				continue
			}
			if span < minDashboardSpan {
				span = minDashboardSpan
			}
			if span > maxDashboardSpan {
				span = maxDashboardSpan
			}
			out.Spans[id] = span
		}
		if len(out.Spans) == 0 {
			out.Spans = nil
		}
	}

	return out
}

func sanitizePanelIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if len(out) >= maxDashboardPanels {
			break
		}
		if !validPanelID(id) || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func validPanelID(id string) bool {
	return id != "" && len(id) <= maxDashboardIDLen
}
