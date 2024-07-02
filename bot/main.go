package main

import (
	"log"
	"os"

	"github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/rabbitmq/amqp091-go"
)

var rabbitMQUrl = os.Getenv("RABBITMQ_URL")

func main() {
	bot, err := tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates, _ := bot.GetUpdatesChan(u)

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

	for update := range updates {
		if update.Message == nil {
			continue
		}

		link := update.Message.Text

		headers := amqp091.Table{
			"chat_id":    update.Message.Chat.ID,
			"user_id":    update.Message.From.ID,
			"username":   update.Message.From.UserName,
			"first_name": update.Message.From.FirstName,
			"last_name":  update.Message.From.LastName,
		}

		err = ch.Publish(
			"",
			q.Name,
			false,
			false,
			amqp091.Publishing{
				ContentType: "text/plain",
				Body:        []byte(link),
				Headers:     headers,
			})
		if err != nil {
			log.Fatal(err)
		}
	}
}
