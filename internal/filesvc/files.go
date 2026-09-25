package filesvc

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const headerAuth = "Authorization"

type Entry struct {
	Name    string    `json:"name"`
	Dir     bool      `json:"dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

func SafePath(root, req string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	req = strings.ReplaceAll(req, "\\", "/")
	if strings.Contains(req, "\x00") {
		return "", errors.New("invalid path")
	}
	req = strings.TrimPrefix(req, "/")
	if req == "" || req == "." {
		return root, nil
	}
	full := filepath.Join(root, filepath.FromSlash(req))
	full, err = filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if !within(root, full) {
		return "", errors.New("path escapes the shared folder")
	}
	if resolved, err := filepath.EvalSymlinks(full); err == nil {
		resolved, err = filepath.Abs(resolved)
		if err != nil || !within(root, resolved) {
			return "", errors.New("path escapes the shared folder")
		}
		return resolved, nil
	}
	parent := filepath.Dir(full)
	if resolvedParent, err := filepath.EvalSymlinks(parent); err == nil {
		resolvedParent, _ = filepath.Abs(resolvedParent)
		candidate := filepath.Join(resolvedParent, filepath.Base(full))
		if !within(root, candidate) {
			return "", errors.New("path escapes the shared folder")
		}
	}
	return full, nil
}

func within(root, full string) bool {
	rel, err := filepath.Rel(root, full)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

func Handler(root func() string, secret string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, secret) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		dir := root()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/list":
			serveList(w, r, dir)
		case r.Method == http.MethodGet && r.URL.Path == "/download":
			serveDownload(w, r, dir)
		case r.Method == http.MethodPut && r.URL.Path == "/upload":
			serveUpload(w, r, dir)
		case r.Method == http.MethodPost && r.URL.Path == "/mkdir":
			serveMkdir(w, r, dir)
		case r.Method == http.MethodDelete && r.URL.Path == "/delete":
			serveDelete(w, r, dir)
		default:
			http.NotFound(w, r)
		}
	})
}

func authorized(r *http.Request, secret string) bool {
	got := strings.TrimPrefix(r.Header.Get(headerAuth), "Bearer ")
	if len(got) != len(secret) || secret == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
}

func serveList(w http.ResponseWriter, r *http.Request, root string) {
	full, err := SafePath(root, r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Entry{Name: e.Name(), Dir: e.IsDir(), Size: info.Size(), ModTime: info.ModTime()})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"path": displayPath(root, full), "entries": out})
}

func serveDownload(w http.ResponseWriter, r *http.Request, root string) {
	full, err := SafePath(root, r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.Error(w, "not a file", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", st.Name()))
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}

func serveUpload(w http.ResponseWriter, r *http.Request, root string) {
	full, err := SafePath(root, r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, err := os.Create(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()
	if _, err := io.Copy(f, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func serveMkdir(w http.ResponseWriter, r *http.Request, root string) {
	full, err := SafePath(root, r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func serveDelete(w http.ResponseWriter, r *http.Request, root string) {
	full, err := SafePath(root, r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rootAbs, _ := filepath.Abs(root)
	if full == rootAbs {
		http.Error(w, "cannot delete the shared folder itself", http.StatusBadRequest)
		return
	}
	st, err := os.Lstat(full)
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if st.IsDir() {
		err = os.Remove(full)
	} else {
		err = os.Remove(full)
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func displayPath(root, full string) string {
	root, _ = filepath.Abs(root)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == "." {
		return ""
	}
	return filepath.ToSlash(rel)
}
