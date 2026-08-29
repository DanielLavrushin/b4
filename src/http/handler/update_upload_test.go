package handler

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func fakeELF(t *testing.T, machine elf.Machine, class, data byte) []byte {
	t.Helper()

	// Sized like a real binary: inspectUpdateArchive refuses an entry too small to be
	// the service, which is what rules out a header-only decoy.
	b := make([]byte, minBinarySize+4096)
	copy(b, []byte{0x7f, 'E', 'L', 'F'})
	b[4] = class
	b[5] = data
	b[16] = 2
	if data == 2 {
		b[18] = byte(uint16(machine) >> 8)
		b[19] = byte(uint16(machine))
	} else {
		b[18] = byte(uint16(machine))
		b[19] = byte(uint16(machine) >> 8)
	}
	return b
}

type tarEntry struct {
	name string
	body []byte
}

func writeOrderedArchive(t *testing.T, entries []tarEntry) string {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: e.name, Mode: 0755, Size: int64(len(e.body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gw.Close()

	p := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(p, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeArchive(t *testing.T, entries map[string][]byte) string {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, body := range entries {
		if err := tw.WriteHeader(&tar.Header{
			Name: name, Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gw.Close()

	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectUpdateArchiveReadsTheArchitecture(t *testing.T) {
	path := writeArchive(t, map[string][]byte{
		"b4": fakeELF(t, elf.EM_AARCH64, 2, 1),
	})

	id, err := inspectUpdateArchive(path)
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if id.machine != uint16(elf.EM_AARCH64) {
		t.Fatalf("machine = 0x%x, want aarch64", id.machine)
	}
	if got := id.String(); got != "aarch64" {
		t.Fatalf("String() = %q, want aarch64", got)
	}
}

func TestInspectUpdateArchiveRejectsBadInput(t *testing.T) {
	notGzip := filepath.Join(t.TempDir(), "x.tar.gz")
	if err := os.WriteFile(notGzip, []byte("this is not a gzip file at all"), 0644); err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"no b4 entry":     writeArchive(t, map[string][]byte{"README": []byte("hello")}),
		"b4 is not ELF":   writeArchive(t, map[string][]byte{"b4": []byte("#!/bin/sh\necho pwned\n")}),
		"b4 is truncated": writeArchive(t, map[string][]byte{"b4": {0x7f, 'E', 'L', 'F'}}),
		"not a gzip":      notGzip,
	}

	for name, path := range cases {
		if _, err := inspectUpdateArchive(path); err == nil {
			t.Errorf("%s: expected a refusal, got none", name)
		}
	}
}

func TestParseELFIdentityHandlesBigEndian(t *testing.T) {
	be, err := parseELFIdentity(fakeELF(t, elf.EM_MIPS, 1, 2))
	if err != nil {
		t.Fatalf("big-endian parse: %v", err)
	}
	if be.machine != uint16(elf.EM_MIPS) {
		t.Fatalf("machine = 0x%x, want mips", be.machine)
	}
	if got := be.String(); got != "mips big-endian" {
		t.Fatalf("String() = %q, want %q", got, "mips big-endian")
	}

	le, err := parseELFIdentity(fakeELF(t, elf.EM_MIPS, 1, 1))
	if err != nil {
		t.Fatalf("little-endian parse: %v", err)
	}
	if le == be {
		t.Fatal("big-endian and little-endian mips must not compare equal")
	}
}

func updateUploadRequest(t *testing.T, archivePath, sha string) *http.Request {
	return updateUploadRequestOrdered(t, archivePath, sha, false)
}

func updateUploadRequestOrdered(t *testing.T, archivePath, sha string, shaFirst bool) *http.Request {
	t.Helper()

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)

	writeSha := func() {
		if sha == "" {
			return
		}
		if err := mw.WriteField("sha256", sha); err != nil {
			t.Fatal(err)
		}
	}

	if shaFirst {
		writeSha()
	}

	part, err := mw.CreateFormFile("file", filepath.Base(archivePath))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}

	if !shaFirst {
		writeSha()
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/system/update/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func newUploadTestAPI(t *testing.T, serviceManager string) (*API, *[]installerRun) {
	t.Helper()

	cfg := &config.Config{}
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "secret"

	launched := &[]installerRun{}

	mux := http.NewServeMux()
	api := &API{
		mux:                    mux,
		cfgPtr:                 testCfgPtr(cfg),
		overrideServiceManager: func() string { return serviceManager },
		overrideLaunchInstaller: func(run installerRun) {
			*launched = append(*launched, run)
		},
	}
	api.mux.HandleFunc("/api/system/update/upload", api.handleUpdateUpload)
	return api, launched
}

func decodeUploadResponse(t *testing.T, rec *httptest.ResponseRecorder) UpdateResponse {
	t.Helper()
	var out UpdateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestUploadRefusesWrongArchitecture(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}

	other := elf.EM_AARCH64
	if running.machine == uint16(elf.EM_AARCH64) {
		other = elf.EM_X86_64
	}
	path := writeArchive(t, map[string][]byte{"b4": fakeELF(t, other, running.class, running.data)})

	api, launched := newUploadTestAPI(t, "systemd")
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, updateUploadRequest(t, path, ""))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
	res := decodeUploadResponse(t, rec)
	if res.Success {
		t.Fatal("a wrong-architecture archive must not be accepted")
	}
	if len(*launched) != 0 {
		t.Fatal("a refused upload must not reach the installer")
	}
	if !bytes.Contains([]byte(res.Message), []byte("Wrong architecture")) {
		t.Fatalf("message = %q, want it to name the architecture mismatch", res.Message)
	}
}

func TestUploadRefusesChecksumMismatch(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}
	path := writeArchive(t, map[string][]byte{
		"b4": fakeELF(t, elf.Machine(running.machine), running.class, running.data),
	})

	api, launched := newUploadTestAPI(t, "systemd")
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, updateUploadRequest(t, path, "00000000000000000000000000000000000000000000000000000000deadbeef"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", rec.Code)
	}
	if res := decodeUploadResponse(t, rec); res.Success {
		t.Fatal("a checksum mismatch must not be accepted")
	}
	if len(*launched) != 0 {
		t.Fatal("a refused upload must not reach the installer")
	}
}

func TestUploadAcceptsMatchingChecksum(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}
	path := writeArchive(t, map[string][]byte{
		"b4": fakeELF(t, elf.Machine(running.machine), running.class, running.data),
	})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)

	api, launched := newUploadTestAPI(t, "systemd")
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, updateUploadRequest(t, path, hex.EncodeToString(sum[:])))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d body = %s, want 200", rec.Code, rec.Body.String())
	}
	if res := decodeUploadResponse(t, rec); !res.Success {
		t.Fatalf("expected acceptance, got %q", res.Message)
	}

	if len(*launched) != 1 {
		t.Fatalf("installer launches = %d, want 1", len(*launched))
	}
	run := (*launched)[0]
	if run.localArchive == "" {
		t.Fatal("the installer must be handed the uploaded archive")
	}
	if run.version != "" {
		t.Fatalf("version = %q, want empty so the installer does not resolve one over the network", run.version)
	}
	if _, err := os.Stat(run.localArchive); err != nil {
		t.Fatalf("staged archive is missing: %v", err)
	}
	os.Remove(run.localArchive)
}

func TestUploadRefusedInDockerAndStandalone(t *testing.T) {
	for _, sm := range []string{"docker", "standalone"} {
		api, _ := newUploadTestAPI(t, sm)
		rec := httptest.NewRecorder()
		api.mux.ServeHTTP(rec, updateUploadRequest(t, writeArchive(t, map[string][]byte{"b4": fakeELF(t, elf.EM_X86_64, 2, 1)}), ""))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", sm, rec.Code)
		}
		if res := decodeUploadResponse(t, rec); res.Success {
			t.Errorf("%s: must be refused", sm)
		}
	}
}

