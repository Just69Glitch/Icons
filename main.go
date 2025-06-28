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
	"github.com/ncruces/zenity"
)

var (
	version = "1.0.0"
	author = "Just Glitch <https://github.com/Just69Glitch>"
)

const (
	configFile = "config.json"
)

type Config struct {
	Port string `json:"port"`
	IconDir string `json:"iconDir"`
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
	// Check if directory exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false
	}

	// Check if it's actually a directory
	if fileInfo, err := os.Stat(path); err != nil || !fileInfo.IsDir() {
		return false
	}

	// Read directory contents (non-recursive)
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}

	// Check for at least one .svg file in current directory
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".svg") {
			return true
		}
	}

	return false
}

func selectValidIconDir() (string, error) {
	for {
		// Prompt user to select directory
		dir, err := zenity.SelectFile(
			zenity.Directory(),
			zenity.Title("Select Folder with SVG Icons"),
		)
		if err != nil {
			return "", err // Return any error (including cancellation)
		}
		// Validate the selected directory
		if isValidIconDir(dir) {
			return dir, nil
		}

		// Show error and retry
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
		// Create default config
		cfg := &Config{
			IconDir: "nil",
			Port: "nil",
		}
		log.Printf("Creating default config: %s", configFile)
		return cfg, saveConfig(cfg)
	}

	// Read existing config
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

		files, err := getSortedIconNames(iconDir)
		if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		total := len(files)

		if !hasPage && !hasLimit {
			writeJSONResponse(w, files, 1, total, total)
			return
		}

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
	log.Printf("Starting Icon Server v%s by %s", version, author)
	// Load or create config
	cfg, err := loadOrCreateConfig()
	if err != nil {
		log.Fatalf("Error loading config: %v", err)
	}
	
	// Check if port is valid
	if !isValidPort(cfg.Port) {
		log.Printf("Invalid port in config: %s", cfg.Port)
		
		// Let user select a valid port
		newPort, err := promptForValidPort()
		if err != nil {
			if err == zenity.ErrCanceled {
				log.Println("User cancelled port selection")
				return
			}
			log.Fatalf("Error selecting port: %v", err)
		}
		
		// Update config
		cfg.Port = newPort
		if err := saveConfig(cfg); err != nil {
			log.Fatalf("Failed to save config: %v", err)
		}
	}

	// Check if iconDir exists and contains SVGs
	if !isValidIconDir(cfg.IconDir) {
		log.Printf("No valid SVG icons found in '%s'", cfg.IconDir)
		
		// Let user select a valid directory
		newDir, err := selectValidIconDir()
		if err != nil {
			if err == zenity.ErrCanceled {
				log.Println("User cancelled directory selection")
				return
			}
			log.Fatalf("Error selecting directory: %v", err)
		}
		
		// Update config
		cfg.IconDir = newDir
		if err := saveConfig(cfg); err != nil {
			log.Fatalf("Failed to save config: %v", err)
		}
	}

	log.Printf("Serving icons from: %s", cfg.IconDir)
	log.Printf("Using port: %s", cfg.Port)
	// HTTP Routes
	http.HandleFunc("/Icons/list", listIcons(cfg.IconDir))
	http.Handle("/Icons/", http.StripPrefix("/Icons", http.FileServer(http.Dir(cfg.IconDir))))

	fmt.Printf("Serving icons on http://localhost:%s/Icons/\n", cfg.Port)
	log.Printf("Server starting on port %s...", cfg.Port)
	if err := http.ListenAndServe(":"+cfg.Port, nil); err != nil {
			log.Fatalf("Server failed to start: %v", err)
	}
}