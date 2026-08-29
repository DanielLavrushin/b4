package handler

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var errInstallerNoLocalArchive = errors.New("the installer published in the b4 repository cannot install from a file")

type installerRun struct {
	serviceManager string
	logPath        string
	version        string
	mirrors        []string
	localArchive   string
	cachePath      string
}

func (api *API) installerCachePath() string {
	cfgPath := api.getCfg().ConfigPath
	if cfgPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cfgPath), "install.sh")
}

func cacheInstaller(src, dst string) {
	if dst == "" {
		return
	}
	body, err := os.ReadFile(src)
	if err != nil {
		return
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, body, 0755); err != nil {
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
	}
}

func looksLikeShellScript(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	header := make([]byte, 4)
	n, _ := io.ReadFull(f, header)
	return n == 4 && strings.HasPrefix(string(header[:n]), "#!/")
}

func installerSupportsLocalArchive(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return bytes.Contains(body, []byte("B4_LOCAL_ARCHIVE"))
}

func (r installerRun) usable(path string) bool {
	if !looksLikeShellScript(path) {
		return false
	}
	if r.localArchive != "" && !installerSupportsLocalArchive(path) {
		return false
	}
	return true
}

func (r installerRun) obtainInstaller(path string) error {
	url := githubRawBase + "/" + repoOwner + "/" + repoName + "/main/install.sh"

	writeUpdateLog(r.logPath, "Downloading installer from %s", url)
	_, err := downloadFileMirrored(url, path, r.mirrors)
	switch {
	case err != nil:
	case !looksLikeShellScript(path):
		err = fmt.Errorf("downloaded installer is not a shell script")
	case !r.usable(path):
		err = errInstallerNoLocalArchive
	default:
		cacheInstaller(path, r.cachePath)
		return nil
	}

	if r.cachePath != "" && r.usable(r.cachePath) {
		log.Warnf("Using the cached installer at %s (%v)", r.cachePath, err)
		writeUpdateLog(r.logPath, "Falling back to the cached installer %s (%v)", r.cachePath, err)
		body, readErr := os.ReadFile(r.cachePath)
		if readErr != nil {
			return err
		}
		if writeErr := os.WriteFile(path, body, 0755); writeErr != nil {
			return err
		}
		return nil
	}

	if r.localArchive != "" {
		if errors.Is(err, errInstallerNoLocalArchive) {
			return fmt.Errorf("%w, so it would ignore the file and install a different version. Nothing was changed, and this is not about the version you picked: it needs a newer install.sh published in the repository, or a copy of one saved at %s", err, r.cachePath)
		}
		return fmt.Errorf("could not fetch an installer able to install from a file: %v", err)
	}

	return err
}

func (api *API) launchInstaller(run installerRun) error {
	if api.overrideLaunchInstaller != nil {
		api.overrideLaunchInstaller(run)
		return nil
	}

	// A private 0700 directory: b4 runs as root and execs what it writes here, and a
	// fixed name under a world-writable /tmp can be pre-created as a symlink by anyone.
	stageDir, err := os.MkdirTemp("", "b4update-")
	if err != nil {
		log.Errorf("Failed to create a staging directory: %v", err)
		writeUpdateLog(run.logPath, "ERROR: failed to create a staging directory: %v", err)
		if run.localArchive != "" {
			os.Remove(run.localArchive)
		}
		return err
	}
	installerPath := filepath.Join(stageDir, "install.sh")

	cleanup := func() {
		os.RemoveAll(stageDir)
		if run.localArchive != "" {
			os.Remove(run.localArchive)
		}
	}

	if err := run.obtainInstaller(installerPath); err != nil {
		log.Errorf("Failed to obtain installer: %v", err)
		writeUpdateLog(run.logPath, "ERROR: failed to obtain a usable installer: %v", err)
		cleanup()
		return err
	}

	if err := os.Chmod(installerPath, 0700); err != nil {
		log.Errorf("Failed to make installer executable: %v", err)
		writeUpdateLog(run.logPath, "ERROR: failed to chmod installer: %v", err)
		cleanup()
		return err
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		log.Infof("Initiating update process...")

		fullPath := config.ExtendedPATH(os.Getenv("PATH"))

		log.Infof("Installer ready, starting update process...")
		log.Infof("Service will stop now - this is expected")
		writeUpdateLog(run.logPath, "Installer ready; handing off to %s", installerPath)

		existingBin := ""
		if exe, err := os.Executable(); err == nil {
			if resolved, err := filepath.EvalSymlinks(exe); err == nil {
				exe = resolved
			}
			existingBin = exe
		}

		env := []string{
			"B4_UPDATE_LOG=" + run.logPath,
			"B4_MIRRORS=" + strings.Join(run.mirrors, " "),
		}
		if existingBin != "" {
			env = append(env, "B4_EXISTING_BIN="+existingBin)
		}
		if run.localArchive != "" {
			env = append(env, "B4_LOCAL_ARCHIVE="+run.localArchive)
		}

		var cmd *exec.Cmd
		if run.serviceManager == "systemd" {
			args := []string{"--scope", "--unit=b4-update"}
			for _, e := range env {
				args = append(args, "--setenv="+e)
			}
			args = append(args, installerPath, "--update", "--quiet")
			if run.version != "" {
				args = append(args, run.version)
			}
			cmd = exec.Command("systemd-run", args...)
		} else {
			args := []string{"--update", "--quiet"}
			if run.version != "" {
				args = append(args, run.version)
			}
			cmd = exec.Command(installerPath, args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		}

		cmd.Env = append(os.Environ(), fmt.Sprintf("PATH=%s", fullPath))
		cmd.Env = append(cmd.Env, env...)

		devNull, _ := os.Open("/dev/null")
		cmd.Stdin = devNull

		var logFile *os.File
		if run.logPath != "" {
			logFile, _ = openUpdateLog(run.logPath)
		}
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		} else {
			cmd.Stdout = devNull
			cmd.Stderr = devNull
		}

		if err := cmd.Start(); err != nil {
			log.Errorf("Update command failed to start: %v", err)
			writeUpdateLog(run.logPath, "ERROR: update command failed to start: %v", err)
			if devNull != nil {
				devNull.Close()
			}
			if logFile != nil {
				logFile.Close()
			}
			cleanup()
			return
		}

		log.Infof("Update process started (PID: %d)", cmd.Process.Pid)
		writeUpdateLog(run.logPath, "Update process started (PID: %d, service manager: %s)", cmd.Process.Pid, run.serviceManager)

		go func() {
			_ = cmd.Wait()
			if devNull != nil {
				devNull.Close()
			}
			if logFile != nil {
				logFile.Close()
			}
			cleanup()
		}()
	}()

	return nil
}
