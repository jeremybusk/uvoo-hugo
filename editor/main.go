package main

import (
	"bytes"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

//go:embed web/dist
var embeddedWeb embed.FS

type server struct {
	siteDir    string
	contentDir string
	webDir     string
	editorAddr string
	previewURL string
	publicURL  string
	hugoAddr   string
	logPath    string
	authUser   string
	authPass   string

	mu      sync.Mutex
	hugoCmd *exec.Cmd
}

type pageSummary struct {
	Path  string `json:"path"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type page struct {
	Path        string `json:"path"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Delimiter   string `json:"delimiter"`
	FrontMatter string `json:"frontMatter"`
	Body        string `json:"body"`
}

type saveRequest struct {
	Path        string `json:"path"`
	Delimiter   string `json:"delimiter"`
	FrontMatter string `json:"frontMatter"`
	Body        string `json:"body"`
}

type createRequest struct {
	Path  string `json:"path"`
	Title string `json:"title"`
}

type siteConfig struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

type saveSiteConfigRequest struct {
	Body string `json:"body"`
}

type mediaItem struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
	Modified  string `json:"modified"`
	PublicURL string `json:"publicURL"`
	Download  string `json:"download"`
	Snippet   string `json:"snippet"`
}

func main() {
	addr := flag.String("addr", envDefault("UVOO_HUGO_EDITOR_ADDR", "127.0.0.1:1314"), "editor server address")
	site := flag.String("site", envDefault("UVOO_HUGO_EDITOR_SITE", "hugo_website_demo"), "Hugo site directory")
	web := flag.String("web", envDefault("UVOO_HUGO_EDITOR_WEB_DIR", "editor/web/dist"), "built React app directory")
	hugoAddr := flag.String("hugo-addr", envDefault("UVOO_HUGO_EDITOR_HUGO_ADDR", "127.0.0.1:1313"), "local Hugo preview server address")
	publicURL := flag.String("public-url", os.Getenv("UVOO_HUGO_EDITOR_PUBLIC_URL"), "public editor base URL used for Hugo preview links")
	authUser := flag.String("auth-user", os.Getenv("UVOO_HUGO_EDITOR_AUTH_USER"), "HTTP Basic Auth username")
	authPassword := flag.String("auth-password", os.Getenv("UVOO_HUGO_EDITOR_AUTH_PASSWORD"), "HTTP Basic Auth password")
	authPasswordFile := flag.String("auth-password-file", os.Getenv("UVOO_HUGO_EDITOR_AUTH_PASSWORD_FILE"), "file containing HTTP Basic Auth password")
	startHugo := flag.Bool("start-hugo", envBool("UVOO_HUGO_EDITOR_START_HUGO", true), "start Hugo preview server on launch")
	flag.Parse()

	if *authPassword == "" && *authPasswordFile != "" {
		password, err := os.ReadFile(*authPasswordFile)
		if err != nil {
			log.Fatalf("read auth password file: %v", err)
		}
		*authPassword = strings.TrimSpace(string(password))
	}
	if *authUser == "" || *authPassword == "" {
		log.Fatal("basic auth is required: set UVOO_HUGO_EDITOR_AUTH_USER and UVOO_HUGO_EDITOR_AUTH_PASSWORD, or pass -auth-user and -auth-password")
	}

	siteDir, err := filepath.Abs(*site)
	if err != nil {
		log.Fatal(err)
	}
	contentDir := filepath.Join(siteDir, "content")
	if info, err := os.Stat(contentDir); err != nil || !info.IsDir() {
		log.Fatalf("content directory not found: %s", contentDir)
	}
	webDir, _ := filepath.Abs(*web)

	s := &server{
		siteDir:    siteDir,
		contentDir: contentDir,
		webDir:     webDir,
		editorAddr: *addr,
		previewURL: "/preview/",
		publicURL:  strings.TrimRight(*publicURL, "/"),
		hugoAddr:   *hugoAddr,
		logPath:    filepath.Join(filepath.Dir(siteDir), "hugo-server.log"),
		authUser:   *authUser,
		authPass:   *authPassword,
	}

	if *startHugo {
		if err := s.startHugo(); err != nil {
			log.Printf("hugo preview did not start: %v", err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", s.withCORS(s.handleConfig))
	mux.HandleFunc("/api/site-config", s.withCORS(s.handleSiteConfig))
	mux.HandleFunc("/api/media", s.withCORS(s.handleMedia))
	mux.HandleFunc("/api/media/download", s.withCORS(s.handleMediaDownload))
	mux.HandleFunc("/api/pages", s.withCORS(s.handlePages))
	mux.HandleFunc("/api/page", s.withCORS(s.handlePage))
	mux.HandleFunc("/api/preview/start", s.withCORS(s.handlePreviewStart))
	mux.HandleFunc("/api/preview/stop", s.withCORS(s.handlePreviewStop))
	mux.HandleFunc("/preview/", s.handlePreviewProxy)
	mux.HandleFunc("/", s.handleStatic)

	log.Printf("editor listening on http://%s", *addr)
	log.Printf("hugo site: %s", siteDir)
	log.Printf("hugo preview: http://%s/ proxied at /preview/", s.hugoAddr)
	if err := http.ListenAndServe(*addr, s.withAuth(mux)); err != nil {
		log.Fatal(err)
	}
}

func envDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envBool(name string, fallback bool) bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

func (s *server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		userOK := subtle.ConstantTimeCompare([]byte(user), []byte(s.authUser)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(pass), []byte(s.authPass)) == 1
		if !ok || !userOK || !passOK {
			w.Header().Set("WWW-Authenticate", `Basic realm="Hugo Editor", charset="UTF-8"`)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *server) handleConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	running := s.hugoCmd != nil && s.hugoCmd.Process != nil
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"siteDir":     s.siteDir,
		"previewURL":  s.previewURL,
		"publicURL":   s.publicURL,
		"hugoURL":     "http://" + s.hugoAddr + "/",
		"hugoLog":     s.logPath,
		"hugoRunning": running,
	})
}

func (s *server) handlePages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	pages, err := s.listPages()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pages)
}

