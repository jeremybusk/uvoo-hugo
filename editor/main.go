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

func main() {
	addr := flag.String("addr", envDefault("UVOOHUGO_EDITOR_ADDR", "127.0.0.1:1314"), "editor server address")
	site := flag.String("site", envDefault("UVOOHUGO_EDITOR_SITE", "hugo_website_demo"), "Hugo site directory")
	web := flag.String("web", envDefault("UVOOHUGO_EDITOR_WEB_DIR", "editor/web/dist"), "built React app directory")
	hugoAddr := flag.String("hugo-addr", envDefault("UVOOHUGO_EDITOR_HUGO_ADDR", "127.0.0.1:1313"), "local Hugo preview server address")
	publicURL := flag.String("public-url", os.Getenv("UVOOHUGO_EDITOR_PUBLIC_URL"), "public editor base URL used for Hugo preview links")
	authUser := flag.String("auth-user", os.Getenv("UVOOHUGO_EDITOR_AUTH_USER"), "HTTP Basic Auth username")
	authPassword := flag.String("auth-password", os.Getenv("UVOOHUGO_EDITOR_AUTH_PASSWORD"), "HTTP Basic Auth password")
	authPasswordFile := flag.String("auth-password-file", os.Getenv("UVOOHUGO_EDITOR_AUTH_PASSWORD_FILE"), "file containing HTTP Basic Auth password")
	startHugo := flag.Bool("start-hugo", envBool("UVOOHUGO_EDITOR_START_HUGO", true), "start Hugo preview server on launch")
	flag.Parse()

	if *authPassword == "" && *authPasswordFile != "" {
		password, err := os.ReadFile(*authPasswordFile)
		if err != nil {
			log.Fatalf("read auth password file: %v", err)
		}
		*authPassword = strings.TrimSpace(string(password))
	}
	if *authUser == "" || *authPassword == "" {
		log.Fatal("basic auth is required: set UVOOHUGO_EDITOR_AUTH_USER and UVOOHUGO_EDITOR_AUTH_PASSWORD, or pass -auth-user and -auth-password")
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
	return filepath.Join(s.siteDir, "hugo.toml")
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
