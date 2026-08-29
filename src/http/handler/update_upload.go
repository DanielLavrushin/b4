package handler

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	gopath "path"
	"path/filepath"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const (
	maxUpdateUploadSize = 64 << 20
	elfHeaderLen        = 20
	minBinarySize       = 64 << 10
)

type elfIdentity struct {
	class   byte
	data    byte
	machine uint16
}

func (e elfIdentity) String() string {
	name := map[uint16]string{
		uint16(elf.EM_X86_64):    "x86-64",
		uint16(elf.EM_386):       "x86",
		uint16(elf.EM_AARCH64):   "aarch64",
		uint16(elf.EM_ARM):       "arm",
		uint16(elf.EM_MIPS):      "mips",
		uint16(elf.EM_PPC64):     "ppc64",
		uint16(elf.EM_RISCV):     "riscv",
		uint16(elf.EM_S390):      "s390x",
		uint16(elf.EM_LOONGARCH): "loong64",
	}[e.machine]
	if name == "" {
		name = fmt.Sprintf("machine 0x%x", e.machine)
	}
	if e.data == 2 {
		name += " big-endian"
	}
	return name
}

func parseELFIdentity(header []byte) (elfIdentity, error) {
	var id elfIdentity
	if len(header) < elfHeaderLen {
		return id, fmt.Errorf("file is too short to be a program")
	}
	if header[0] != 0x7f || header[1] != 'E' || header[2] != 'L' || header[3] != 'F' {
		return id, fmt.Errorf("the b4 entry in the archive is not a Linux executable")
	}

	id.class = header[4]
	id.data = header[5]
	if id.data == 2 {
		id.machine = uint16(header[18])<<8 | uint16(header[19])
	} else {
		id.machine = uint16(header[19])<<8 | uint16(header[18])
	}
	return id, nil
}

func runningELFIdentity() (elfIdentity, error) {
	var id elfIdentity

	exe, err := os.Executable()
	if err != nil {
		return id, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	f, err := os.Open(exe)
	if err != nil {
		return id, err
	}
	defer f.Close()

	header := make([]byte, elfHeaderLen)
	if _, err := io.ReadFull(f, header); err != nil {
		return id, err
	}
	return parseELFIdentity(header)
}

// inspectUpdateArchive reports the architecture of the b4 the installer would put in
// place. The installer extracts the archive and installs whatever ends up at the root as
// "b4", so only the root entry is examined: a nested decoy/b4 is not what gets installed,
// and a second root entry would overwrite the first during extraction, which is why more
// than one is refused rather than picked between.
func inspectUpdateArchive(archivePath string) (elfIdentity, error) {
	var id elfIdentity

	f, err := os.Open(archivePath)
	if err != nil {
		return id, err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return id, fmt.Errorf("not a gzip archive: %v", err)
	}
	defer gr.Close()

	found := false
	tr := tar.NewReader(gr)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return id, fmt.Errorf("not a readable tar archive: %v", err)
		}
		if gopath.Clean(hdr.Name) != "b4" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return id, fmt.Errorf("the b4 entry in the archive is not a regular file")
		}
		if found {
			return id, fmt.Errorf("the archive contains more than one b4 entry")
		}
		if hdr.Size < minBinarySize {
			return id, fmt.Errorf("the b4 entry in the archive is only %d bytes, too small to be the service", hdr.Size)
		}

		header := make([]byte, elfHeaderLen)
		if _, err := io.ReadFull(tr, header); err != nil {
			return id, fmt.Errorf("the b4 entry in the archive is truncated")
		}
		if id, err = parseELFIdentity(header); err != nil {
			return id, err
		}
		found = true
	}

	if !found {
		return id, fmt.Errorf("the archive does not contain a file named b4 at its root")
	}

	return id, nil
}

// webAuthConfigured mirrors the web server's own rule: authentication is only in force
// once a username and a password are both set. The middleware waves everything through
// when they are not, and the default bind address is 0.0.0.0, so a route that installs and
// runs a binary as root has to insist on credentials for itself.
func (api *API) webAuthConfigured() bool {
	ws := api.getCfg().System.WebServer
	return ws.Username != "" && ws.Password != ""
}

