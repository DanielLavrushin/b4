package config

import "testing"

// stripCLIOverrides must restore the config-file value for a transient CLI
// override so it isn't persisted. The deprecated --error-file flag maps to
// Logging.Directory (its parent dir), and an empty value means "disabled" — the
// strip comparison has to match ApplyCLIOverrides exactly for both forms.
func TestStripCLIOverrides_ErrorFile(t *testing.T) {
	saved := persistedOverrides
	t.Cleanup(func() { persistedOverrides = saved })

	cases := []struct {
		name      string
		errorFile string // value passed to --error-file
		applied   string // Directory after ApplyCLIOverrides
	}{
		{"empty disables", "", ""},
		{"custom path", "/custom/logs/errors.log", "/custom/logs"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := &Config{}
			snap.System.Logging.Directory = "/var/log/b4" // value from config file

			persistedOverrides.snapshot = snap
			persistedOverrides.fields = map[string]bool{"error-file": true}
			persistedOverrides.overrides = CLIOverrides{ErrorFile: tc.errorFile}

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
