package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ncruces/zenity"
)

var (
	version = "1.1.0"
	author  = "Just Glitch <https://github.com/Just69Glitch>"
)

const (
	configFile = "config.json"
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

type Config struct {
	Port    string `json:"port"`
	IconDir string `json:"iconDir"`
}

// RequestLogger is a middleware that logs all HTTP requests with colors
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		log.Printf("%s[%s]%s %s%s%s %s%s%s %s%d%s %s%v%s - IP: %s%s%s, Icon: %s%s%s, User-Agent: %s%s%s",
			colorGray,
			start.Format("2006-01-02 15:04:05"),
			colorReset,
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

// loggingResponseWriter wraps http.ResponseWriter to capture status code
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
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

func loadOrCreateConfig() (*Config, error) {
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		cfg := &Config{
			IconDir: "nil",
			Port:    "nil",
		}
		log.Printf("%sCreating default config: %s%s", colorYellow, configFile, colorReset)
		return cfg, saveConfig(cfg)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	if !isValidPort(cfg.Port) {
		newPort, err := promptForValidPort()
		if err != nil {
			return nil, err
		}
		cfg.Port = newPort
		if err := saveConfig(&cfg); err != nil {
			return nil, err
		}
	}

	return &cfg, nil
}

func saveConfig(cfg *Config) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(configFile, data, 0644)
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

func listIcons(iconDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		hasPage := query.Has("page")
		hasLimit := query.Has("limit")
		searchQuery := strings.TrimSpace(strings.ToLower(query.Get("search")))
		page := 1
		limit := 1000

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

		allFiles, err := getSortedIconNames(iconDir)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		var files []string
		if len(searchQuery) > 0 && len(searchQuery) < 2 {
			files = []string{}
		} else if len(searchQuery) >= 2 {
			// If the search query is at least 2 characters, filter the files
			for _, file := range allFiles {
				// Remove .svg extension for comparison
				fileName := strings.TrimSuffix(strings.ToLower(file), ".svg")
				if strings.Contains(fileName, searchQuery) {
					files = append(files, file)
				}
			}
		} else {
			// No search query, return all files
			files = allFiles
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
	}
}

func main() {
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

	iconHandler := RequestLogger(CORSMiddleware(http.StripPrefix("/Icons", http.FileServer(http.Dir(cfg.IconDir)))))
	http.Handle("/Icons/", iconHandler)

	listHandler := RequestLogger(CORSMiddleware(http.HandlerFunc(listIcons(cfg.IconDir))))
	http.Handle("/Icons/list", listHandler)

	fmt.Printf("%sServing icons on http://localhost:%s/Icons/%s\n", colorCyan, cfg.Port, colorReset)
	log.Printf("%sServer starting on port %s...%s", colorGreen, cfg.Port, colorReset)
	if err := http.ListenAndServe(":"+cfg.Port, nil); err != nil {
		log.Fatalf("%sServer failed to start: %v%s", colorRed, err, colorReset)
	}
}