// @Summary Update from an uploaded release archive
// @Description Installs b4 from a b4-linux-<arch>.tar.gz uploaded by the reader, for a
// @Description network where no download source can be reached. The archive is checked for
// @Description a b4 entry built for this machine before anything is replaced.
// @Tags System
// @Accept mpfd
// @Produce json
// @Param file formData file true "b4-linux-<arch>.tar.gz release archive"
// @Param sha256 formData string false "Expected SHA256 of the archive"
// @Success 200 {object} UpdateResponse
// @Failure 400 {object} UpdateResponse
// @Security BearerAuth
// @Router /system/update/upload [post]
func (api *API) handleUpdateUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	serviceManager := api.getServiceManager()

	if !api.webAuthConfigured() {
		log.Warnf("Refused an archive upload: the web server has no username and password set")
		writeUploadRefusal(w, serviceManager,
			"Installing from a file needs a web server username and password to be set. Without them b4 accepts every request on this port, and this route replaces the binary that runs as root. Set credentials under Settings, Web Server.")
		return
	}

	if serviceManager == "docker" {
		writeUploadRefusal(w, serviceManager, "Cannot update: B4 is running inside Docker. Pull the latest image and recreate your container to update.")
		return
	}
	if serviceManager == "standalone" {
		writeUploadRefusal(w, serviceManager, "Cannot update: B4 is not running as a service. Please update manually.")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUpdateUploadSize)
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		writeUploadRefusal(w, serviceManager, "Failed to read the upload: "+err.Error())
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeUploadRefusal(w, serviceManager, "No file provided")
		return
	}
	defer file.Close()

	tmp, err := os.CreateTemp("/tmp", "b4-upload-*.tar.gz")
	if err != nil {
		writeUploadRefusal(w, serviceManager, "Cannot stage the upload: "+err.Error())
		return
	}
	archivePath := tmp.Name()

	digest := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, digest), file)
	tmp.Close()
	if err != nil {
		os.Remove(archivePath)
		writeUploadRefusal(w, serviceManager, "Failed to write the upload to disk: "+err.Error())
		return
	}

	sum := hex.EncodeToString(digest.Sum(nil))

	if want := strings.TrimSpace(strings.ToLower(r.FormValue("sha256"))); want != "" {
		if want != sum {
			os.Remove(archivePath)
			writeUploadRefusal(w, serviceManager, fmt.Sprintf("Checksum does not match: the upload is %s, expected %s", sum, want))
			return
		}
	}

	uploaded, err := inspectUpdateArchive(archivePath)
	if err != nil {
		os.Remove(archivePath)
		writeUploadRefusal(w, serviceManager, err.Error())
		return
	}

	if running, err := runningELFIdentity(); err != nil {
		log.Warnf("Cannot read the running binary to compare architectures: %v", err)
	} else if running != uploaded {
		os.Remove(archivePath)
		writeUploadRefusal(w, serviceManager, fmt.Sprintf(
			"Wrong architecture: the archive holds a %s build, this router runs %s",
			uploaded, running))
		return
	}

	log.Infof("Update requested from an uploaded archive (%s, %d bytes, sha256 %s)", header.Filename, size, sum)

	logPath := api.updateLogPath()
	if logPath != "" {
		if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
			logPath = ""
		} else if err := os.WriteFile(logPath, []byte{}, 0644); err != nil {
			logPath = ""
		}
	}
	writeUpdateLog(logPath, "=== Update session started (uploaded archive) ===")
	writeUpdateLog(logPath, "Service manager: %s | file: %q | %d bytes | sha256 %s | arch %s",
		serviceManager, header.Filename, size, sum, uploaded)

	api.backupConfig(logPath)

	if err := api.launchInstaller(installerRun{
		serviceManager: serviceManager,
		logPath:        logPath,
		mirrors:        api.updateMirrors(),
		localArchive:   archivePath,
		cachePath:      api.installerCachePath(),
	}); err != nil {
		writeUploadRefusal(w, serviceManager, "Cannot install the archive: "+err.Error())
		return
	}

	sendResponse(w, UpdateResponse{
		Success:        true,
		Message:        "Archive accepted. The service will restart automatically.",
		ServiceManager: serviceManager,
	})
}

func writeUploadRefusal(w http.ResponseWriter, serviceManager, message string) {
	setJsonHeader(w)
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(UpdateResponse{
		Success:        false,
		Message:        message,
		ServiceManager: serviceManager,
	})
}
