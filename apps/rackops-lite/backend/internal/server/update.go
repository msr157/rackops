package server

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Update checking + one-click self-update.
//
// The dashboard polls /api/version-check (server-side cached, so GitHub sees
// at most one request per 6h regardless of how many browsers are open) and
// shows an "Update available" banner. POST /api/update downloads the new
// binary over the running one and restarts the process; systemd (or the
// operator's supervisor) brings it back up on the new version.

const releaseRepo = "msr157/rackops"

// ServerVersion is stamped by main() so update checks and API responses can
// report the running build.
var ServerVersion = "dev"

type versionInfo struct {
	Current         string    `json:"current"`
	Latest          string    `json:"latest"`
	UpdateAvailable bool      `json:"updateAvailable"`
	ReleaseURL      string    `json:"releaseUrl"`
	CheckedAt       time.Time `json:"checkedAt"`
	Error           string    `json:"error,omitempty"`
}

var (
	versionMu    sync.Mutex
	versionCache versionInfo
	updating     bool
)

// LatestRelease resolves the latest GitHub release tag without the API (no
// rate-limit trouble): the /releases/latest redirect carries the tag.
func LatestRelease() (tag, url string, err error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("https://github.com/" + releaseRepo + "/releases/latest")
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	loc := resp.Header.Get("Location")
	if loc == "" || !strings.Contains(loc, "/tag/") {
		return "", "", fmt.Errorf("no release found")
	}
	tag = loc[strings.LastIndex(loc, "/tag/")+len("/tag/"):]
	return tag, loc, nil
}

// versionNewer compares dotted semver-ish tags ("v0.2.0" vs "0.10.1").
func versionNewer(latest, current string) bool {
	norm := func(v string) []int {
		v = strings.TrimPrefix(strings.TrimSpace(v), "v")
		v = strings.SplitN(v, "-", 2)[0] // strip pre-release suffix
		parts := strings.Split(v, ".")
		out := make([]int, 3)
		for i := 0; i < 3 && i < len(parts); i++ {
			n, _ := strconv.Atoi(parts[i])
			out[i] = n
		}
		return out
	}
	l, c := norm(latest), norm(current)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// handleVersionCheck reports whether a newer release exists (cached 6h).
func (a *API) handleVersionCheck(w http.ResponseWriter, r *http.Request) {
	versionMu.Lock()
	defer versionMu.Unlock()
	if time.Since(versionCache.CheckedAt) > 6*time.Hour || r.URL.Query().Get("force") == "true" {
		info := versionInfo{Current: ServerVersion, CheckedAt: time.Now()}
		tag, url, err := LatestRelease()
		if err != nil {
			info.Error = err.Error()
		} else {
			info.Latest = tag
			info.ReleaseURL = url
			info.UpdateAvailable = versionNewer(tag, ServerVersion)
		}
		versionCache = info
	}
	writeJSON(w, versionCache)
}

// handleApplyUpdate performs the in-place self-update: download the release
// binary for this platform, swap it over the current executable, restart.
// Owner/admin session required — API tokens and setup mode can't trigger it.
func (a *API) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	claims := claimsFrom(r)
	if claims == nil || (claims.Role != "owner" && claims.Role != "admin") {
		http.Error(w, "only admins can update the server", http.StatusForbidden)
		return
	}
	versionMu.Lock()
	if updating {
		versionMu.Unlock()
		http.Error(w, "an update is already in progress", http.StatusConflict)
		return
	}
	updating = true
	versionMu.Unlock()
	done := func() {
		versionMu.Lock()
		updating = false
		versionMu.Unlock()
	}

	tag, _, err := LatestRelease()
	if err != nil {
		done()
		http.Error(w, "couldn't reach GitHub releases: "+err.Error(), http.StatusBadGateway)
		return
	}
	if !versionNewer(tag, ServerVersion) {
		done()
		http.Error(w, "already up to date (v"+ServerVersion+")", http.StatusBadRequest)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		done()
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := SelfUpdate(exe, tag); err != nil {
		done()
		http.Error(w, "update failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	a.Store.AddEvent("server", "Updating RackOps v"+ServerVersion+" → "+tag+" — restarting", "", "")
	writeJSON(w, map[string]any{"ok": true, "version": tag, "restarting": true})

	// Give the response time to flush, then exec into the new binary (or exit
	// and let systemd restart us — both land on the new version).
	go func() {
		time.Sleep(500 * time.Millisecond)
		restartSelf(exe)
	}()
}

// SelfUpdate downloads the release binary for this OS/arch and atomically
// replaces the executable at path. Shared by the API and `rackops update`.
func SelfUpdate(path, tag string) error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("self-update is supported on linux and macOS only")
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/rackops-%s-%s", releaseRepo, tag, runtime.GOOS, runtime.GOARCH)
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
	}

	// Write next to the target so the final rename is atomic (same fs).
	tmp := path + ".update"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := f.ReadFrom(resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	// Sanity check: the downloaded file must be a runnable rackops binary.
	out, err := exec.Command(tmp, "version").Output()
	if err != nil || !strings.Contains(string(out), "rackops") {
		os.Remove(tmp)
		return fmt.Errorf("downloaded binary failed verification (%v)", err)
	}
	return os.Rename(tmp, path)
}

// restartSelf replaces the process with the (freshly updated) binary using
// the original arguments. If exec fails, exit(0) and rely on the supervisor.
func restartSelf(exe string) {
	env := os.Environ()
	if err := syscallExec(exe, os.Args, env); err != nil {
		os.Exit(0)
	}
}