func (s *server) handlePage(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.readPage(w, r)
	case http.MethodPut:
		s.savePage(w, r)
	case http.MethodPost:
		s.createPage(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleSiteConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.readSiteConfig(w, r)
	case http.MethodPut:
		s.saveSiteConfig(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) handleMedia(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listMedia(w, r)
	case http.MethodPost:
		s.uploadMedia(w, r)
	case http.MethodDelete:
		s.deleteMedia(w, r)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *server) listMedia(w http.ResponseWriter, r *http.Request) {
	kindFilter := strings.TrimSpace(r.URL.Query().Get("kind"))
	items := []mediaItem{}
	for _, root := range mediaRoots() {
		if kindFilter != "" && kindFilter != "all" && kindFilter != root.kind {
			continue
		}
		dir := filepath.Join(s.siteDir, root.dir)
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(s.siteDir, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			kind, ok := mediaKindForPath(rel)
			if !ok || (kindFilter != "" && kindFilter != "all" && kindFilter != kind) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			items = append(items, s.mediaItem(rel, kind, info))
			return nil
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind == items[j].Kind {
			return items[i].Path < items[j].Path
		}
		return items[i].Kind < items[j].Kind
	})
	writeJSON(w, http.StatusOK, items)
}

func (s *server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	const maxUploadSize = 100 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)
	if err := r.ParseMultipartForm(maxUploadSize); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	kind := strings.TrimSpace(r.FormValue("kind"))
	root, ok := mediaRootForKind(kind)
	if !ok {
		writeError(w, http.StatusBadRequest, "media kind must be images, docs, or videos")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	defer file.Close()

	name, err := sanitizeUploadName(header.Filename)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !allowedMediaExtension(root.kind, filepath.Ext(name)) {
		writeError(w, http.StatusBadRequest, "file extension is not allowed for this media kind")
		return
	}
	dir := filepath.Join(s.siteDir, root.dir)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	target := uniqueMediaPath(dir, name)
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := io.Copy(out, file); err != nil {
		_ = out.Close()
		_ = os.Remove(target)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := out.Close(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rel, _ := filepath.Rel(s.siteDir, target)
	writeJSON(w, http.StatusCreated, s.mediaItem(filepath.ToSlash(rel), root.kind, info))
}

func (s *server) deleteMedia(w http.ResponseWriter, r *http.Request) {
	fullPath, rel, err := s.safeMediaPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := os.Remove(fullPath); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true, "path": rel})
}

func (s *server) handleMediaDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	fullPath, _, err := s.safeMediaPath(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	http.ServeFile(w, r, fullPath)
}

func (s *server) readSiteConfig(w http.ResponseWriter, r *http.Request) {
	path := s.siteConfigPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, siteConfig{
		Path: filepath.Base(path),
		Body: string(raw),
	})
}

func (s *server) saveSiteConfig(w http.ResponseWriter, r *http.Request) {
	var req saveSiteConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	path := s.siteConfigPath()
	if err := writeFileAtomic(path, []byte(req.Body)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	buildOutput, buildErr := s.validateHugo()
	resp := map[string]any{
		"path":   filepath.Base(path),
		"output": buildOutput,
		"ok":     buildErr == nil,
	}
	if buildErr != nil {
		resp["error"] = buildErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) readPage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	fullPath, cleanPath, err := s.safeContentPath(path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	raw, err := os.ReadFile(fullPath)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	delimiter, fm, body := splitFrontMatter(string(raw))
	writeJSON(w, http.StatusOK, page{
		Path:        cleanPath,
		Title:       titleFromFrontMatter(fm),
		URL:         contentURL(cleanPath),
		Delimiter:   delimiter,
		FrontMatter: fm,
		Body:        body,
	})
}

func (s *server) savePage(w http.ResponseWriter, r *http.Request) {
	var req saveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	fullPath, cleanPath, err := s.safeContentPath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Delimiter == "" {
		req.Delimiter = "---"
	}
	if req.Delimiter != "---" && req.Delimiter != "+++" {
		writeError(w, http.StatusBadRequest, "front matter delimiter must be --- or +++")
		return
	}

	raw := assemblePage(req.Delimiter, req.FrontMatter, req.Body)
	if err := writeFileAtomic(fullPath, []byte(raw)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	buildOutput, buildErr := s.validateHugo()
	resp := map[string]any{
		"path":   cleanPath,
		"url":    contentURL(cleanPath),
		"output": buildOutput,
		"ok":     buildErr == nil,
	}
	if buildErr != nil {
		resp["error"] = buildErr.Error()
		writeJSON(w, http.StatusOK, resp)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) createPage(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	req.Path = normalizeNewPagePath(req.Path)
	fullPath, cleanPath, err := s.safeContentPath(req.Path)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := os.Stat(fullPath); err == nil {
		writeError(w, http.StatusConflict, "file already exists")
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Title == "" {
		req.Title = strings.TrimSuffix(filepath.Base(filepath.Dir(cleanPath)), ".md")
		if req.Title == "." || req.Title == "" {
			req.Title = "Untitled"
		}
	}
	fm := fmt.Sprintf("title: %q\n", req.Title)
	raw := assemblePage("---", fm, "Start writing here.")
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := writeFileAtomic(fullPath, []byte(raw)); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"path": cleanPath,
		"url":  contentURL(cleanPath),
	})
}

func (s *server) handlePreviewStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.startHugo(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"previewURL": s.previewURL})
}

func (s *server) handlePreviewStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hugoCmd != nil && s.hugoCmd.Process != nil {
		_ = s.hugoCmd.Process.Kill()
		s.hugoCmd = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{"stopped": true})
}

func (s *server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if info, err := os.Stat(filepath.Join(s.webDir, "index.html")); err == nil && !info.IsDir() {
		http.FileServer(http.Dir(s.webDir)).ServeHTTP(w, r)
		return
	}

	dist, err := fs.Sub(embeddedWeb, "web/dist")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	fileServer := http.FileServer(http.FS(dist))
	if r.URL.Path == "/" {
		index, err := dist.Open("index.html")
		if err != nil {
			writeError(w, http.StatusNotFound, "React app is not built yet. Run npm install && npm run build in editor/web.")
			return
		}
		_ = index.Close()
	}
	fileServer.ServeHTTP(w, r)
}

func (s *server) handlePreviewProxy(w http.ResponseWriter, r *http.Request) {
	if err := s.startHugo(); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	target, err := url.Parse("http://" + s.hugoAddr + "/")
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
	}
	proxy.ServeHTTP(w, r)
}

