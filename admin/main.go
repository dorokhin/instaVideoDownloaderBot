package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"text/template"

	_ "github.com/mattn/go-sqlite3"
)

var (
	databaseFile = "/app/data/videos.db"
	logger       *log.Logger
)

func init() {
	// Create or open the log file
	logFile, err := os.OpenFile("app.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}

	// Create a new logger
	logger = log.New(logFile, "INFO: ", log.Ldate|log.Ltime|log.Lshortfile)
}

func initDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", databaseFile)
	if err != nil {
		logger.Printf("Failed to connect to database: %v", err)
		return nil, err
	}
	return db, nil
}

type ProcessedURL struct {
	URL          string
	Timestamp    string
	FileSize     int64
	PreviewImage string
	Tags         string
	Description  string
}

type UserDownload struct {
	UserID       int64
	Username     string
	FirstName    string
	LastName     string
	URL          string
	Timestamp    string
	FileSize     int64
	PreviewImage string
	Tags         string
	Description  string
}

type Statistics struct {
	TotalFileSize  int64
	TotalDownloads int
	UserDownloads  []UserDownload
}

func loadTemplate(name string) (*template.Template, error) {
	return template.ParseFiles("templates/" + name + ".html")
}

func processedURLsHandler(w http.ResponseWriter, r *http.Request) {
	db, err := initDB()
	if err != nil {
		logger.Printf("Error initializing DB: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	rows, err := db.Query("SELECT DISTINCT url, timestamp, file_size, preview_image, tags, description FROM processed_urls")
	if err != nil {
		logger.Printf("Error querying processed URLs: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var processedURLs []ProcessedURL
	for rows.Next() {
		var url ProcessedURL
		if err := rows.Scan(&url.URL, &url.Timestamp, &url.FileSize, &url.PreviewImage, &url.Tags, &url.Description); err != nil {
			logger.Printf("Error scanning row: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		processedURLs = append(processedURLs, url)
	}

	tmpl, err := loadTemplate("processed_urls")
	if err != nil {
		logger.Printf("Error loading template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, processedURLs); err != nil {
		logger.Printf("Error executing template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func userDownloadsHandler(w http.ResponseWriter, r *http.Request) {
	db, err := initDB()
	if err != nil {
		logger.Printf("Error initializing DB: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	rows, err := db.Query(`
		SELECT DISTINCT u.user_id, u.username, u.first_name, u.last_name, d.url, d.timestamp, d.file_size, d.preview_image, d.tags, d.description
		FROM downloads d
		JOIN users u ON d.user_id = u.user_id
	`)
	if err != nil {
		logger.Printf("Error querying user downloads: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var userDownloads []UserDownload
	for rows.Next() {
		var download UserDownload
		if err := rows.Scan(&download.UserID, &download.Username, &download.FirstName, &download.LastName, &download.URL, &download.Timestamp, &download.FileSize, &download.PreviewImage, &download.Tags, &download.Description); err != nil {
			logger.Printf("Error scanning row: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		userDownloads = append(userDownloads, download)
	}

	tmpl, err := loadTemplate("user_downloads")
	if err != nil {
		logger.Printf("Error loading template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, userDownloads); err != nil {
		logger.Printf("Error executing template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func statisticsHandler(w http.ResponseWriter, r *http.Request) {
	db, err := initDB()
	if err != nil {
		logger.Printf("Error initializing DB: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close()

	var totalFileSize int64
	var totalDownloads int

	err = db.QueryRow("SELECT SUM(file_size) FROM processed_urls").Scan(&totalFileSize)
	if err != nil {
		logger.Printf("Error querying total file size: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = db.QueryRow("SELECT COUNT(DISTINCT url) FROM processed_urls").Scan(&totalDownloads)
	if err != nil {
		logger.Printf("Error querying total downloads: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	rows, err := db.Query(`
		SELECT DISTINCT u.user_id, u.username, u.first_name, u.last_name, d.url, d.timestamp, d.file_size, d.preview_image, d.tags, d.description
		FROM downloads d
		JOIN users u ON d.user_id = u.user_id
	`)
	if err != nil {
		logger.Printf("Error querying user downloads: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var userDownloads []UserDownload
	for rows.Next() {
		var download UserDownload
		if err := rows.Scan(&download.UserID, &download.Username, &download.FirstName, &download.LastName, &download.URL, &download.Timestamp, &download.FileSize, &download.PreviewImage, &download.Tags, &download.Description); err != nil {
			logger.Printf("Error scanning row: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		userDownloads = append(userDownloads, download)
	}

	statistics := Statistics{
		TotalFileSize:  totalFileSize,
		TotalDownloads: totalDownloads,
		UserDownloads:  userDownloads,
	}

	tmpl, err := loadTemplate("statistics")
	if err != nil {
		logger.Printf("Error loading template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := tmpl.Execute(w, statistics); err != nil {
		logger.Printf("Error executing template: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func serveStaticFiles(directory string, allowDirListing bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowDirListing && r.URL.Path == "/" {
			http.Error(w, "Directory listing is not allowed", http.StatusForbidden)
			return
		}
		http.FileServer(http.Dir(directory)).ServeHTTP(w, r)
	})
}

func GetEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}

func main() {
	// Read environment variables
	downloadDir := GetEnv("DOWNLOAD_DIR", "/static")

	allowDirListing := GetEnv("ALLOW_DIR_LISTING", "false") == "true"

	http.HandleFunc("/processed_urls", processedURLsHandler)
	http.HandleFunc("/user_downloads", userDownloadsHandler)
	http.HandleFunc("/statistics", statisticsHandler)
	http.Handle("/static/", http.StripPrefix("/static", serveStaticFiles(downloadDir, allowDirListing)))
	log.Fatal(http.ListenAndServe(":8080", nil))
}
