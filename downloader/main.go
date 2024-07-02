package main

import (
	"database/sql"
	"fmt"
	"github.com/go-telegram-bot-api/telegram-bot-api"
	_ "github.com/mattn/go-sqlite3"
	"github.com/rabbitmq/amqp091-go"
	"log"
	"os"
	"os/exec"
)

var (
	rabbitMQUrl       = os.Getenv("RABBITMQ_URL")
	telegramBotToken  = os.Getenv("TELEGRAM_BOT_TOKEN")
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
	cmd := exec.Command("youtube-dl", "-o", "/tmp/video.mp4", url)
	err := cmd.Run()
	if err != nil {
		return "", err
	}
	return "/tmp/video.mp4", nil
}

func main() {
	db, err := initDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	conn, err := amqp091.Dial(rabbitMQUrl)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
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
		log.Fatal(err)
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
		log.Fatal(err)
	}

	bot, err := tgbotapi.NewBotAPI(telegramBotToken)
	if err != nil {
		log.Panic(err)
	}

	for d := range msgs {
		url := string(d.Body)
		chatID := d.Headers["chat_id"].(int64)
		userID := d.Headers["user_id"].(int64)
		username := d.Headers["username"].(string)
		firstName := d.Headers["first_name"].(string)
		lastName := d.Headers["last_name"].(string)

		filePath, err := downloadVideo(url)
		if err != nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Failed to download video: %v", err))
			bot.Send(msg)
			continue
		}

		// Save user information to the database
		_, err = db.Exec(insertUserQuery, userID, username, firstName, lastName)
		if err != nil {
			log.Printf("Failed to save user information: %v", err)
		}

		// Save the download information to the database
		_, err = db.Exec(insertDownloadQuery, userID, url)
		if err != nil {
			log.Printf("Failed to save download information: %v", err)
		}

		// Save the URL to the database
		_, err = db.Exec(insertProcessedURLQuery, url)
		if err != nil {
			log.Printf("Failed to save URL: %v", err)
		}

		file, err := os.Open(filePath)
		if err != nil {
			msg := tgbotapi.NewMessage(chatID, fmt.Sprintf("Failed to open video: %v", err))
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
	}
}
