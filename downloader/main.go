package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rabbitmq/amqp091-go"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type UserInfo struct {
	UserID    int64  `json:"user_id"`
	UserName  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type DownloadTask struct {
	URL       string   `json:"url"`
	ChatID    int64    `json:"chat_id"`
	MessageID int      `json:"message_id"`
	User      UserInfo `json:"user"`
}

type DownloadResult struct {
	URL          string `json:"url"`
	FilePath     string `json:"file_path"`
	Size         int64  `json:"size"`
	PreviewImage string `json:"preview_image"`
	Tags         string `json:"tags"`
	Description  string `json:"description"`
	ChatID       int64  `json:"chat_id"`
	MessageID    int    `json:"message_id"`
}

var (
	cookiesFilePath = os.Getenv("COOKIES_FILE_PATH")
	databaseFile    = "/app/data/videos.db"
)

var (
	createTablesQuery = `
	CREATE TABLE IF NOT EXISTS processed_urls (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		file_size INTEGER,
		preview_image TEXT,
		tags TEXT,
		description TEXT
	);
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL,
		username TEXT,
		first_name TEXT,
		last_name TEXT,
		total_bytes_downloaded INTEGER DEFAULT 0
	);
	CREATE TABLE IF NOT EXISTS downloads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		url TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		file_size INTEGER,
		preview_image TEXT,
		tags TEXT,
		description TEXT,
		FOREIGN KEY (user_id) REFERENCES users (id)
	);
	`
	insertUserQuery         = `INSERT OR IGNORE INTO users (user_id, username, first_name, last_name) VALUES (?, ?, ?, ?)`
	updateUserQuery         = `UPDATE users SET total_bytes_downloaded = total_bytes_downloaded + ? WHERE user_id = ?`
	insertDownloadQuery     = `INSERT INTO downloads (user_id, url, file_size, preview_image, tags, description) VALUES (?, ?, ?, ?, ?, ?)`
	insertProcessedURLQuery = `INSERT INTO processed_urls (url, file_size, preview_image, tags, description) VALUES (?, ?, ?, ?, ?)`
)

func GetEnv(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		value = fallback
	}
	return value
}

func init() {
	if cookiesFilePath == "" {
		log.Fatal("COOKIES_FILE_PATH environment variable is not set")
	}
	fmt.Printf("Cookies File Path: %s\n", cookiesFilePath)
	cookiesContent, err := os.ReadFile(cookiesFilePath)
	if err != nil {
		log.Fatalf("Failed to read cookies file: %v", err)
	}
	fmt.Printf("Cookies File Content:\n%s\n", string(cookiesContent))
}

func downloadVideo(url string) (string, int64, string, string, string, error) {
	id := uuid.New()
	// Determine file extension (assume mp4 for simplicity)
	fileExt := "mp4" // yt-dlp will determine the correct extension
	outputPath := fmt.Sprintf("/tmp/%s.%s", id.String(), fileExt)
	log.Println("Starting downloading video to: ", outputPath)
	cmd := exec.Command("yt-dlp", "-o", fmt.Sprintf("/tmp/%s.%%(ext)s", id.String()), "--cookies", cookiesFilePath, "--write-info-json", url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to download video: %s", string(output))
		return "", 0, "", "", "", err
	}
	infoFile := fmt.Sprintf("/tmp/%s.info.json", id.String())
	info, err := os.ReadFile(infoFile)
	if err != nil {
		log.Printf("Failed to read info file: %s", err)
		return "", 0, "", "", "", err
	}

	var size int64
	var previewImage, tags, description string
	infoJSON := map[string]interface{}{}
	err = json.Unmarshal(info, &infoJSON)
	if err != nil {
		log.Printf("Failed to parse info JSON: %s", err)
	} else {
		if fileSize, ok := infoJSON["filesize_approx"].(float64); ok {
			size = int64(fileSize)
		} else if fileSize, ok := infoJSON["filesize"].(float64); ok {
			size = int64(fileSize)
		}
		if thumbnail, ok := infoJSON["thumbnail"].(string); ok {
			previewImage = thumbnail
		}
		if tagsList, ok := infoJSON["tags"].([]interface{}); ok {
			var tagsArr []string
			for _, tag := range tagsList {
				if tagStr, ok := tag.(string); ok {
					tagsArr = append(tagsArr, tagStr)
				}
			}
			tags = strings.Join(tagsArr, ", ")
		}
		if desc, ok := infoJSON["description"].(string); ok {
			description = desc
		}
	}

	err = os.Remove(infoFile)
	if err != nil {
		log.Printf("Failed to delete video file: %s %v", infoFile, err)
	} else {
		log.Println("Successfully deleted info file: ", infoFile)
	}

	return outputPath, size, previewImage, tags, description, nil
}

func initDB() (*sql.DB, error) {
	log.Println("Initializing database")
	db, err := sql.Open("sqlite3", databaseFile)
	if err != nil {
		return nil, err
	}

	_, err = db.Exec(createTablesQuery)
	if err != nil {
		return nil, err
	}

	return db, nil
}

