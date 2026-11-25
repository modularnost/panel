package web

import (
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// spaHandler serves the built frontend: the file when there is one,
// index.html for every other path, so a client-side route survives a reload.
func spaHandler(fsys fs.FS) http.HandlerFunc {
	index, indexErr := fs.ReadFile(fsys, "index.html")
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'self'; object-src 'none'")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if st, err := fs.Stat(fsys, name); err == nil && !st.IsDir() {
			if strings.HasPrefix(name, "assets/") {
				// File names carry a content hash.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				ext := path.Ext(name)
				if ext == ".js" || ext == ".css" {
					w.Header().Add("Vary", "Accept-Encoding")
					if r.Header.Get("Range") == "" && acceptsGzip(r.Header.Values("Accept-Encoding")) {
						if gz, err := fs.Stat(fsys, name+".gz"); err == nil && !gz.IsDir() {
							w.Header().Set("Content-Type", mime.TypeByExtension(ext))
							w.Header().Set("Content-Encoding", "gzip")
							name += ".gz"
						}
					}
				}
			}
			http.ServeFileFS(w, r, fsys, name)
			return
		}
		if indexErr != nil {
			http.Error(w, "the web UI is not built into this binary", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	}
}

func acceptsGzip(values []string) bool {
	var wildcard bool
	for _, header := range values {
		for entry := range strings.SplitSeq(header, ",") {
			coding, parameters, _ := strings.Cut(entry, ";")
			coding = strings.ToLower(strings.TrimSpace(coding))
			if coding != "gzip" && coding != "*" {
				continue
			}
			q := 1.0
			for param := range strings.SplitSeq(parameters, ";") {
				if value, ok := strings.CutPrefix(strings.TrimSpace(param), "q="); ok {
					q, _ = strconv.ParseFloat(value, 64)
				}
			}
			allowed := q > 0 && q <= 1
			if coding == "gzip" {
				return allowed
			}
			wildcard = allowed
		}
	}
	return wildcard
}