func TestUploadRejectsNonPost(t *testing.T) {
	api, _ := newUploadTestAPI(t, "systemd")
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/update/upload", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("code = %d, want 405", rec.Code)
	}
}

func TestInspectUpdateArchiveIgnoresANestedDecoy(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}

	// decoy/b4 is a valid header for this machine; the root b4, which is what the
	// installer actually puts in place, is not an executable at all.
	path := writeOrderedArchive(t, []tarEntry{
		{"decoy/b4", fakeELF(t, elf.Machine(running.machine), running.class, running.data)},
		{"b4", []byte("#!/bin/sh\necho pwned\n")},
	})

	if _, err := inspectUpdateArchive(path); err == nil {
		t.Fatal("a nested decoy must not satisfy the check for the root b4 that gets installed")
	}
}

func TestInspectUpdateArchiveRejectsDuplicateRootEntries(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}

	other := elf.EM_AARCH64
	if running.machine == uint16(elf.EM_AARCH64) {
		other = elf.EM_X86_64
	}

	// tar extraction lets the later entry win, so validating the first is meaningless.
	path := writeOrderedArchive(t, []tarEntry{
		{"b4", fakeELF(t, elf.Machine(running.machine), running.class, running.data)},
		{"b4", fakeELF(t, other, running.class, running.data)},
	})

	if _, err := inspectUpdateArchive(path); err == nil {
		t.Fatal("two b4 entries must be refused: the one validated is not the one installed")
	}
}

