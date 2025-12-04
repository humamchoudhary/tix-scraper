package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"tix-scraper/internal/cli"
)

func main() {
	botIndex := flag.Int("bot", -1, "Run specific bot by index (-1 for all bots)")
	help := flag.Bool("help", false, "Show help message")

	flag.Parse()

	if *help {
		printHelp()
		os.Exit(0)
	}

	log.Println("⚡ Starting CLI mode...")
	if *botIndex >= 0 {
		log.Printf("Running bot #%d\n", *botIndex)
		if err := cli.RunSingle(*botIndex); err != nil {
			log.Fatal(err)
		}
	} else {
		log.Println("Running all bots from config...")
		if err := cli.Run(); err != nil {
			log.Fatal(err)
		}
	}
}

func printHelp() {
	fmt.Println(`
Tix Scraper CLI - Multi-Bot Runner
===================================

Usage:
  tix-scraper-cli [options]

Options:
  -bot int
        Run specific bot by index (-1 for all bots)
  
  -help
        Show this help message

Examples:
  # Run all bots
  go run cmd/tix-scraper-cli/main.go
  
  # Run specific bot
  go run cmd/tix-scraper-cli/main.go -bot=0
`)
}
