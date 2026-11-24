package nfq

import (
	"bytes"
	"io"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/log"
)

func TestARecordingDoesNotPutDNSQueriesIntoTheTrafficFeed(t *testing.T) {
	cases := []struct {
		name      string
		level     log.Level
		recording bool
		wantRow   bool
		wantLine  bool
	}{
		{"info", log.LevelInfo, false, true, false},
		{"info while recording", log.LevelInfo, true, true, true},
		{"error", log.LevelError, false, false, false},
		{"error while recording", log.LevelError, true, false, true},
		{"silent while recording", -1, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			previous := log.ConfiguredLevel()
			log.Init(io.Discard, tc.level, true)
			recorded := &bytes.Buffer{}
			if tc.recording {
				log.StartRecording(recorded, log.LevelDebug)
			}
			t.Cleanup(func() {
				log.StopRecording(recorded)
				log.Init(os.Stderr, previous, true)
			})
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			logDNSEvent("UDP", nil, "feed.example", net.IPv4(192, 0, 2, 1), net.IPv4(198, 51, 100, 1), 40000, "", "block")

			row := ""
			select {
			case row = <-ch:
			default:
			}
			if got := strings.Contains(row, "feed.example"); got != tc.wantRow {
				t.Errorf("row in the feed = %v, want %v: %q", got, tc.wantRow, row)
			}
			if got := strings.Contains(recorded.String(), "feed.example"); got != tc.wantLine {
				t.Errorf("line in the recording = %v, want %v: %q", got, tc.wantLine, recorded.String())
			}
		})
	}
}
