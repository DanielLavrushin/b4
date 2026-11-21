package uicheck

import (
	"regexp"
	"strconv"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestQueueAdvancedDefaultsMatchTheConfig(t *testing.T) {
	src := readUI(t, "components/settings/Queue.tsx")
	block := regexp.MustCompile(`(?s)const QUEUE_DEFAULTS = \{(.*?)\};`).FindStringSubmatch(src)
	if block == nil {
		t.Fatal("Queue.tsx has no QUEUE_DEFAULTS, so the Advanced section cannot tell a changed value")
	}
	got := map[string]int{}
	for _, m := range regexp.MustCompile(`(\w+):\s*(\d+)`).FindAllStringSubmatch(block[1], -1) {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatal(err)
		}
		got[m[1]] = n
	}
	queue := config.DefaultConfig.Queue
	want := map[string]int{
		"start_num":            queue.StartNum,
		"mark":                 int(queue.Mark),
		"threads":              queue.Threads,
		"tcp_conn_bytes_limit": queue.TCPConnBytesLimit,
		"udp_conn_bytes_limit": queue.UDPConnBytesLimit,
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("QUEUE_DEFAULTS.%s is %d in Queue.tsx, the config default is %d", key, got[key], value)
		}
	}
	if len(got) != len(want) {
		t.Errorf("QUEUE_DEFAULTS lists %d values, the test knows %d", len(got), len(want))
	}
}
