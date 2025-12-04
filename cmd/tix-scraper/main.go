package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"tix-scraper/internal/cli"
	"tix-scraper/internal/gui"
)

func main() {
	// Define command-line flags
	mode := flag.String("mode", "gui", "Run mode: 'gui' or 'cli'")
	botIndex := flag.Int("bot", -1, "Run specific bot by index (CLI mode only, -1 for all bots)")
	help := flag.Bool("help", false, "Show help message")

	flag.Parse()

	// Show help
	if *help {
		printHelp()
		os.Exit(0)
	}

	// Run based on mode
	switch *mode {
	case "gui":
		log.Println("🎨 Starting GUI mode...")
		gui.NewGUI().Run()

	case "cli":
		log.Println("⚡ Starting CLI mode...")
		if *botIndex >= 0 {
			// Run single bot
			log.Printf("Running bot #%d\n", *botIndex)
			if err := cli.RunSingle(*botIndex); err != nil {
				log.Fatal(err)
			}
		} else {
			// Run all bots
			log.Println("Running all bots from config...")
			if err := cli.Run(); err != nil {
				log.Fatal(err)
			}
		}

	default:
		log.Fatalf("Invalid mode: %s (use 'gui' or 'cli')", *mode)
	}
}

func printHelp() {
	fmt.Println(`
Tix Scraper - Multi-Bot Ticket Scraper
========================================

Usage:
  tix-scraper [options]

Options:
  -mode string
        Run mode: 'gui' or 'cli' (default "gui")
  
  -bot int
        Run specific bot by index in CLI mode
        Use -1 to run all bots (default -1)
  
  -help
        Show this help message

Examples:
  # Start GUI (default)
  ./tix-scraper
  
  # Start GUI explicitly
  ./tix-scraper -mode=gui
  
  # Run all bots from config in CLI mode
  ./tix-scraper -mode=cli
  
  # Run specific bot (first bot = index 0)
  ./tix-scraper -mode=cli -bot=0
  
  # Run second bot
  ./tix-scraper -mode=cli -bot=1
`)
}
