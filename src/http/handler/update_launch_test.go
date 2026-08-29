package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func writeScript(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(p, []byte(body), 0755); err != nil {
		t.Fatal(err)
	}
	return p
}

const oldInstaller = "#!/bin/sh\naction_update() { echo no local archive support; }\n"
const newInstaller = "#!/bin/sh\naction_update() { echo \"$B4_LOCAL_ARCHIVE\"; }\n"

func TestUsableRejectsAnInstallerThatIgnoresTheArchive(t *testing.T) {
	old := writeScript(t, oldInstaller)
	new_ := writeScript(t, newInstaller)

	upload := installerRun{localArchive: "/tmp/x.tar.gz"}
	if upload.usable(old) {
		t.Error("an installer with no B4_LOCAL_ARCHIVE support must be refused for an upload")
	}
	if !upload.usable(new_) {
		t.Error("an installer that supports B4_LOCAL_ARCHIVE must be accepted")
	}

	ordinary := installerRun{}
	if !ordinary.usable(old) {
		t.Error("an ordinary update must still accept the published installer")
	}
}

func TestObtainInstallerFallsBackToACachedCopyThatSupportsUploads(t *testing.T) {
	dead := deadServerURL(t)
	swapBases(t, dead, dead, dead)
	swapMirrors(t, nil)

	cache := writeScript(t, newInstaller)
	dest := filepath.Join(t.TempDir(), "staged.sh")

	run := installerRun{localArchive: "/tmp/x.tar.gz", cachePath: cache}
	if err := run.obtainInstaller(dest); err != nil {
		t.Fatalf("expected the cached installer to be used, got %v", err)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != newInstaller {
		t.Fatalf("staged installer = %q, want the cached copy", string(body))
	}
}

func TestObtainInstallerRefusesWhenTheServedInstallerIsTooOld(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(oldInstaller))
	}))
	defer srv.Close()
	swapBases(t, srv.URL, srv.URL, srv.URL)
	swapMirrors(t, nil)

	run := installerRun{localArchive: "/tmp/x.tar.gz", cachePath: writeScript(t, oldInstaller)}
	err := run.obtainInstaller(filepath.Join(t.TempDir(), "staged.sh"))
	if err == nil {
		t.Fatal("expected a refusal rather than a silent fall back to a normal update")
	}
	if !errors.Is(err, errInstallerNoLocalArchive) {
		t.Fatalf("error = %q, want it to wrap errInstallerNoLocalArchive", err)
	}
	if !strings.Contains(err.Error(), "not about the version you picked") {
		t.Fatalf("error = %q, want it to head off the downgrade misreading", err)
	}
}

func TestObtainInstallerReportsAFetchFailureAsItself(t *testing.T) {
	dead := deadServerURL(t)
	swapBases(t, dead, dead, dead)
	swapMirrors(t, nil)

	run := installerRun{localArchive: "/tmp/x.tar.gz"}
	err := run.obtainInstaller(filepath.Join(t.TempDir(), "staged.sh"))
	if err == nil {
		t.Fatal("expected an error")
	}
	if errors.Is(err, errInstallerNoLocalArchive) {
		t.Fatalf("a network failure must not be reported as an unsupported installer: %q", err)
	}
	if !strings.Contains(err.Error(), "could not fetch") {
		t.Fatalf("error = %q, want it to name the fetch failure", err)
	}
}

func TestObtainInstallerKeepsAGoodCacheWhenTheDownloadIsTooOld(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(oldInstaller))
	}))
	defer srv.Close()
	swapBases(t, srv.URL, srv.URL, srv.URL)
	swapMirrors(t, nil)

	cache := writeScript(t, newInstaller)
	dest := filepath.Join(t.TempDir(), "staged.sh")

	run := installerRun{localArchive: "/tmp/x.tar.gz", cachePath: cache}
	if err := run.obtainInstaller(dest); err != nil {
		t.Fatalf("the good cached installer should have been used, got %v", err)
	}

	staged, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(staged) != newInstaller {
		t.Fatalf("staged = %q, want the cached upload-capable installer", string(staged))
	}

	still, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	if string(still) != newInstaller {
		t.Fatalf("the cache was overwritten with the older download: %q", string(still))
	}
}

func TestLaunchInstallerStagesPrivatelyAndCleansUpOnFailure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	dead := deadServerURL(t)
	swapBases(t, dead, dead, dead)
	swapMirrors(t, nil)

	archive := filepath.Join(t.TempDir(), "b4.tar.gz")
	if err := os.WriteFile(archive, []byte("payload"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	api := &API{cfgPtr: testCfgPtr(cfg)}

	err := api.launchInstaller(installerRun{
		serviceManager: "systemd",
		localArchive:   archive,
	})
	if err == nil {
		t.Fatal("expected the unreachable installer to fail the launch")
	}

	entries, readErr := os.ReadDir(tmp)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "b4update-") {
			t.Fatalf("staging directory %s was left behind", e.Name())
		}
	}

	if _, statErr := os.Stat(archive); !os.IsNotExist(statErr) {
		t.Fatal("the staged upload should be removed when the launch fails")
	}
}

func TestStagedInstallerIsNotAtAPredictablePath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		dir, err := os.MkdirTemp("", "b4update-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)

		if seen[dir] {
			t.Fatalf("staging directory %s was reused, it must be unguessable", dir)
		}
		seen[dir] = true

		fi, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if perm := fi.Mode().Perm(); perm != 0700 {
			t.Fatalf("staging directory mode = %o, want 0700 so nobody can plant a symlink in it", perm)
		}
	}
}

func TestEnvWithoutUpdateKeysDropsWhatTheInstallerLeftBehind(t *testing.T) {
	// Exactly what was found in a live b4 that had been restarted by the installer.
	t.Setenv("B4_LOCAL_ARCHIVE", "/tmp/b4-upload-1336015582.tar.gz")
	t.Setenv("B4_LOCAL_ARCHIVE_OWNED", "1")
	t.Setenv("B4_EXISTING_BIN", "/opt/sbin/b4")
	t.Setenv("B4_UPDATE_LOG", "/var/log/b4/update.log")
	t.Setenv("B4_MIRRORS", "https://proxy.b4core.app")
	t.Setenv("B4_KEEP_ME", "yes")

	got := envWithoutUpdateKeys()

	for _, kv := range got {
		for _, key := range updateEnvKeys {
			if strings.HasPrefix(kv, key+"=") {
				t.Errorf("%s must not be forwarded from b4's own environment", key)
			}
		}
	}

	found := false
	for _, kv := range got {
		if kv == "B4_KEEP_ME=yes" {
			found = true
		}
	}
	if !found {
		t.Error("unrelated environment variables must still be passed through")
	}
}
