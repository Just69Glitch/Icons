package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/ncruces/zenity"
)

type Config struct {
	Port      string `json:"port"`
	IconDir   string `json:"iconDir"`
	DebugMode bool   `json:"debugMode"`
}

type IconCache struct {
	mu          sync.RWMutex
	icons       map[string][]byte // filename -> content
	names       []string          // sorted list of filenames
	nameIndex   map[string]int    // filename -> index in names slice
	searchIndex map[string][]int  // search term -> slice of indexes in names
	htmlPage    []byte            // cached HTML page
	gzippedPage []byte            // gzipped version of HTML page
	lastUpdated time.Time
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

type ansiStripper struct {
	w io.Writer
}

var (
	version = "1.2.1"
	author  = "Just Glitch <https://github.com/Just69Glitch>"
)

const (
	configFile = "config.json"
	logsDir    = "logs"
	timeFormat = "2006-01-02_15-04-05.000"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
	colorGray   = "\033[90m"
)

func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func stripANSI(input []byte) []byte {
	var buf bytes.Buffer
	inEscape := false

	for _, b := range input {
		if inEscape {
			if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
				inEscape = false
			}
			continue
		}

		if b == '\x1b' { // ANSI escape character
			inEscape = true
			continue
		}

		buf.WriteByte(b)
	}

	return buf.Bytes()
}

func setupSessionLogging() (*os.File, error) {
	// Create logs directory if it doesn't exist
	if err := os.MkdirAll(logsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %v", err)
	}

	// Generate filename with current timestamp
	logFileName := filepath.Join(logsDir, fmt.Sprintf("session_%s.log", time.Now().Format(timeFormat)))

	// Create or open the log file
	logFile, err := os.OpenFile(logFileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create log file: %v", err)
	}

	// Write session header
	fmt.Fprintf(logFile, "=== Icon Server Session Started at %s ===\n", time.Now().Format("2006-01-02 15:04:05.000 MST"))
	fmt.Fprintf(logFile, "Version: %s\n", version)
	fmt.Fprintf(logFile, "Author: %s\n\n", author)

	// Configure logging to both file and console
	log.SetOutput(io.MultiWriter(
		os.Stdout,                 // Keep colors for console
		&ansiStripper{w: logFile}, // Strip colors for file
	))

	// Add timestamp to each log entry
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	return logFile, nil
}

func (w *ansiStripper) Write(p []byte) (n int, err error) {
	return w.w.Write(stripANSI(p))
}

// RequestLogger is a middleware that logs all HTTP requests with colors
func RequestLogger(cfg *Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cfg.DebugMode {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ip := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			ip = forwarded
		}

		// Create a custom ResponseWriter to capture status code
		lrw := &loggingResponseWriter{ResponseWriter: w}

		// Call the next handler
		next.ServeHTTP(lrw, r)

		// Log the request details with colors
		duration := time.Since(start)
		iconName := "N/A"
		if strings.HasPrefix(r.URL.Path, "/Icons/") && !strings.HasPrefix(r.URL.Path, "/Icons/list") {
			iconName = strings.TrimPrefix(r.URL.Path, "/Icons/")
		}

		// Color the status code based on its value
		statusColor := colorGreen
		if lrw.statusCode >= 400 {
			statusColor = colorRed
		} else if lrw.statusCode >= 300 {
			statusColor = colorYellow
		}

		// Color the method based on HTTP verb
		methodColor := colorCyan
		switch r.Method {
		case "GET":
			methodColor = colorBlue
		case "POST":
			methodColor = colorGreen
		case "PUT", "PATCH":
			methodColor = colorYellow
		case "DELETE":
			methodColor = colorRed
		}

		log.Printf("%s%s%s %s%s%s %s%d%s %s%v%s - IP: %s%s%s, Icon: %s%s%s, User-Agent: %s%s%s",
			methodColor,
			r.Method,
			colorReset,
			colorPurple,
			r.URL.Path,
			colorReset,
			statusColor,
			lrw.statusCode,
			colorReset,
			colorYellow,
			duration,
			colorReset,
			colorCyan,
			ip,
			colorReset,
			colorGreen,
			iconName,
			colorReset,
			colorGray,
			r.UserAgent(),
			colorReset,
		)
	})
}

func insertCommas(n int) string {
	in := strconv.Itoa(n)
	if len(in) <= 3 {
		return in
	}
	var out strings.Builder
	mod := len(in) % 3
	if mod > 0 {
		out.WriteString(in[:mod])
		if len(in) > mod {
			out.WriteByte(',')
		}
	}
	for i := mod; i < len(in); i += 3 {
		out.WriteString(in[i : i+3])
		if i+3 < len(in) {
			out.WriteByte(',')
		}
	}
	return out.String()
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func isValidPort(portStr string) bool {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return false
	}
	return port > 0 && port < 65536
}

