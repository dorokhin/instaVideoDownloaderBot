package main

import (
	"encoding/json"
	"log"
	"os"

	"github.com/go-telegram-bot-api/telegram-bot-api"
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

func main() {
	bot, err := tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, _ := bot.GetUpdatesChan(u)

	conn, err := amqp091.Dial(os.Getenv("RABBITMQ_URL"))
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		log.Fatal(err)
	}
	defer ch.Close()

	downloadQueue, err := ch.QueueDeclare(
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

	completionQueue, err := ch.QueueDeclare(
		"video_download_completion",
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
		completionQueue.Name,
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

	go func() {
		for d := range msgs {
			var result DownloadResult
			err := json.Unmarshal(d.Body, &result)
			if err != nil {
				log.Printf("Failed to unmarshal result: %v", err)
				continue
			}

			msg := tgbotapi.NewVideoUpload(result.ChatID, result.FilePath)
			msg.Caption = result.Description
			msg.ReplyToMessageID = result.MessageID

			_, err = bot.Send(msg)
			if err != nil {
				log.Printf("Failed to send video: %v", err)
			}
		}
	}()

	for update := range updates {
		if update.Message == nil {
			continue
		}

		link := update.Message.Text
		chatID := update.Message.Chat.ID
		messageID := update.Message.MessageID

		task := DownloadTask{
			URL:       link,
			ChatID:    chatID,
			MessageID: messageID,
		}

		body, err := json.Marshal(task)
		if err != nil {
			log.Printf("Failed to marshal task: %v", err)
			continue
		}

		err = ch.Publish(
			"",
			downloadQueue.Name,
			false,
			false,
			amqp091.Publishing{
				ContentType: "application/json",
				Body:        body,
			})
		if err != nil {
			log.Printf("Failed to publish task: %v", err)
			continue
		}
	}
}
