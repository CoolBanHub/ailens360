package app

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

var defaultUIDirs = []string{
	"/app/ui",
	"frontend/dist",
}

func resolveUIDir(explicit string) string {
	candidates := defaultUIDirs
	if explicit != "" {
		candidates = append([]string{explicit}, candidates...)
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
				return dir
			}
		}
	}
	return ""
}

func newSPAHandler(dir string) http.Handler {
	root := os.DirFS(dir)
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		if isReservedRoute(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		cleanPath := path.Clean("/" + r.URL.Path)
		name := strings.TrimPrefix(cleanPath, "/")
		if name == "." || name == "" {
			name = "index.html"
		}

		if canServeFile(root, name) {
			serveFSPath(root, files, w, r, name)
			return
		}
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}

		serveFSPath(root, files, w, r, "index.html")
	})
}

func isReservedRoute(p string) bool {
	return p == "/healthz" ||
		p == "/version" ||
		p == "/api" ||
		strings.HasPrefix(p, "/api/")
}

func canServeFile(root fs.FS, name string) bool {
	st, err := fs.Stat(root, name)
	return err == nil && !st.IsDir()
}

func serveFSPath(root fs.FS, files http.Handler, w http.ResponseWriter, r *http.Request, name string) {
	if name == "index.html" {
		f, err := root.Open(name)
		if err == nil {
			defer f.Close()
			if seeker, ok := f.(interface {
				io.Seeker
				Stat() (fs.FileInfo, error)
				Read([]byte) (int, error)
			}); ok {
				if st, err := seeker.Stat(); err == nil {
					http.ServeContent(w, r, name, fileModTime(st), seeker)
					return
				}
			}
		}
	}

	req := r.Clone(r.Context())
	req.URL.Path = "/" + name
	files.ServeHTTP(w, req)
}

func fileModTime(info fs.FileInfo) time.Time {
	if info == nil {
		return time.Time{}
	}
	return info.ModTime()
}