// Function to get random user agent
func getRandomUserAgent() string {
	userAgents := []string{
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.1 Safari/605.1.15",
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:89.0) Gecko/20100101 Firefox/89.0",
	}
	return userAgents[rand.Intn(len(userAgents))]
}

// Function to get file extension from content type
func getFileExtension(contentType string) string {
	switch contentType {
	case "image/jpeg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	default:
		return ""
	}
}

func DownloadImage(url string) (string, error) {
	// Get random user agent
	userAgent := getRandomUserAgent()

	// Create HTTP request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to create request: %v", err)
		log.Println(errMsg)
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)

	// Perform HTTP request
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		errMsg := fmt.Sprintf("Failed to download image: %v", err)
		log.Println(errMsg)
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println("Failed to close response body")
		}
	}(resp.Body)

	// Generate filename
	uuidFilename := uuid.New().String()
	fileExtension := getFileExtension(resp.Header.Get("Content-Type"))
	currentDate := time.Now().Format("2006_01_02")
	filename := fmt.Sprintf("%s.%s", uuidFilename, fileExtension)
	filename = filepath.Join(currentDate, filename)

	// Save file
	filePath := filepath.Join(GetEnv("DOWNLOAD_DIR", "/static"), filename)
	file, err := os.Create(filePath)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to create file: %v", err)
		log.Println(errMsg)
		return "", err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			log.Println("Failed to close file")
		}
	}(file)

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		errMsg := fmt.Sprintf("Failed to save file: %v", err)
		log.Println(errMsg)
		return "", err
	}

	log.Printf("Image from url: %s downloaded and saved as %s to %s", url, filename, filePath)

	return filename, nil
}

func main() {
	log.Println("Starting downloader service")
	conn, err := amqp091.Dial(GetEnv("RABBITMQ_URL", ""))
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatalf("Failed to open a channel: %v", err)
	}
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"video_download",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	msgs, err := ch.Consume(
		q.Name,
		"",
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to register a consumer: %v", err)
	}

	completionQueue, err := ch.QueueDeclare(
		"video_download_completion",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Failed to declare a queue: %v", err)
	}

	db, err := initDB()
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer db.Close()

	forever := make(chan bool)

	go func() {
		for d := range msgs {
			var task DownloadTask
			err := json.Unmarshal(d.Body, &task)
			if err != nil {
				log.Printf("Failed to unmarshal task: %v", err)
				continue
			}

			// Save user information to the database
			_, err = db.Exec(insertUserQuery, task.User.UserID, task.User.UserName, task.User.FirstName, task.User.LastName)
			if err != nil {
				log.Printf("Failed to save user information: %v", err)
			} else {
				log.Println("User information saved")
			}

			log.Println("Accepted task for download video from: ", task.URL, "ChatID: ", task.ChatID)
			filePath, size, previewImageUrl, tags, description, err := downloadVideo(task.URL)
			log.Println("Completed task for download video from: ", task.URL, "saved to: ", filePath, "size: ", size, "preview image: ", previewImageUrl, "tags: ", tags, "description: ", description)
			if err != nil {
				log.Printf("Failed to download video: %v", err)
				continue
			}

			previewImage, err := DownloadImage(previewImageUrl)
			if err != nil {
				log.Printf("Failed to download preview image: %v", err)
			}

			// Save the download information to the database
			_, err = db.Exec(insertDownloadQuery, task.User.UserID, task.URL, size, previewImage, tags, description)
			if err != nil {
				log.Printf("Failed to save download information: %v", err)
			} else {
				log.Println("Download information saved")
			}

			_, err = db.Exec(insertProcessedURLQuery, task.URL, size, previewImage, tags, description)

			// Update user total bytes downloaded
			_, err = db.Exec(updateUserQuery, size, task.User.UserID)
			if err != nil {
				log.Printf("Failed to update user total bytes downloaded: %v", err)
			} else {
				log.Println("User total bytes downloaded updated")
				if err != nil {
					log.Printf("Failed to save URL: %v", err)
				} else {
					log.Println("URL information saved")
				}

				result := DownloadResult{
					URL:          task.URL,
					FilePath:     filePath,
					Size:         size,
					PreviewImage: previewImage,
					Tags:         tags,
					Description:  description,
					ChatID:       task.ChatID,
					MessageID:    task.MessageID,
				}
				body, err := json.Marshal(result)
				if err != nil {
					log.Printf("Failed to marshal result: %v", err)
					continue
				}

				err = ch.Publish(
					"",
					completionQueue.Name,
					false,
					false,
					amqp091.Publishing{
						ContentType: "application/json",
						Body:        body,
					})
				if err != nil {
					log.Printf("Failed to publish result: %v", err)
					continue
				}

			}
		}
	}()

	log.Printf("Waiting for messages. To exit press CTRL+C")
	<-forever
}