func promptForValidPort() (string, error) {
	for {
		port, err := zenity.Entry(
			"Please enter a valid port number (1-65535):",
			zenity.Title("Invalid Port"),
			zenity.EntryText(""),
		)
		if err != nil {
			return "", err
		}

		if isValidPort(port) {
			return port, nil
		}

		err = zenity.Error(
			"Invalid port number. Must be between 1 and 65535.",
			zenity.Title("Invalid Input"),
			zenity.ErrorIcon,
		)
		if err != nil {
			return "", err
		}
	}
}

func isValidIconDir(path string) bool {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}

	if fileInfo, err := os.Stat(path); err != nil || !fileInfo.IsDir() {
		return false
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".svg") {
			return true
		}
	}

	return false
}

func selectValidIconDir() (string, error) {
	for {
		dir, err := zenity.SelectFile(
			zenity.Directory(),
			zenity.Title("Select Folder with SVG Icons"),
		)
		if err != nil {
			return "", err
		}
		if isValidIconDir(dir) {
			return dir, nil
		}

		err = zenity.Error(
			"The selected directory doesn't contain any SVG files.\nPlease select a directory containing .svg icons.",
			zenity.Title("Invalid Directory"),
			zenity.ErrorIcon,
		)
		if err != nil {
			return "", err
		}
	}
}

func promptDebugMode() (bool, error) {
	err := zenity.Question(
		"Enable debug mode? This will log every request.",
		zenity.Title("Debug Mode"),
	)
	if err == zenity.ErrCanceled {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func loadOrCreateConfig() (*Config, error) {
	log.Printf("%sChecking config file: %s%s", colorGray, configFile, colorReset)

	// Check if config file exists
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Printf("%sNo config found. Creating new one.%s", colorYellow, colorReset)
		debugModeEnabled, err := promptDebugMode()
		if err != nil {
			return nil, fmt.Errorf("failed to get debug mode preference: %v", err)
		}

		cfg := &Config{
			IconDir:   "nil",
			Port:      "nil",
			DebugMode: debugModeEnabled,
		}
		log.Printf("%sCreating default config with DebugMode=%v%s", colorYellow, cfg.DebugMode, colorReset)
		if err := saveConfig(cfg); err != nil {
			return nil, fmt.Errorf("failed to save new config: %v", err)
		}
		return cfg, nil
	}

	// Load existing config
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %v", err)
	}
	log.Printf("%sFound config content%s", colorGray, colorReset)

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config JSON: %v", err)
	}

	// Validate the DebugMode field
	var tempMap map[string]interface{}
	if err := json.Unmarshal(data, &tempMap); err == nil {
		debugValue, exists := tempMap["debugMode"]
		if !exists || (debugValue != true && debugValue != false) {
			log.Printf("%sDebugMode is missing or invalid in config. Prompting user...%s", colorYellow, colorReset)
			debugModeEnabled, err := promptDebugMode()
			if err != nil {
				return nil, fmt.Errorf("failed to get debug mode preference: %v", err)
			}
			cfg.DebugMode = debugModeEnabled
			log.Printf("%sSetting DebugMode to %v in config%s", colorGreen, cfg.DebugMode, colorReset)
			if err := saveConfig(&cfg); err != nil {
				return nil, fmt.Errorf("failed to update config with DebugMode: %v", err)
			}
		}
	} else {
		log.Printf("%sFailed to unmarshal config into map for DebugMode validation%s", colorRed, colorReset)
	}

	log.Printf("%sLoaded config successfully. DebugMode=%v%s", colorGreen, cfg.DebugMode, colorReset)
	return &cfg, nil
}

func saveConfig(cfg *Config) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	log.Printf("%sSaving config to disk%s", colorGray, colorReset)
	return os.WriteFile(configFile, data, 0644)
}

func NewIconCache(iconDir string) (*IconCache, error) {
	cache := &IconCache{
		icons:       make(map[string][]byte),
		nameIndex:   make(map[string]int),
		searchIndex: make(map[string][]int),
	}
	if err := cache.Rebuild(iconDir); err != nil {
		return nil, err
	}
	return cache, nil
}

