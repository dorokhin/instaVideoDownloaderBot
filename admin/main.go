package main

import (
	"database/sql"
	"log"
	"time"

	"github.com/gofiber/fiber/v2"
	_ "github.com/mattn/go-sqlite3"
)

var databaseFile = "/app/data/videos.db"

func initDB() (*sql.DB, error) {
	db, err := sql.Open("sqlite3", databaseFile)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func getProcessedURLs(db *sql.DB) ([]fiber.Map, error) {
	rows, err := db.Query("SELECT url, timestamp FROM processed_urls")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var urls []fiber.Map
	for rows.Next() {
		var url string
		var timestamp time.Time
		err = rows.Scan(&url, &timestamp)
		if err != nil {
			return nil, err
		}
		urls = append(urls, fiber.Map{
			"url":       url,
			"timestamp": timestamp,
		})
	}

	return urls, nil
}

func getStatistics(db *sql.DB, duration time.Duration) (int, error) {
	var count int
	timeLimit := time.Now().Add(-duration)
	query := `SELECT COUNT(*) FROM processed_urls WHERE timestamp > ?`
	err := db.QueryRow(query, timeLimit).Scan(&count)
	return count, err
}

func getUsers(db *sql.DB) ([]fiber.Map, error) {
	rows, err := db.Query("SELECT user_id, username, first_name, last_name FROM users")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []fiber.Map
	for rows.Next() {
		var userID int
		var username, firstName, lastName string
		err = rows.Scan(&userID, &username, &firstName, &lastName)
		if err != nil {
			return nil, err
		}
		users = append(users, fiber.Map{
			"user_id":    userID,
			"username":   username,
			"first_name": firstName,
			"last_name":  lastName,
		})
	}

	return users, nil
}

func getUserDownloads(db *sql.DB, userID string) ([]fiber.Map, error) {
	rows, err := db.Query("SELECT url, timestamp FROM downloads WHERE user_id = ?", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var downloads []fiber.Map
	for rows.Next() {
		var url string
		var timestamp time.Time
		err = rows.Scan(&url, &timestamp)
		if err != nil {
			return nil, err
		}
		downloads = append(downloads, fiber.Map{
			"url":       url,
			"timestamp": timestamp,
		})
	}

	return downloads, nil
}

func main() {
	db, err := initDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	app := fiber.New()

	app.Get("/processed_urls", func(c *fiber.Ctx) error {
		urls, err := getProcessedURLs(db)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.JSON(urls)
	})

	app.Get("/statistics", func(c *fiber.Ctx) error {
		stats := make(map[string]int)

		durations := map[string]time.Duration{
			"last_hour":  time.Hour,
			"last_24h":   24 * time.Hour,
			"last_week":  7 * 24 * time.Hour,
			"last_month": 30 * 24 * time.Hour,
		}

		for k, d := range durations {
			count, err := getStatistics(db, d)
			if err != nil {
				return c.Status(500).SendString(err.Error())
			}
			stats[k] = count
		}

		return c.JSON(stats)
	})

	app.Get("/users", func(c *fiber.Ctx) error {
		users, err := getUsers(db)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.JSON(users)
	})

	app.Get("/user_downloads/:id", func(c *fiber.Ctx) error {
		userID := c.Params("id")
		downloads, err := getUserDownloads(db, userID)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.JSON(downloads)
	})

	log.Fatal(app.Listen(":8080"))
}