func (s *server) listPages() ([]pageSummary, error) {
	var pages []pageSummary
	err := filepath.WalkDir(s.contentDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(s.contentDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, fm, _ := splitFrontMatter(string(raw))
		title := titleFromFrontMatter(fm)
		if title == "" {
			title = rel
		}
		pages = append(pages, pageSummary{
			Path:  rel,
			Title: title,
			URL:   contentURL(rel),
		})
		return nil
	})
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].Path < pages[j].Path
	})
	return pages, err
}

func (s *server) siteConfigPath() string {
	for _, name := range []string{"hugo.yaml", "hugo.yml", "hugo.toml", "hugo.json"} {
		path := filepath.Join(s.siteDir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return filepath.Join(s.siteDir, "hugo.yaml")
}

type mediaRoot struct {
	kind string
	dir  string
}

func mediaRoots() []mediaRoot {
	return []mediaRoot{
		{kind: "images", dir: "assets/images"},
		{kind: "docs", dir: "static/media/docs"},
		{kind: "videos", dir: "static/media/video"},
	}
}

func mediaRootForKind(kind string) (mediaRoot, bool) {
	for _, root := range mediaRoots() {
		if root.kind == kind {
			return root, true
		}
	}
	return mediaRoot{}, false
}

func mediaKindForPath(path string) (string, bool) {
	path = strings.TrimPrefix(filepath.ToSlash(path), "/")
	for _, root := range mediaRoots() {
		prefix := strings.TrimSuffix(root.dir, "/") + "/"
		if strings.HasPrefix(path, prefix) {
			if allowedMediaExtension(root.kind, filepath.Ext(path)) {
				return root.kind, true
			}
			return "", false
		}
	}
	return "", false
}

func allowedMediaExtension(kind, ext string) bool {
	ext = strings.ToLower(ext)
	allowed := map[string][]string{
		"images": {".jpg", ".jpeg", ".png", ".webp", ".gif"},
		"docs":   {".pdf"},
		"videos": {".mp4", ".webm"},
	}
	for _, candidate := range allowed[kind] {
		if ext == candidate {
			return true
		}
	}
	return false
}

func (s *server) safeMediaPath(path string) (string, string, error) {
	if path == "" {
		return "", "", errors.New("path is required")
	}
	path = strings.TrimPrefix(strings.ReplaceAll(path, "\\", "/"), "/")
	path = filepath.Clean(filepath.FromSlash(path))
	if path == "." || filepath.IsAbs(path) || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", "", errors.New("invalid media path")
	}
	rel := filepath.ToSlash(path)
	if _, ok := mediaKindForPath(rel); !ok {
		return "", "", errors.New("media path is not in an allowed media directory")
	}
	fullPath := filepath.Join(s.siteDir, path)
	checkedRel, err := filepath.Rel(s.siteDir, fullPath)
	if err != nil || checkedRel == ".." || strings.HasPrefix(checkedRel, ".."+string(filepath.Separator)) {
		return "", "", errors.New("media path escapes site directory")
	}
	if info, err := os.Stat(fullPath); err != nil || info.IsDir() {
		return "", "", errors.New("media file not found")
	}
	return fullPath, rel, nil
}

func sanitizeUploadName(name string) (string, error) {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", errors.New("invalid filename")
	}
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, filepath.Ext(name))
	var out strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(base) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			out.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_':
			out.WriteRune(r)
			lastDash = false
		case !lastDash:
			out.WriteRune('-')
			lastDash = true
		}
	}
	cleanBase := strings.Trim(out.String(), "-_")
	if cleanBase == "" || ext == "" {
		return "", errors.New("invalid filename")
	}
	return cleanBase + ext, nil
}

