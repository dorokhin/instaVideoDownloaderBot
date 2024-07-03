package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/go-telegram-bot-api/telegram-bot-api"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rabbitmq/amqp091-go"
)

var (
	rabbitMQUrl       = os.Getenv("RABBITMQ_URL")
	telegramBotToken  = os.Getenv("TELEGRAM_BOT_TOKEN")
	instagramUsername = os.Getenv("INSTAGRAM_USERNAME")
	instagramPassword = os.Getenv("INSTAGRAM_PASSWORD")
	databaseFile      = "/app/data/videos.db"
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

func downloadVideo(url string) (string, error) {
	outputPath := "/tmp/video.mp4"
	cmd := exec.Command("yt-dlp", "-o", outputPath, "--username", instagramUsername, "--password", instagramPassword, url)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to download video: %s", string(output))
		return "", err
	}
	return outputPath, nil
}

func main() {
	log.Println("Starting downloader service")

	db, err := initDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	log.Println("Database initialized")

	conn, err := amqp091.Dial(rabbitMQUrl)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()
	log.Println("Connected to RabbitMQ")

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
	}
	defer ch.Close()
	log.Println("Channel opened")

	q, err := ch.QueueDeclare(
		"video_download",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Queue declared")

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
		log.Fatal(err)
	}
	log.Println("Started consuming messages")

	bot, err := tgbotapi.NewBotAPI(telegramBotToken)
	if err != nil {
		log.Panic(err)
	}
	log.Println("Telegram bot initialized")

	for d := range msgs {
		log.Println("Received a message")
		url := string(d.Body)

		chatID, ok := d.Headers["chat_id"].(int64)
		if !ok {
			chatID32 := d.Headers["chat_id"].(int32)
			chatID = int64(chatID32)
		}

		userID, ok := d.Headers["user_id"].(int64)
		if !ok {
			userID32 := d.Headers["user_id"].(int32)
			userID = int64(userID32)
		}

		username, ok := d.Headers["username"].(string)
		if !ok {
			log.Printf("Invalid type for username")
			continue
		}

		firstName, ok := d.Headers["first_name"].(string)
		if !ok {
			log.Printf("Invalid type for first_name")
			continue
		}

		lastName, ok := d.Headers["last_name"].(string)
		if !ok {
			log.Printf("Invalid type for last_name")
			continue
		}

		log.Printf("Downloading video from URL: %s", url)
		filePath, err := downloadVideo(url)
		if err != nil {
			errorMessage := fmt.Sprintf("Failed to download video: %v", err)
			log.Println(errorMessage)
			msg := tgbotapi.NewMessage(chatID, errorMessage)
			bot.Send(msg)
			continue
		}
		log.Printf("Video downloaded to: %s", filePath)

		// Save user information to the database
		_, err = db.Exec(insertUserQuery, userID, username, firstName, lastName)
		if err != nil {
			log.Printf("Failed to save user information: %v", err)
		} else {
			log.Println("User information saved")
		}

		// Save the download information to the database
		_, err = db.Exec(insertDownloadQuery, userID, url)
		if err != nil {
			log.Printf("Failed to save download information: %v", err)
		} else {
			log.Println("Download information saved")
		}

		// Save the URL to the database
		_, err = db.Exec(insertProcessedURLQuery, url)
		if err != nil {
			log.Printf("Failed to save URL: %v", err)
		} else {
			log.Println("URL information saved")
		}

		file, err := os.Open(filePath)
		if err != nil {
			errorMessage := fmt.Sprintf("Failed to open video: %v", err)
			log.Println(errorMessage)
			msg := tgbotapi.NewMessage(chatID, errorMessage)
			bot.Send(msg)
			continue
		}
		defer file.Close()

		msg := tgbotapi.NewDocumentUpload(chatID, tgbotapi.FileReader{
			Name:   "video.mp4",
			Reader: file,
			Size:   -1,
		})
		bot.Send(msg)
		log.Println("Video sent to user")
	}
}