func TestInspectUpdateArchiveAcceptsARealReleaseLayout(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}
	body := fakeELF(t, elf.Machine(running.machine), running.class, running.data)

	for _, name := range []string{"b4", "./b4"} {
		path := writeOrderedArchive(t, []tarEntry{{name, body}})
		id, err := inspectUpdateArchive(path)
		if err != nil {
			t.Fatalf("entry %q: a real release layout must be accepted, got %v", name, err)
		}
		if id != running {
			t.Fatalf("entry %q: identity = %v, want the running one", name, id)
		}
	}
}

func TestUploadRefusedWhenTheWebServerHasNoCredentials(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}
	archive := writeOrderedArchive(t, []tarEntry{
		{"b4", fakeELF(t, elf.Machine(running.machine), running.class, running.data)},
	})

	for _, creds := range []struct{ user, pass string }{
		{"", ""},
		{"admin", ""},
		{"", "secret"},
	} {
		api, launched := newUploadTestAPI(t, "systemd")
		cfg := api.getCfg()
		cfg.System.WebServer.Username = creds.user
		cfg.System.WebServer.Password = creds.pass

		rec := httptest.NewRecorder()
		api.mux.ServeHTTP(rec, updateUploadRequest(t, archive, ""))

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("user=%q pass=%q: code = %d, want 400", creds.user, creds.pass, rec.Code)
		}
		if res := decodeUploadResponse(t, rec); res.Success {
			t.Fatalf("user=%q pass=%q: an unauthenticated instance must not install an uploaded binary", creds.user, creds.pass)
		}
		if len(*launched) != 0 {
			t.Fatalf("user=%q pass=%q: the installer must not be reached", creds.user, creds.pass)
		}
	}
}

func bigIncompressibleArchive(t *testing.T, id elfIdentity) string {
	t.Helper()

	body := make([]byte, 12<<20)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	copy(body, fakeELF(t, elf.Machine(id.machine), id.class, id.data))
	return writeOrderedArchive(t, []tarEntry{{"b4", body}})
}

func TestUploadHandlesAnArchiveLargerThanTheMemoryBudget(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}

	archive := bigIncompressibleArchive(t, running)
	raw, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 8<<20 {
		t.Fatalf("fixture is %d bytes, it must exceed the old in-memory budget to be meaningful", len(raw))
	}
	sum := sha256.Sum256(raw)

	// Either ordering must work: streaming reads parts in order, so a checksum that
	// arrives after the file has to be compared once the whole body has been read.
	for _, shaFirst := range []bool{false, true} {
		api, launched := newUploadTestAPI(t, "systemd")
		rec := httptest.NewRecorder()
		api.mux.ServeHTTP(rec, updateUploadRequestOrdered(t, archive, hex.EncodeToString(sum[:]), shaFirst))

		if rec.Code != http.StatusOK {
			t.Fatalf("shaFirst=%v: code = %d body = %s", shaFirst, rec.Code, rec.Body.String())
		}
		if len(*launched) != 1 {
			t.Fatalf("shaFirst=%v: installer launches = %d, want 1", shaFirst, len(*launched))
		}

		staged, err := os.ReadFile((*launched)[0].localArchive)
		if err != nil {
			t.Fatalf("shaFirst=%v: %v", shaFirst, err)
		}
		if !bytes.Equal(staged, raw) {
			t.Fatalf("shaFirst=%v: the staged archive does not match what was uploaded", shaFirst)
		}
		os.Remove((*launched)[0].localArchive)
	}
}

func TestUploadRefusesASecondFilePart(t *testing.T) {
	running, err := runningELFIdentity()
	if err != nil {
		t.Skipf("cannot read the test binary's ELF header: %v", err)
	}
	archive := writeOrderedArchive(t, []tarEntry{
		{"b4", fakeELF(t, elf.Machine(running.machine), running.class, running.data)},
	})
	data, err := os.ReadFile(archive)
	if err != nil {
		t.Fatal(err)
	}

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	for i := 0; i < 2; i++ {
		part, err := mw.CreateFormFile("file", "b4.tar.gz")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/api/system/update/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	api, launched := newUploadTestAPI(t, "systemd")
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 for two file parts", rec.Code)
	}
	if len(*launched) != 0 {
		t.Fatal("the installer must not be reached")
	}
}
