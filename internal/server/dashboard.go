package server

import (
	"errors"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"strings"
)

const (
	defaultDashboardDir = "web/dist"
	dashboardIndexFile  = "index.html"
)

func resolveDashboardFS(cfg Config) fs.FS {
	if cfg.DashboardFS != nil {
		return cfg.DashboardFS
	}
	dashboardDir := strings.TrimSpace(cfg.DashboardDir)
	if dashboardDir != "" {
		return os.DirFS(dashboardDir)
	}
	if embedded := embeddedDashboardFS(); dashboardAvailable(embedded) {
		return embedded
	}
	return os.DirFS(defaultDashboardDir)
}

func (h *handler) serveDashboard(w http.ResponseWriter, r *http.Request) bool {
	if !dashboardAvailable(h.config.DashboardFS) {
		return false
	}

	name := dashboardPath(r.URL.Path)
	if name != "" {
		data, err := fs.ReadFile(h.config.DashboardFS, name)
		switch {
		case err == nil:
			serveDashboardBytes(w, r, name, data)
			return true
		case isMissingFile(err):
			if isDashboardAssetPath(name) {
				writeError(w, http.StatusNotFound, "not_found", "dashboard asset not found")
				return true
			}
		default:
			writeError(w, http.StatusInternalServerError, "dashboard_asset_error", err.Error())
			return true
		}
	}

	index, err := fs.ReadFile(h.config.DashboardFS, dashboardIndexFile)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "dashboard_index_error", err.Error())
		return true
	}
	serveDashboardBytes(w, r, dashboardIndexFile, index)
	return true
}

func dashboardAvailable(fsys fs.FS) bool {
	if fsys == nil {
		return false
	}
	info, err := fs.Stat(fsys, dashboardIndexFile)
	return err == nil && !info.IsDir()
}

func dashboardPath(requestPath string) string {
	cleaned := path.Clean("/" + requestPath)
	cleaned = strings.TrimPrefix(cleaned, "/")
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func isDashboardAssetPath(name string) bool {
	return strings.HasPrefix(name, "assets/") || path.Ext(name) != ""
}

func isAPIV1Path(requestPath string) bool {
	return requestPath == "/api/v1" || strings.HasPrefix(requestPath, "/api/v1/")
}

func acceptsDashboardHTML(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

func isMissingFile(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, os.ErrNotExist)
}

func serveDashboardBytes(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	if name == dashboardIndexFile {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data)
	}
}
