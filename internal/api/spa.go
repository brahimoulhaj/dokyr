package api

import (
	"crypto/sha256"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"regexp"
	"strings"

	"panel/internal/brand"
)

var inlineScriptRe = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// pageCSP is the policy of the web app's page: its own files only, plus the inline
// scripts index.html itself carries (the theme applied before first paint), by hash.
// Styles may be inline: the terminal and the charts set them from code.
// Forms go to the panel itself, and to GitHub: creating a GitHub App (for a Git
// connection or for GitHub sign-in) posts its manifest there.
func pageCSP(index []byte) string {
	scripts := "script-src 'self'"
	for _, m := range inlineScriptRe.FindAllSubmatch(index, -1) {
		sum := sha256.Sum256(m[1])
		scripts += " 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	}
	return "default-src 'self'; " + scripts + "; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https:; " +
		"font-src 'self' data:; media-src 'self' data: blob:; worker-src 'self' blob:; connect-src 'self'; " +
		"object-src 'none'; base-uri 'self'; form-action 'self' https://github.com; frame-ancestors 'none'"
}

// spa serves the embedded web app. Unknown paths fall back to index.html so that
// client-side routes survive a page reload.
func (s *Server) spa() http.Handler {
	files := http.FileServerFS(s.web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name != "" && !strings.HasPrefix(path.Base(name), ".") {
			if info, err := fs.Stat(s.web, name); err == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") { // content-hashed by Vite
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if path.Ext(name) != "" { // a missing file, not a client-side route
			http.NotFound(w, r)
			return
		}
		index, err := fs.ReadFile(s.web, "index.html")
		if err != nil {
			http.Error(w, brand.Name+": the web app is not built into this binary. Run `make web` and rebuild.",
				http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", pageCSP(index))
		w.Write(index)
	})
}
