package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestRawFeedNamesThatCanBeCutShowInFullInATooltip(t *testing.T) {
	table := readUI(t, "components/connections/Table.tsx")
	title := regexp.MustCompile(`(?s)^<Tooltip\s+title=\{(.*?)\}\s+placement=`)
	for _, c := range []struct{ element, name string }{
		{"label={log.ipSet || log.hostSet}", "log.ipSet || log.hostSet"},
		{"label={log.deviceName}", "log.deviceName"},
		{"label={asnInfo.name}", "asnInfo.name"},
		{"<Typography noWrap>{log.domain}", "log.domain"},
	} {
		at := strings.Index(table, c.element)
		if at < 0 {
			t.Errorf("the Raw feed table has no %s", c.element)
			continue
		}
		open := strings.LastIndex(table[:at], "<Tooltip")
		if open < 0 || strings.LastIndex(table[:at], "</Tooltip>") > open {
			t.Errorf("%s in the Raw feed is not inside a tooltip, so a name cut by its column cannot be read", c.element)
			continue
		}
		m := title.FindStringSubmatch(table[open:at])
		if m == nil || !strings.Contains(m[1], c.name) {
			t.Errorf("the tooltip around %s in the Raw feed does not show %s", c.element, c.name)
		}
	}
}

func TestRawFeedWaitingMessageSitsOutsideTheWideTable(t *testing.T) {
	table := readUI(t, "components/connections/Table.tsx")
	msg := strings.Index(table, `"connections.table.waitingForPackets"`)
	end := strings.LastIndex(table, "</Table>")
	if msg < 0 || end < 0 {
		t.Fatal("the Raw feed table has no waiting message or no closing </Table>")
	}
	if msg < end {
		t.Error("the Raw feed's waiting message is inside the table, which is wider than a phone or an iPad, so the message is centred off screen")
	}
}
