package config

import "testing"

func TestStripCLIOverrides_LogDir(t *testing.T) {
	saved := persistedOverrides
	t.Cleanup(func() { persistedOverrides = saved })

	cases := []struct {
		name    string
		logDir  string // value passed to --log-dir
		applied string // Directory after ApplyCLIOverrides
	}{
		{"empty disables", "", ""},
		{"custom path", "/custom/logs", "/custom/logs"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := &Config{}
			snap.System.Logging.Directory = "/var/log/b4" // value from config file

			persistedOverrides.snapshot = snap
			persistedOverrides.fields = map[string]bool{"log-dir": true}
			persistedOverrides.overrides = CLIOverrides{LogDir: tc.logDir}

			cur := &Config{}
			cur.System.Logging.Directory = tc.applied

			got := stripCLIOverrides(cur)
			if got.System.Logging.Directory != "/var/log/b4" {
				t.Errorf("override not stripped: want %q, got %q",
					"/var/log/b4", got.System.Logging.Directory)
			}
		})
	}
}