func (c *IconCache) Rebuild(iconDir string) error {
	files, err := getSortedIconNames(iconDir)
	if err != nil {
		return err
	}

	newIcons := make(map[string][]byte)
	newNameIndex := make(map[string]int)
	newSearchIndex := make(map[string][]int)

	for i, file := range files {
		content, err := os.ReadFile(filepath.Join(iconDir, file))
		if err != nil {
			return err
		}

		newIcons[file] = content
		newNameIndex[file] = i

		// Build search index (filename without .svg)
		name := strings.TrimSuffix(strings.ToLower(file), ".svg")
		for term := range strings.SplitSeq(name, "-") {
			if len(term) >= 2 {
				newSearchIndex[term] = append(newSearchIndex[term], i)
			}
		}
	}

	// Build HTML page
	var htmlBuilder strings.Builder
	htmlBuilder.WriteString(`<!DOCTYPE html><html><head><title>Icon Server - All Icons</title><style>body { font-family: Arial, sans-serif; margin: 20px; }.icon-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(250px, 1fr)); gap: 15px; }.icon-item { text-align: center; padding: 10px; border: 1px solid #eee; border-radius: 5px; }.icon-item:hover { background-color: #f5f5f5; }.icon-img { height: 50px; width: 50px; margin-bottom: 5px; }.icon-name { word-break: break-all; font-size: 12px; }</style></head><body><h1>Available Icons (Total: ` + insertCommas(len(files)) + `)</h1><div class="icon-grid">`)

	for _, name := range files {
		htmlBuilder.WriteString(`<div class="icon-item"><a href="/Icons/`)
		htmlBuilder.WriteString(name)
		htmlBuilder.WriteString(`"><div class="icon-name">`)
		htmlBuilder.WriteString(name)
		htmlBuilder.WriteString(`</div></a></div>`)
	}

	htmlBuilder.WriteString(`</div></body></html>`)

	htmlPage := []byte(htmlBuilder.String())
	var gzippedBuf bytes.Buffer
	gz := gzip.NewWriter(&gzippedBuf)
	if _, err := gz.Write(htmlPage); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.icons = newIcons
	c.names = files
	c.nameIndex = newNameIndex
	c.searchIndex = newSearchIndex
	c.htmlPage = htmlPage
	c.gzippedPage = gzippedBuf.Bytes()
	c.lastUpdated = time.Now()

	return nil
}

func (c *IconCache) GetIcon(name string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	content, exists := c.icons[name]
	return content, exists
}

func (c *IconCache) Search(query string) []string {
	query = strings.ToLower(query)
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(query) < 2 {
		return nil
	}

	var results []string
	if indexes, exists := c.searchIndex[query]; exists {
		for _, idx := range indexes {
			results = append(results, c.names[idx])
		}
	} else {
		// Fallback to full scan if not in index
		for _, name := range c.names {
			if strings.Contains(strings.ToLower(name), query) {
				results = append(results, name)
			}
		}
	}
	return results
}

func (c *IconCache) GetHTML() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.htmlPage
}

func (c *IconCache) GetGzippedHTML() []byte {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.gzippedPage
}

func getSortedIconNames(iconDir string) ([]string, error) {
	entries, err := fs.ReadDir(os.DirFS(iconDir), ".")
	if err != nil {
		return nil, err
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".svg") {
			files = append(files, e.Name())
		}
	}

	sort.Strings(files)
	return files, nil
}

