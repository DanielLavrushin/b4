package uicheck

import (
	"strings"
	"testing"
)

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
