package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	_ "github.com/mattn/go-sqlite3"
	"github.com/rabbitmq/amqp091-go"
)

type DownloadTask struct {
	URL       string `json:"url"`
	ChatID    int64  `json:"chat_id"`
	MessageID int    `json:"message_id"`
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
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL,
		username TEXT,
		first_name TEXT,
		last_name TEXT
	);
	CREATE TABLE IF NOT EXISTS downloads (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		url TEXT NOT NULL,
		timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (user_id) REFERENCES users (id)
	);
	`
	insertUserQuery         = `INSERT OR IGNORE INTO users (user_id, username, first_name, last_name) VALUES (?, ?, ?, ?)`
	insertDownloadQuery     = `INSERT INTO downloads (user_id, url) VALUES (?, ?)`
	insertProcessedURLQuery = `INSERT INTO processed_urls (url) VALUES (?)`
)

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
	outputPath := "/tmp/video.mp4"
	cmd := exec.Command("yt-dlp", "-o", "/tmp/video.%(ext)s", "--cookies", cookiesFilePath, "--write-info-json", url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to download video: %s", string(output))
		return "", 0, "", "", "", err
	}
	infoFile := "/tmp/video.info.json"
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

	return outputPath, size, previewImage, tags, description, nil
}

func initDB() (*sql.DB, error) {
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

func main() {
	log.Println("Starting downloader service")
	conn, err := amqp091.Dial(os.Getenv("RABBITMQ_URL"))
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

			filePath, size, previewImage, tags, description, err := downloadVideo(task.URL)
			if err != nil {
				log.Printf("Failed to download video: %v", err)
				continue
			}

			_, err = db.Exec(
				`INSERT INTO processed_urls (url, file_size, preview_image, tags, description) VALUES (?, ?, ?, ?, ?)`,
				task.URL, size, previewImage, tags, description,
			)
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

			err = os.Remove(filePath)
			if err != nil {
				log.Printf("Failed to delete video file: %v", err)
			}
		}
	}()

	log.Printf("Waiting for messages. To exit press CTRL+C")
	<-forever
}