func uniqueMediaPath(dir, name string) string {
	target := filepath.Join(dir, name)
	if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
		return target
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		target = filepath.Join(dir, fmt.Sprintf("%s-%d%s", base, i, ext))
		if _, err := os.Stat(target); errors.Is(err, os.ErrNotExist) {
			return target
		}
	}
}

func (s *server) mediaItem(rel, kind string, info os.FileInfo) mediaItem {
	name := filepath.Base(rel)
	download := "/api/media/download?path=" + url.QueryEscape(rel)
	publicURL := mediaPublicURL(rel, kind)
	return mediaItem{
		Path:      rel,
		Name:      name,
		Kind:      kind,
		Size:      info.Size(),
		Modified:  info.ModTime().Format(time.RFC3339),
		PublicURL: publicURL,
		Download:  download,
		Snippet:   mediaSnippet(rel, kind, publicURL),
	}
}

func mediaPublicURL(rel, kind string) string {
	switch kind {
	case "docs":
		return "/" + strings.TrimPrefix(strings.TrimPrefix(rel, "static/"), "/")
	case "videos":
		return "/" + strings.TrimPrefix(strings.TrimPrefix(rel, "static/"), "/")
	default:
		return ""
	}
}

func mediaSnippet(rel, kind, publicURL string) string {
	name := filepath.Base(rel)
	switch kind {
	case "images":
		src := strings.TrimPrefix(rel, "assets/")
		return fmt.Sprintf(`{{< image src="%s" alt="" >}}`, src)
	case "docs":
		return fmt.Sprintf("[%s](%s)", name, publicURL)
	case "videos":
		return fmt.Sprintf(`{{< video src="%s" >}}`, publicURL)
	default:
		return rel
	}
}