func writeJSONResponse(w http.ResponseWriter, files []string, page, limit, total int) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"page":%d,"limit":%d,"total":%d,"files":[`, page, limit, total)

	for i, f := range files {
		fmt.Fprintf(w, `"%s"`, f)
		if i < len(files)-1 {
			fmt.Fprintf(w, ",")
		}
	}

	fmt.Fprintf(w, `]}`)
}

func iconHandler(cfg *Config, cache *IconCache) http.Handler {
	return RequestLogger(cfg, CORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle requests for the root path "/Icons/" or "/Icons"
		if r.URL.Path == "/Icons/" || r.URL.Path == "/Icons" {
			if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(cache.GetGzippedHTML())
			} else {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(cache.GetHTML())
			}
			return
		}

		// Handle requests for specific icons
		iconName := strings.TrimPrefix(r.URL.Path, "/Icons/")
		if content, exists := cache.GetIcon(iconName); exists {
			w.Header().Set("Content-Type", "image/svg+xml")
			w.Write(content)
		} else {
			http.NotFound(w, r)
		}
	})))
}

func listHandler(cfg *Config, cache *IconCache) http.Handler {
	return RequestLogger(cfg, CORSMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		searchQuery := strings.TrimSpace(strings.ToLower(query.Get("search")))

		// Get pagination parameters
		page := 1
		limit := 1000
		hasPage := query.Has("page")
		hasLimit := query.Has("limit")

		if hasPage || hasLimit {
			pageStr := query.Get("page")
			limitStr := query.Get("limit")

			var err error
			if page, err = strconv.Atoi(pageStr); err != nil || page < 1 {
				page = 1
			}

			if limit, err = strconv.Atoi(limitStr); err != nil || limit < 1 {
				limit = 1000
			}
		}

		var files []string
		if searchQuery != "" {
			files = cache.Search(searchQuery)
		} else {
			cache.mu.RLock()
			files = append([]string{}, cache.names...)
			cache.mu.RUnlock()
		}

		total := len(files)

		// If no pagination parameters and no search, return all files
		if !hasPage && !hasLimit && searchQuery == "" {
			writeJSONResponse(w, files, 1, total, total)
			return
		}

		// If search is provided but no pagination, return all matching results
		if searchQuery != "" && !hasPage && !hasLimit {
			writeJSONResponse(w, files, 1, total, total)
			return
		}

		// Apply pagination
		start := (page - 1) * limit
		end := start + limit

		if start > total {
			start = total
		}
		if end > total {
			end = total
		}

		writeJSONResponse(w, files[start:end], page, limit, total)
	})))
}

func watchDirectory(dir string, cache *IconCache) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("%sFailed to create watcher: %v%s", colorRed, err, colorReset)
		return
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0 {
					if strings.HasSuffix(strings.ToLower(event.Name), ".svg") {
						log.Printf("%sDetected change in icons, rebuilding cache...%s", colorYellow, colorReset)
						time.Sleep(500 * time.Millisecond) // debounce
						if err := cache.Rebuild(dir); err != nil {
							log.Printf("%sError rebuilding cache: %v%s", colorRed, err, colorReset)
						} else {
							log.Printf("%sCache rebuilt successfully%s", colorGreen, colorReset)
						}
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("%sWatcher error: %v%s", colorRed, err, colorReset)
			}
		}
	}()

	if err := watcher.Add(dir); err != nil {
		log.Printf("%sError watching directory: %v%s", colorRed, err, colorReset)
		return
	}
	<-done
}

func main() {
	logFile, err := setupSessionLogging()
	if err != nil {
		log.Fatalf("%sError setting up logging: %v%s", colorRed, err, colorReset)
	}

	defer func() {
		// Write session end timestamp
		fmt.Fprintf(logFile, "\n=== Session Ended at %s ===\n",
			time.Now().Format("2006-01-02 15:04:05.000 MST"))
		logFile.Close()
	}()

	log.Printf("%sStarting Icon Server v%s by %s%s", colorCyan, version, author, colorReset)
	cfg, err := loadOrCreateConfig()
	if err != nil {
		log.Fatalf("%sError loading config: %v%s", colorRed, err, colorReset)
	}

	if !isValidPort(cfg.Port) {
		log.Printf("%sInvalid port in config: %s%s", colorYellow, cfg.Port, colorReset)

		newPort, err := promptForValidPort()
		if err != nil {
			if err == zenity.ErrCanceled {
				log.Printf("%sUser cancelled port selection%s", colorYellow, colorReset)
				return
			}
			log.Fatalf("%sError selecting port: %v%s", colorRed, err, colorReset)
		}

		cfg.Port = newPort
		if err := saveConfig(cfg); err != nil {
			log.Fatalf("%sFailed to save config: %v%s", colorRed, err, colorReset)
		}
	}

	if !isValidIconDir(cfg.IconDir) {
		log.Printf("%sNo valid SVG icons found in '%s'%s", colorYellow, cfg.IconDir, colorReset)

		newDir, err := selectValidIconDir()
		if err != nil {
			if err == zenity.ErrCanceled {
				log.Printf("%sUser cancelled directory selection%s", colorYellow, colorReset)
				return
			}
			log.Fatalf("%sError selecting directory: %v%s", colorRed, err, colorReset)
		}

		cfg.IconDir = newDir
		if err := saveConfig(cfg); err != nil {
			log.Fatalf("%sFailed to save config: %v%s", colorRed, err, colorReset)
		}
	}

	log.Printf("%sServing icons from: %s%s", colorGreen, cfg.IconDir, colorReset)
	log.Printf("%sUsing port: %s%s", colorGreen, cfg.Port, colorReset)

	log.Printf("%sLoading icons...%s", colorCyan, colorReset)
	start := time.Now()
	cache, err := NewIconCache(cfg.IconDir)
	elapsed := time.Since(start)
	if err != nil {
		log.Fatalf("%sError initializing icon cache: %v%s", colorRed, err, colorReset)
	}
	formattedCount := insertCommas(len(cache.names))
	log.Printf("%sLoaded: %s[%s]%s icons successfully%s in [%s%.2fs%s]", colorGreen, colorRed, formattedCount, colorGreen, colorReset, colorGray, elapsed.Seconds(), colorReset)

	// Start filesystem watcher
	go watchDirectory(cfg.IconDir, cache)

	http.Handle("/Icons/", iconHandler(cfg, cache))
	http.Handle("/Icons/list", listHandler(cfg, cache))

	log.Printf("%sServing icons on http://localhost:%s/Icons/%s\n", colorCyan, cfg.Port, colorReset)
	log.Printf("%sServer starting on port %s...%s", colorGreen, cfg.Port, colorReset)
	if err := http.ListenAndServe(":"+cfg.Port, nil); err != nil {
		log.Fatalf("%sServer failed to start: %v%s", colorRed, err, colorReset)
	}
}
