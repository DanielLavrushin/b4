package handler

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