func (s *server) safeContentPath(path string) (string, string, error) {
	if path == "" {
		return "", "", errors.New("path is required")
	}
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.TrimPrefix(path, "content/")
	path = filepath.Clean(filepath.FromSlash(path))
	if path == "." || strings.HasPrefix(path, ".."+string(filepath.Separator)) || filepath.IsAbs(path) {
		return "", "", errors.New("invalid content path")
	}
	if filepath.Ext(path) != ".md" {
		return "", "", errors.New("content path must end in .md")
	}
	fullPath := filepath.Join(s.contentDir, path)
	rel, err := filepath.Rel(s.contentDir, fullPath)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", "", errors.New("content path escapes content directory")
	}
	return fullPath, filepath.ToSlash(path), nil
}

func (s *server) validateHugo() (string, error) {
	cmd := exec.Command("hugo", "--source", s.siteDir, "--quiet")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func (s *server) startHugo() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hugoCmd != nil && s.hugoCmd.Process != nil {
		return nil
	}

	logFile, err := os.OpenFile(s.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	_, _ = io.WriteString(logFile, "\n--- starting hugo server "+time.Now().Format(time.RFC3339)+" ---\n")

	host, port, ok := strings.Cut(s.hugoAddr, ":")
	if !ok {
		host = "127.0.0.1"
		port = "1313"
	}
	port = firstAvailablePort(host, port, s.editorAddr)
	s.hugoAddr = host + ":" + port
	s.previewURL = "/preview/"
	cmd := exec.Command(
		"hugo", "server",
		"--source", s.siteDir,
		"--disableFastRender",
		"--disableLiveReload",
		"--bind", host,
		"--port", port,
		"--baseURL", s.publicPreviewBaseURL(),
		"--appendPort=false",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	s.hugoCmd = cmd
	go func() {
		_ = cmd.Wait()
		_ = logFile.Close()
		s.mu.Lock()
		if s.hugoCmd == cmd {
			s.hugoCmd = nil
		}
		s.mu.Unlock()
	}()
	return nil
}

func (s *server) publicPreviewBaseURL() string {
	if s.publicURL != "" {
		return strings.TrimRight(s.publicURL, "/") + "/preview/"
	}
	host, port, err := net.SplitHostPort(s.editorAddr)
	if err != nil {
		return "http://127.0.0.1:1314/preview/"
	}
	if host == "" || host == "0.0.0.0" || host == "::" || host == "[::]" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/preview/"
}

func firstAvailablePort(host, preferred, skipAddr string) string {
	start := 1313
	if _, err := fmt.Sscanf(preferred, "%d", &start); err != nil {
		start = 1313
	}
	for port := start; port < start+20; port++ {
		addr := fmt.Sprintf("%s:%d", host, port)
		if sameTCPAddr(addr, skipAddr) {
			continue
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		_ = ln.Close()
		return fmt.Sprintf("%d", port)
	}
	return preferred
}

func sameTCPAddr(a, b string) bool {
	if b == "" {
		return false
	}
	_, aPort, aErr := net.SplitHostPort(a)
	_, bPort, bErr := net.SplitHostPort(b)
	if aErr == nil && bErr == nil && aPort == bPort {
		return true
	}
	normalize := func(value string) string {
		if strings.HasPrefix(value, ":") {
			return "127.0.0.1" + value
		}
		if strings.HasPrefix(value, "localhost:") {
			return "127.0.0.1:" + strings.TrimPrefix(value, "localhost:")
		}
		return value
	}
	return normalize(a) == normalize(b)
}

func splitFrontMatter(raw string) (delimiter, frontMatter, body string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	for _, delim := range []string{"---", "+++"} {
		prefix := delim + "\n"
		if strings.HasPrefix(raw, prefix) {
			rest := strings.TrimPrefix(raw, prefix)
			end := "\n" + delim + "\n"
			if idx := strings.Index(rest, end); idx >= 0 {
				return delim, rest[:idx], rest[idx+len(end):]
			}
			if strings.HasSuffix(rest, "\n"+delim) {
				return delim, strings.TrimSuffix(rest, "\n"+delim), ""
			}
		}
	}
	return "---", "", raw
}

func assemblePage(delimiter, frontMatter, body string) string {
	frontMatter = strings.Trim(frontMatter, "\r\n")
	body = strings.TrimLeft(body, "\r\n")
	return delimiter + "\n" + frontMatter + "\n" + delimiter + "\n\n" + body
}

func titleFromFrontMatter(frontMatter string) string {
	for _, line := range strings.Split(frontMatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "title:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "title:"))
			value = strings.Trim(value, `"'`)
			return value
		}
	}
	return ""
}

func normalizeNewPagePath(path string) string {
	path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
	path = strings.TrimPrefix(path, "content/")
	if path == "" {
		return "untitled/index.md"
	}
	if strings.HasSuffix(path, "/") {
		return path + "index.md"
	}
	if filepath.Ext(path) == "" {
		return strings.TrimSuffix(path, "/") + "/index.md"
	}
	return path
}

func contentURL(path string) string {
	path = strings.TrimPrefix(filepath.ToSlash(path), "content/")
	path = strings.TrimSuffix(path, ".md")
	switch {
	case path == "_index":
		return "/"
	case strings.HasSuffix(path, "/index"):
		return "/" + strings.TrimSuffix(path, "/index") + "/"
	case strings.HasSuffix(path, "/_index"):
		return "/" + strings.TrimSuffix(path, "/_index") + "/"
	default:
		return "/" + path + "/"
	}
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".edit-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
