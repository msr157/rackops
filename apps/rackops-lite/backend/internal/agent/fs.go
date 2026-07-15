package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// dirEntry is one row in a host folder listing.
type dirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size,omitempty"`
}

// listDir returns a JSON listing of a host directory for the folder browser.
// Only absolute, cleaned paths are allowed; sensitive system dirs are blocked.
func listDir(path string) (string, error) {
	if path == "" {
		path = "/"
	}
	path = filepath.Clean(path)
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	for _, blocked := range []string{"/proc", "/sys", "/dev", "/run"} {
		if path == blocked || strings.HasPrefix(path, blocked+"/") {
			return "", fmt.Errorf("browsing %s is not allowed", blocked)
		}
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}
	out := make([]dirEntry, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		de := dirEntry{Name: e.Name(), IsDir: e.IsDir()}
		if info, err := e.Info(); err == nil && !e.IsDir() {
			de.Size = info.Size()
		}
		out = append(out, de)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir
		}
		return out[i].Name < out[j].Name
	})
	data, err := json.Marshal(map[string]any{"path": path, "entries": out})
	return string(data), err
}
