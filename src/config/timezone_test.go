package config

import (
	"testing"
	"time"
)

func TestParseFixedOffset(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantNil    bool
		wantOffset int // seconds east of UTC
	}{
		{"bare UTC", "UTC", false, 0},
		{"UTC+1", "UTC+1", false, 3600},
		{"UTC-5", "UTC-5", false, -5 * 3600},
		{"UTC+5:30", "UTC+5:30", false, 5*3600 + 30*60},
		{"UTC-9:30", "UTC-9:30", false, -(9*3600 + 30*60)},
		{"UTC+0", "UTC+0", false, 0},
		{"UTC+14", "UTC+14", false, 14 * 3600},
		{"POSIX EST-5", "EST-5", false, 5 * 3600},
		{"POSIX CST+6", "CST+6", false, -6 * 3600},
		{"POSIX MSK-3:00", "MSK-3:00", false, 3*3600 + 0*60},
		{"POSIX IST-5:30", "IST-5:30", false, 5*3600 + 30*60},
		{"POSIX no offset", "EST", true, 0},
		{"POSIX single char", "X-5", true, 0},
		{"invalid: UTC+15", "UTC+15", true, 0},
		{"invalid: UTC+1:99", "UTC+1:99", true, 0},
		{"invalid: UTCx", "UTCx", true, 0},
		{"invalid: UTC+abc", "UTC+abc", true, 0},
		{"invalid: empty", "", true, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loc := parseFixedOffset(tt.input)
			if tt.wantNil {
				if loc != nil {
					t.Errorf("parseFixedOffset(%q) = %v, want nil", tt.input, loc)
				}
				return
			}
			if loc == nil {
				t.Fatalf("parseFixedOffset(%q) = nil, want offset %d", tt.input, tt.wantOffset)
			}
			// Verify offset by checking a fixed time
			ref := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
			inLoc := ref.In(loc)
			_, gotOffset := inLoc.Zone()
			if gotOffset != tt.wantOffset {
				t.Errorf("parseFixedOffset(%q) offset = %d, want %d", tt.input, gotOffset, tt.wantOffset)
			}
		})
	}
}

func TestApplyTimezone(t *testing.T) {
	origLocal := time.Local
	defer func() { time.Local = origLocal }()

	t.Run("IANA timezone", func(t *testing.T) {
		ApplyTimezone("Europe/Oslo")
		if time.Local.String() != "Europe/Oslo" {
			t.Errorf("time.Local = %q, want Europe/Oslo", time.Local.String())
		}
	})

	t.Run("fixed offset", func(t *testing.T) {
		ApplyTimezone("UTC+3")
		ref := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		_, offset := ref.In(time.Local).Zone()
		if offset != 3*3600 {
			t.Errorf("offset = %d, want %d", offset, 3*3600)
		}
	})

	t.Run("empty restores the zone b4 started with", func(t *testing.T) {
		t.Setenv("TZ", "")
		ApplyTimezone("UTC+5")
		ApplyTimezone("")
		if time.Local != systemLocation {
			t.Errorf("time.Local = %q after a switch back to auto, want the system zone %q", time.Local.String(), systemLocation.String())
		}
	})

	t.Run("empty applies TZ again when it is set", func(t *testing.T) {
		t.Setenv("TZ", "MSK-3")
		ApplyTimezone("UTC+5")
		ApplyTimezone("")
		ref := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, offset := ref.In(time.Local).Zone(); offset != 3*3600 {
			t.Errorf("offset = %d after a switch back to auto with TZ=MSK-3, want %d", offset, 3*3600)
		}
	})

	t.Run("invalid falls back to UTC", func(t *testing.T) {
		ApplyTimezone("Invalid/Garbage")
		ref := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
		_, offset := ref.In(time.Local).Zone()
		if offset != 0 {
			t.Errorf("offset = %d, want 0 (UTC fallback)", offset)
		}
	})
}
