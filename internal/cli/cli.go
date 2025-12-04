package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"tix-scraper/internal/services"
)

type BotConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	User        User   `json:"user"`
	SID         string `json:"sid"`
	EventID     string `json:"event_id"`
	TicketID    string `json:"ticket_id"`
	Filter      string `json:"filter"`
	Quantity    string `json:"quantity"`
	MaxTickets  string `json:"max_tickets"`
	PreSaleCode string `json:"pre_sale_code"`
	Loop        bool   `json:"loop"`
	Schedule    bool   `json:"schedule"`
	StartDate   string `json:"start_date"` // Format: "2006-01-02"
	StartTime   string `json:"start_time"` // Format: "15:04"
}

type User struct {
	SID      string `json:"sid"`
	Username string `json:"username"`
}

// Run reads the bots_config.json file and runs all configured bots
func Run() error {
	// Read the configuration file
	data, err := os.ReadFile("bots_config.json")
	if err != nil {
		return fmt.Errorf("failed to read bots_config.json: %w", err)
	}

	// Parse the configuration
	var configs []BotConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("failed to parse bots_config.json: %w", err)
	}

	if len(configs) == 0 {
		return fmt.Errorf("no bots configured in bots_config.json")
	}

	log.Printf("Found %d bot(s) in configuration\n", len(configs))

	// Run each bot in a separate goroutine
	ctx := context.Background()
	for i, config := range configs {
		botNum := i + 1
		go runBot(ctx, config, botNum)
	}

	// Keep the program running
	select {}
}

// runBot executes a single bot configuration
func runBot(ctx context.Context, config BotConfig, botNum int) {
	log.Printf("[Bot %d - %s] Initializing...\n", botNum, config.Name)

	// Validate configuration
	if config.SID == "" {
		log.Printf("[Bot %d - %s] ❌ Error: No SID configured\n", botNum, config.Name)
		return
	}

	if config.EventID == "" {
		log.Printf("[Bot %d - %s] ❌ Error: No Event ID configured\n", botNum, config.Name)
		return
	}

	// Handle scheduling if enabled
	if config.Schedule {
		if err := waitForScheduledTime(ctx, config.StartDate, config.StartTime, config.Name, botNum); err != nil {
			log.Printf("[Bot %d - %s] ❌ Schedule error: %v\n", botNum, config.Name, err)
			return
		}
	}

	// Create scraper configuration
	scraperCfg := services.ScraperConfig{
		BaseURL:        "https://tixcraft.com/ticket/area",
		EventID:        config.EventID,
		TicketID:       config.TicketID,
		Filter:         config.Filter,
		PerOrderTicket: config.Quantity,
		MaxTickets:     config.MaxTickets,
		PreSaleCode:    config.PreSaleCode,
		SessionID:      config.SID,
		Loop:           config.Loop,
	}

	// Start the scraper
	log.Printf("[Bot %d - %s] 🚀 Starting scraper...\n", botNum, config.Name)
	services.RunScraper(ctx, scraperCfg)
	log.Printf("[Bot %d - %s] 🛑 Scraper stopped\n", botNum, config.Name)
}

// waitForScheduledTime waits until the scheduled datetime
func waitForScheduledTime(ctx context.Context, startDate, startTime, botName string, botNum int) error {
	// Parse the scheduled datetime in local time
	scheduled, err := time.ParseInLocation("2006-01-02 15:04", fmt.Sprintf("%s %s", startDate, startTime), time.Local)
	if err != nil {
		return fmt.Errorf("invalid datetime format: %s %s (use YYYY-MM-DD and HH:MM format)", startDate, startTime)
	}

	now := time.Now()

	// If scheduled time is in the past, start immediately
	if scheduled.Before(now) {
		log.Printf("[Bot %d - %s] ⏰ Scheduled time %s has passed, starting immediately\n",
			botNum, botName, scheduled.Format("2006-01-02 15:04"))
		return nil
	}

	// Calculate wait duration
	waitDuration := scheduled.Sub(now)
	log.Printf("[Bot %d - %s] ⏰ Scheduled for %s (Local Time), waiting %v\n",
		botNum, botName, scheduled.Format("2006-01-02 15:04:05"), waitDuration)

	// Create a timer that respects context cancellation
	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	select {
	case <-timer.C:
		log.Printf("[Bot %d - %s] ✅ Scheduled time reached, starting...\n", botNum, botName)
		return nil
	case <-ctx.Done():
		log.Printf("[Bot %d - %s] 🛑 Schedule cancelled\n", botNum, botName)
		return fmt.Errorf("schedule cancelled")
	}
}

// RunSingle runs a single bot by its index in the configuration file
func RunSingle(botIndex int) error {
	// Read the configuration file
	data, err := os.ReadFile("bots_config.json")
	if err != nil {
		return fmt.Errorf("failed to read bots_config.json: %w", err)
	}

	// Parse the configuration
	var configs []BotConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return fmt.Errorf("failed to parse bots_config.json: %w", err)
	}

	if botIndex < 0 || botIndex >= len(configs) {
		return fmt.Errorf("invalid bot index %d (available: 0-%d)", botIndex, len(configs)-1)
	}

	// Run the specified bot
	ctx := context.Background()
	runBot(ctx, configs[botIndex], botIndex+1)

	return nil
}
