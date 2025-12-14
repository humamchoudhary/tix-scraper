package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/joho/godotenv"
)

func init() {
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found or error loading it")
	}
}

type ScraperConfig struct {
	BaseURL        string
	EventID        string
	TicketID       string
	Filter         string
	PerOrderTicket string
	MaxTickets     string
	SessionID      string
	Loop           bool
	PreSaleCode    string
}

type Booking struct {
	SessionID    string `json:"session_id"`
	Seat         string `json:"seat"`
	EventID      string `json:"event_id"`
	TicketID     string `json:"ticket_id"`
	NumOfTickets string `json:"num_of_tickets"`
	OrderNumber  string `json:"order_number"`
	EventName    string `json:"event_name"`
	EventDate    string `json:"event_date"`
	EventVenue   string `json:"event_venue"`
	Section      string `json:"section"`
	SeatInfo     string `json:"seat_info"`
	TicketInfo   string `json:"ticket_info"`
	TicketQty    string `json:"ticket_qty"`
	ServiceFee   string `json:"service_fee"`
	Total        string `json:"total"`
	UserName     string `json:"username"`
}

// Global logger for file logging
var (
	fileLogger   *log.Logger
	logFile      *os.File
	logMutex     sync.Mutex
	guiLogWriter io.Writer
)
var fileMutex sync.Mutex

func SetGUIWriter(writer io.Writer) {
	guiLogWriter = writer
}

// Initialize file logger
func initFileLogger() error {
	logMutex.Lock()
	defer logMutex.Unlock()

	if logFile != nil {
		logFile.Close()
	}

	if err := os.MkdirAll("logs", 0755); err != nil {
		return fmt.Errorf("failed to create logs directory: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	filename := fmt.Sprintf("logs/scraper_%s.log", timestamp)

	var err error
	logFile, err = os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	fileLogger = log.New(multiWriter, "", log.LstdFlags|log.Lshortfile)

	return nil
}

func LogToFile(format string, v ...interface{}) {
	message := fmt.Sprintf(format, v...)

	// Write to file/terminal
	if fileLogger == nil {
		if err := initFileLogger(); err != nil {
			log.Printf("Failed to initialize file logger: %v", err)
			log.Printf("%s", message)
		} else {
			fileLogger.Printf("%s", message)
		}
	} else {
		fileLogger.Printf("%s", message)
	}

	// Write to GUI if available
	if guiLogWriter != nil {
		// Add timestamp for GUI logs
		timestamp := time.Now().Format("15:04:05")
		guiMessage := fmt.Sprintf("%s %s\n", timestamp, message)
		guiLogWriter.Write([]byte(guiMessage))
	}
}

// Global browser context for reuse

func getBrowserContext(parentCtx context.Context) (context.Context, context.CancelFunc) {
	opts := append(chromedp.DefaultExecAllocatorOptions[:],

		// CRITICAL: Must be true for CLI/server environment
		// chromedp.Flag("headless", false), // Changed from false - required for CLI

		// Anti-detection

		// Keep your user agent
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)

	// Initialize popup handler
	setupPopupHandler(browserCtx)

	// Initialize file logger
	initFileLogger()

	go func() {
		<-parentCtx.Done()
		LogToFile("🛑 Browser context cancelled by parent")
		browserCancel()
		allocCancel()
	}()

	return browserCtx, func() {
		LogToFile("🔴 Closing browser instance")
		browserCancel()
		allocCancel()
	}
}

// Setup global popup handler
func setupPopupHandler(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			LogToFile("⚠️ JavaScript popup detected: %s", e.Message)
			// Auto-dismiss the dialog
			go func() {
				_ = chromedp.Run(ctx, page.HandleJavaScriptDialog(true))
				LogToFile("✅ Popup dismissed automatically")
			}()
		}
	})
}

// Main runner function with URL-based routing
// Main runner function with URL-based routing
func runMainFlow(ctx context.Context, cfg *ScraperConfig, isFirstIteration bool) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		var currentURL string
		err := chromedp.Run(ctx, chromedp.Location(&currentURL))
		if err != nil {
			LogToFile("❌ Failed to get current URL: %v", err)
			return false
		}

		LogToFile("🌐 Current URL: %s", currentURL)

		// Route based on URL pattern
		switch {
		case strings.Contains(currentURL, "https://tixcraft.com/activity/game/"):
			LogToFile("🔄 Running monitorEventPage")
			if err := monitorEventPage(ctx, *cfg); err != nil {
				LogToFile("❌ Error in monitorEventPage: %v", err)
				return false
			}
			// Continue to next URL check

		case strings.Contains(currentURL, "https://tixcraft.com/ticket/area/"):
			LogToFile("🔄 Running executeBookingFlow")
			success := executeBookingFlow(ctx, *cfg, isFirstIteration)
			if !success {
				return false
			}
			// After executeBookingFlow succeeds, wait for next URL
			// It should redirect to ticket/ticket/ or ticket/order
			time.Sleep(2 * time.Second)
			continue

		case strings.Contains(currentURL, "https://tixcraft.com/ticket/ticket/"):
			LogToFile("🔄 Running ticket processing flow")
			success := processTicketPage(ctx, cfg)
			if !success {
				return false
			}
			// After processTicketPage succeeds, wait for redirect
			time.Sleep(2 * time.Second)
			continue

		case strings.Contains(currentURL, "https://tixcraft.com/ticket/verify"):
			LogToFile("🔑 Handling pre-sale code verification")
			// ... pre-sale code handling ...
			continue

		case strings.Contains(currentURL, "https://tixcraft.com/ticket/order"):
			LogToFile("🔍 On order page, waiting for redirect...")
			// Just wait for redirect to checkout
			err := chromedp.Run(ctx,
				chromedp.Sleep(2*time.Second),
			)
			if err != nil {
				LogToFile("❌ Error on order page: %v", err)
			}
			continue

		case strings.Contains(currentURL, "https://tixcraft.com/ticket/checkout"):
			LogToFile("💰 Reached checkout page - resetting browser state")

			// Simply reset browser state and return success
			resetBrowserState(ctx, *cfg)

			// Return true to indicate this iteration completed successfully
			return true

		default:
			LogToFile("❓ Unknown URL pattern, waiting...")
			time.Sleep(2 * time.Second)
		}
	}
}

func RunScraper(ctx context.Context, cfg ScraperConfig) {
	if err := initFileLogger(); err != nil {
		log.Printf("❌ Failed to initialize file logger: %v", err)
	} else {
		defer logFile.Close()
	}

	LogToFile("🚀 Starting scraper with config: EventID=%s, TicketID=%s, Filter=%s",
		cfg.EventID, cfg.TicketID, cfg.Filter)

	// Create NEW browser context for each session
	browserCtx, browserCancel := getBrowserContext(ctx)
	defer browserCancel()

	// Determine Loop Count
	numLoops := 1
	quantity, err := strconv.Atoi(cfg.PerOrderTicket)
	if err != nil {
		LogToFile("❌ Invalid quantity format: %s", cfg.PerOrderTicket)
		return
	}

	maxTickets, err := strconv.Atoi(cfg.MaxTickets)
	if err != nil {
		LogToFile("❌ Invalid max tickets format: %s", cfg.MaxTickets)
		return
	}

	if cfg.Loop && quantity > 0 && maxTickets > 0 {
		numLoops = (maxTickets + quantity - 1) / quantity
		LogToFile("🔄 Loop mode: Running %d iterations", numLoops)
	}

	// Initial navigation
	initialURL := buildInitialURL(cfg)
	LogToFile("🌐 Navigating to initial URL: %s", initialURL)

	err = chromedp.Run(browserCtx,
		setupCookies(&cfg),
		chromedp.Navigate(initialURL),
		chromedp.Sleep(2*time.Second),
	)
	if err != nil {
		LogToFile("❌ Initial navigation failed: %v", err)
		return
	}

	// Dismiss cookie banner on first iteration
	// dismissCookieBanner(browserCtx)

	// Execute main flow for each iteration
	for i := 1; i <= numLoops; {
		select {
		case <-ctx.Done():
			LogToFile("⏹️ Scraper stopped by user.")
			return
		default:
		}

		LogToFile("=== Iteration %d/%d ===", i, numLoops)

		success := runMainFlow(browserCtx, &cfg, i == 1)

		if success {
			i++
			if i <= numLoops {
				LogToFile("✅ Success!")
				// time.Sleep(3 * time.Second)
				// resetBrowserState(browserCtx, cfg)
			}
		} else {
			LogToFile("❌ Iteration failed. Retrying in 2 seconds...")
			time.Sleep(2 * time.Second)
			resetBrowserState(browserCtx, cfg)
		}
	}
	LogToFile("🎉 All iterations complete.")
}

func resetBrowserState(ctx context.Context, cfg ScraperConfig) {
	// Try to navigate back to a safe starting point
	baseURL := "https://tixcraft.com"
	if cfg.EventID != "" && cfg.TicketID != "" {
		baseURL = fmt.Sprintf("https://tixcraft.com/ticket/area/%s/%s", cfg.EventID, cfg.TicketID)
	} else if cfg.EventID != "" {
		baseURL = fmt.Sprintf("https://tixcraft.com/activity/game/%s", cfg.EventID)
	}

	LogToFile("🔄 Resetting browser to: %s", baseURL)

	_ = chromedp.Run(ctx,
		chromedp.Navigate(baseURL),
		chromedp.Sleep(2*time.Second),
	)
}

func buildInitialURL(cfg ScraperConfig) string {
	if cfg.TicketID != "" {
		return fmt.Sprintf("https://tixcraft.com/ticket/area/%s/%s", cfg.EventID, cfg.TicketID)
	}
	return fmt.Sprintf("https://tixcraft.com/activity/game/%s", cfg.EventID)
}

func setupCookies(cfg *ScraperConfig) chromedp.Action {
	optanonConsentValue := "isGpcEnabled=0&datestamp=" + strings.ReplaceAll(time.Now().Format("Mon+Jan+02+2006+15:04:05+GMT-0700"), ":", "%3A") +
		"&version=202506.1.0&browserGpcFlag=0&isIABGlobal=false&hosts=&consentId=" + generateUUID() +
		"&interactionCount=1&isAnonUser=0&landingPath=NotLandingPage&groups=C0001%3A1%2CC0003%3A1%2CC0002%3A1%2CC0004%3A1&AwaitingReconsent=false"

	return chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.Run(ctx,
			network.SetCookie("SID", cfg.SessionID).WithDomain("tixcraft.com").WithPath("/"),
			network.SetCookie("TIXUISID", cfg.SessionID).WithDomain("tixcraft.com").WithPath("/"),
			network.SetCookie("OptanonConsent", optanonConsentValue).
				WithDomain(".tixcraft.com").WithPath("/").
				WithExpires(timeToCDPTime(time.Now().Add(365*24*time.Hour))),
			network.SetCookie("OptanonAlertBoxClosed", time.Now().Format("2006-01-02T15:04:05.000Z")).
				WithDomain(".tixcraft.com").WithPath("/").
				WithExpires(timeToCDPTime(time.Now().Add(365*24*time.Hour))),
		)
	})
}

// Process ticket page with captcha retries
func processTicketPage(ctx context.Context, cfg *ScraperConfig) bool {
	maxCaptchaRetries := 8
	for j := 0; j < maxCaptchaRetries; j++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		captchaText, err := fastProcessCaptcha(ctx)
		if err != nil {
			LogToFile("❌ Captcha error: %v", err)
			fastReloadPage(ctx)
			continue
		}

		LogToFile("🔐 Attempting Captcha: %s", captchaText)

		var currentURL, newURL string
		var errorMessage string

		err = chromedp.Run(ctx,
			chromedp.Location(&currentURL),
			chromedp.SetValue("#ticketPriceList select", cfg.PerOrderTicket, chromedp.ByQuery),
			chromedp.SetValue("#TicketForm_verifyCode", captchaText, chromedp.ByQuery),
			chromedp.SetAttributeValue("#TicketForm_agree", "checked", "true", chromedp.ByQuery),
			chromedp.Click("button[type='submit']", chromedp.ByQuery),
			chromedp.Sleep(2000*time.Millisecond),
			chromedp.Evaluate(`
				(function() {
					const errorSelectors = [
						'.alert-danger',
						'.error-message', 
						'.text-danger',
						'#error-message',
						'.verifyCode-error',
						'[class*="error"]',
						'[class*="invalid"]'
					];
					
					for (const selector of errorSelectors) {
						const element = document.querySelector(selector);
						if (element && element.textContent.trim()) {
							return element.textContent.trim();
						}
					}
					
					const captchaError = document.querySelector('#TicketForm_verifyCode-error');
					if (captchaError && captchaError.textContent.trim()) {
						return captchaError.textContent.trim();
					}
					
					return "";
				})()
			`, &errorMessage),
			chromedp.Location(&newURL),
		)

		if err != nil {
			LogToFile("❌ Submission error: %v", err)
			fastReloadPage(ctx)
			continue
		}

		if errorMessage != "" {
			LogToFile("❌ Submission failed: %s", errorMessage)
			fastReloadPage(ctx)
			continue
		}

		if newURL != currentURL {
			LogToFile("🎉 Reservation Successful!")
			return true
		}

		LogToFile("🔁 No URL change or success indicator, retrying captcha... (%d/%d)", j+1, maxCaptchaRetries)
		fastReloadPage(ctx)
	}

	return false
}

// Optimized checkout extraction with better error handling
// func fastCheckoutExtract(ctx context.Context) error {
//
// 	// Wait for page to load and try multiple times if needed
// 	// err := chromedp.Run(ctx,
// 	// 	chromedp.WaitReady("body", chromedp.ByQuery),
// 	// 	// chromedp.Sleep(1*time.Second),
// 	// 	chromedp.WaitVisible("#reSelect", chromedp.ByID),
// 	// 	chromedp.Click("#reSelect", chromedp.ByID),
// 	// chromedp.Evaluate(`(function() {
// 	// 	try {
// 	// 		const getText = (sel) => {
// 	// 			const el = document.querySelector(sel);
// 	// 			return el ? el.innerText.trim() : "";
// 	// 		};
// 	//
// 	// 		const getCell = (rowIdx, colIdx) => {
// 	// 			const row = document.querySelectorAll('tr.orderTicket')[rowIdx];
// 	// 			if(!row) return "";
// 	// 			const cells = row.querySelectorAll('td');
// 	// 			return cells[colIdx] ? cells[colIdx].innerText.trim() : "";
// 	// 		};
// 	//
// 	// 		const data = {
// 	// 			order_number: getText('.hex_Order_number'),
// 	// 			event_name: getText('.ticketEventName'),
// 	// 			event_date: getText('.ticketEventDate')?.replace(/\s+/g, ' ') || "",
// 	// 			event_venue: getText('.ticketEventVenue')?.replace(/\s+/g, ' ') || "",
// 	// 			ticket_qty: getText('.text-primary.bold'),
// 	// 			service_fee: getText('#orderFee'),
// 	// 			total: getText('#orderAmount'),
// 	// 			section: getCell(0, 1),
// 	// 			seat_info: getCell(0, 2),
// 	// 			ticket_info: getCell(0, 3)
// 	// 		};
// 	//
// 	// 		// Validate we have at least order number
// 	// 		if (!data.order_number) {
// 	// 			return "incomplete_data";
// 	// 		}
// 	//
// 	// 		return JSON.stringify(data);
// 	// 	} catch (error) {
// 	// 		return "error: " + error.message;
// 	// 	}
// 	// })()`, &jsonStr),
// 	// )
//
// 	// if jsonStr == "incomplete_data" {
// 	// 	LogToFile("🔁 Checkout data not fully loaded, retrying... (%d/5)", i+1)
// 	// 	time.Sleep(1 * time.Second)
// 	// 	continue
// 	// }
//
// 	// if strings.HasPrefix(jsonStr, "error:") {
// 	// 	return nil, fmt.Errorf("javascript error in checkout extraction: %s", jsonStr)
// 	// }
//
// 	// var b Booking
// 	// if err := json.Unmarshal([]byte(jsonStr), &b); err != nil {
// 	// 	return nil, fmt.Errorf("failed to unmarshal booking data: %w, raw JSON: %s", err, jsonStr)
// 	// }
// 	//
// 	// if b.OrderNumber == "" {
// 	// 	return nil, fmt.Errorf("incomplete booking data: missing order number")
// 	// }
//
// 	// Try to click continue/reselect button
// 	err := chromedp.Run(ctx,
//
// 		chromedp.WaitVisible("#reSelect", chromedp.ByID),
// 		chromedp.Click("#reSelect", chromedp.ByID),
// 		chromedp.Sleep(500*time.Millisecond),
// 		chromedp.Click("#reSelect", chromedp.ByID), // Double click for reliability
// 	)
//
// 	if err != nil {
// 		return fmt.Errorf("failed to extract checkout data: %w", err)
// 	}
//
// 	return nil
// }

// Optimized checkout extraction with better error handling
func fastCheckoutExtract(ctx context.Context, cfg ScraperConfig) error {
	LogToFile("💰 Running checkout extraction")

	var reselectFound bool

	// Check if reselect button exists with a timeout
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := chromedp.Run(checkCtx,
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(`
            (function() {
                const reselectBtn = document.getElementById('reSelect');
                return reselectBtn !== null && reselectBtn.offsetParent !== null;
            })()
        `, &reselectFound),
	)

	if err != nil || !reselectFound {
		LogToFile("❌ Reselect button not found or page not loaded")

		// Reset browser state
		LogToFile("🔄 Resetting browser state...")
		resetBrowserState(ctx, cfg)

		return fmt.Errorf("reselect button not found")
	}

	// Try to click continue/reselect button with retries
	maxRetries := 2
	for attempt := 1; attempt <= maxRetries; attempt++ {
		LogToFile("🖱️ Attempting to click reselect button (%d/%d)", attempt, maxRetries)

		err := chromedp.Run(ctx,
			chromedp.WaitVisible("#reSelect", chromedp.ByID),
			chromedp.Click("#reSelect", chromedp.ByID),
			chromedp.Sleep(500*time.Millisecond),
		)

		if err != nil {
			LogToFile("❌ Failed to click reselect button: %v", err)

			if attempt == maxRetries {
				LogToFile("🔄 Maximum retries reached, resetting browser state...")
				resetBrowserState(ctx, cfg)
				return fmt.Errorf("failed to click reselect button after %d attempts", maxRetries)
			}

			continue
		}

		// Double click for reliability (second click)
		err = chromedp.Run(ctx,
			chromedp.Click("#reSelect", chromedp.ByID),
			chromedp.Sleep(500*time.Millisecond),
		)

		if err != nil {
			LogToFile("⚠️ Second click failed but first succeeded: %v", err)
			// Continue anyway since first click might have worked
		}

		LogToFile("✅ Reselect button clicked successfully")
		return nil
	}

	// Should not reach here, but just in case
	LogToFile("🔄 Unexpected state, resetting browser...")
	resetBrowserState(ctx, cfg)
	return fmt.Errorf("unexpected state in checkout extraction")
}

// Safe file operations with retry mechanism

func safeSaveBooking(booking Booking) {
	fileMutex.Lock()
	defer fileMutex.Unlock()

	// Retry mechanism for file operations
	for attempt := 1; attempt <= 3; attempt++ {
		if err := os.MkdirAll("data", 0755); err != nil {
			LogToFile("❌ Attempt %d: Failed to create data directory: %v", attempt, err)
			time.Sleep(1 * time.Second)
			continue
		}

		var bookings []Booking
		filename := "data/bookings.json"

		// Read existing data
		data, err := os.ReadFile(filename)
		if err != nil && !os.IsNotExist(err) {
			LogToFile("❌ Attempt %d: Error reading bookings file: %v", attempt, err)
			time.Sleep(1 * time.Second)
			continue
		}

		if len(data) > 0 {
			if err := json.Unmarshal(data, &bookings); err != nil {
				LogToFile("❌ Attempt %d: Error unmarshaling existing bookings: %v", attempt, err)
				// Start fresh if file is corrupted
				bookings = []Booking{}
			}
		}

		// Append new booking
		bookings = append(bookings, booking)

		// Write with temporary file to prevent corruption
		tempFilename := filename + ".tmp"
		updatedData, err := json.MarshalIndent(bookings, "", "  ")
		if err != nil {
			LogToFile("❌ Attempt %d: Error marshaling bookings: %v", attempt, err)
			time.Sleep(1 * time.Second)
			continue
		}

		if err := os.WriteFile(tempFilename, updatedData, 0644); err != nil {
			LogToFile("❌ Attempt %d: Error writing temp file: %v", attempt, err)
			time.Sleep(1 * time.Second)
			continue
		}

		// Atomic rename
		if err := os.Rename(tempFilename, filename); err != nil {
			LogToFile("❌ Attempt %d: Error renaming temp file: %v", attempt, err)
			time.Sleep(1 * time.Second)
			continue
		}

		LogToFile("✅ Booking saved to file. Order: %s", booking.OrderNumber)
		return
	}

	LogToFile("❌ Failed to save booking after 3 attempts")
}

func dismissCookieBanner(ctx context.Context) {
	// Try to click any cookie acceptance button that appears
	// log.Println("First iteration: dismissing cookie banner if present")
	//
	// err := chromedp.Run(ctx,
	//
	// 	chromedp.Click("//button[text()='Accept All']", chromedp.BySearch))
	//
	// if err != nil {
	// 	log.Printf("Error dismissing cookies", err)
	// }
}

func monitorEventPage(ctx context.Context, cfg ScraperConfig) error {
	eventURL := fmt.Sprintf("https://tixcraft.com/activity/game/%s", cfg.EventID)
	LogToFile("🔍 Monitoring event page: %s", eventURL)

	retryCount := 0
	maxRetries := 100

	// Navigate to event page first
	err := chromedp.Run(ctx,
		network.SetCookie("SID", cfg.SessionID).WithDomain("tixcraft.com").WithPath("/"),
		network.SetCookie("TIXUISID", cfg.SessionID).WithDomain("tixcraft.com").WithPath("/"),
		chromedp.Navigate(eventURL),
	)
	if err != nil {
		LogToFile("❌ Initial navigation failed: %v", err)
		return err
	}

	for retryCount < maxRetries {
		select {
		case <-ctx.Done():
			LogToFile("🛑 Monitoring cancelled by context")
			return fmt.Errorf("monitoring cancelled")
		default:
		}

		var result string

		// Wait for page to load and check for tickets
		err := chromedp.Run(ctx,
			chromedp.WaitVisible("#gameList", chromedp.ByID),
			// chromedp.Sleep(1*time.Second), // Small delay for stability
			chromedp.Evaluate(`
				(function() {
					try {
						// Find clickable ticket buttons
						const buttons = document.querySelectorAll('button, a');
						for (let btn of buttons) {
							const text = btn.textContent;
							if ((text.includes('Find tickets') || text.includes('找座位')) && 
								!btn.disabled && btn.offsetParent !== null) {
								btn.click();
								return "clicked";
							}
						}
						return "no_tickets";
					} catch (error) {
						return "error";
					}
				})()
			`, &result),
		)

		if err != nil {
			LogToFile("❌ Error checking tickets: %v", err)
			retryCount++
			fastReloadPage(ctx)
			continue
		}

		LogToFile("🔍 Monitoring result: %s", result)

		if result == "clicked" {
			LogToFile("✅ Found available tickets! Proceeding to booking...")
			return nil
		}

		retryCount++
		LogToFile("🔁 No available tickets found (%d/%d), refreshing...", retryCount, maxRetries)
		fastReloadPage(ctx)
	}

	return fmt.Errorf("max retries reached while monitoring event page")
}

func timeToCDPTime(t time.Time) *cdp.TimeSinceEpoch {
	return (*cdp.TimeSinceEpoch)(&t)
}
func executeBookingFlow(ctx context.Context, cfg ScraperConfig, isFirstIteration bool) bool {
	// Build URL based on available data

	// var username string

	// Only navigate if we're not already on the correct page

	// Quick login check with timeout
	// loginCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	// defer cancel()
	//
	// err := chromedp.Run(loginCtx,
	// 	chromedp.WaitReady("body"),
	// 	chromedp.Text(".user-name", &username, chromedp.ByQuery),
	// )
	// if err != nil || strings.TrimSpace(username) == "" {
	// 	LogToFile("❌ Login failed or User not found. Update SID/TIXUISID.")
	// 	return false
	// }
	// LogToFile("✅ Logged in as: %s", username)

	LogToFile("🔍 Checking for pre-sale code requirement...")
	if err := handlePreSaleCode(ctx, cfg.PreSaleCode); err != nil {
		LogToFile("❌ Pre-sale code error: %v", err)
		return false
	}

	// Fast seat selection
	var seatVal string
	actions := []chromedp.Action{
		chromedp.WaitReady("#selectseat", chromedp.ByQuery),
	}

	actions = append(actions,
		chromedp.WaitVisible("#selectseat", chromedp.ByID),
		chromedp.EvaluateAsDevTools(fmt.Sprintf(`
        (function(){
            try {
                const filter = "%s";
                const links = document.querySelectorAll(".area-list a");
                
                // If no filter provided, click the first available seat
                if (!filter || filter.trim() === "") {
                    if (links.length > 0) {
                        links[0].click();
                        return links[0].innerText.trim();
                    }
                    return "no_seats_available";
                }
                
                // Split comma-separated filters
                const filters = filter.split(',').map(f => f.trim()).filter(f => f);
                
                // If multiple filters provided, try each one
                if (filters.length > 1) {
                    for (let filterText of filters) {
                        for (let a of links) {
                            if (a.innerText.includes(filterText)) {
                                a.click();
                                return a.innerText.trim();
                            }
                        }
                    }
                    return "no_matching_seat";
                }
                
                // Single filter (original behavior)
                const singleFilter = filters[0];
                for (let a of links) {
                    if (a.innerText.includes(singleFilter)) {
                        a.click();
                        return a.innerText.trim();
                    }
                }
                return "no_matching_seat";
            } catch (error) {
                return "error: " + error.message;
            }
        })()
    `, cfg.Filter), &seatVal),
	)
	err := chromedp.Run(ctx, actions...)
	if err != nil {
		LogToFile("❌ Error selecting seat: %v", err)
		return false
	}

	if seatVal == "no_matching_seat" {
		LogToFile("❌ No matching seat found for filter: %s", cfg.Filter)
		return false
	}
	if strings.HasPrefix(seatVal, "error:") {
		LogToFile("❌ JavaScript error selecting seat: %s", seatVal)
		return false
	}

	LogToFile("✅ Selected seat: %s", seatVal)

	// Fast captcha processing with retries
	// Fast captcha processing with retries
	maxCaptchaRetries := 8
	for j := 0; j < maxCaptchaRetries; j++ {
		select {
		case <-ctx.Done():
			return false
		default:
		}

		captchaText, err := fastProcessCaptcha(ctx)
		if err != nil {
			LogToFile("❌ Captcha error: %v", err)
			fastReloadPage(ctx)
			continue
		}

		LogToFile("🔐 Attempting Captcha: %s", captchaText)

		// Fast submission
		var currentURL, newURL string
		var hasError bool
		var errorMessage string

		// Listen for JavaScript alerts and errors
		chromedp.ListenTarget(ctx, func(ev interface{}) {
			switch e := ev.(type) {
			case *page.EventJavascriptDialogOpening:
				errorMessage = e.Message
				hasError = true
				// Auto-dismiss the dialog
				go func() {
					_ = chromedp.Run(ctx, page.HandleJavaScriptDialog(true))
				}()
			}
		})

		err = chromedp.Run(ctx,
			chromedp.Location(&currentURL),
			chromedp.SetValue("#ticketPriceList select", cfg.PerOrderTicket, chromedp.ByQuery),
			chromedp.SetValue("#TicketForm_verifyCode", captchaText, chromedp.ByQuery),
			// chromedp.Click("#TicketForm_agree", chromedp.ByQuery),

			chromedp.SetAttributeValue("#TicketForm_agree", "checked", "true", chromedp.ByQuery),
			chromedp.Click("button[type='submit']", chromedp.ByQuery),
			chromedp.Sleep(2000*time.Millisecond),
			// Check for error messages on the page
			chromedp.Evaluate(`
            (function() {
                // Check for common error elements
                const errorSelectors = [
                    '.alert-danger',
                    '.error-message', 
                    '.text-danger',
                    '#error-message',
                    '.verifyCode-error',
                    '[class*="error"]',
                    '[class*="invalid"]'
                ];
                
                for (const selector of errorSelectors) {
                    const element = document.querySelector(selector);
                    if (element && element.textContent.trim()) {
                        return element.textContent.trim();
                    }
                }
                
                // Check for specific captcha error
                const captchaError = document.querySelector('#TicketForm_verifyCode-error');
                if (captchaError && captchaError.textContent.trim()) {
                    return captchaError.textContent.trim();
                }
                
                return "";
            })()
        `, &errorMessage),
			chromedp.Location(&newURL),
		)

		if err != nil {
			LogToFile("❌ Submission error: %v", err)
			fastReloadPage(ctx)
			continue
		}

		// Check if we have any errors
		if hasError || errorMessage != "" {
			LogToFile("❌ Submission failed: %s", errorMessage)
			fastReloadPage(ctx)
			continue
		}

		if newURL != currentURL {
			LogToFile("🎉 Reservation Successful! 919")

			// _ = chromedp.Run(ctx,
			// 	// chromedp.Reload(),
			// 	chromedp.Sleep(1*time.Second),
			// )
			// Fast checkout extraction
			// err := fastCheckoutExtract(ctx, cfg)
			// if err != nil {
			// 	LogToFile("❌ Checkout error: %v", err)
			// 	// fastReloadPage(ctx)
			// 	continue
			// }
			// if err == nil && booking != nil {
			// 	booking.SessionID = cfg.SessionID
			// 	booking.Seat = seatVal
			// 	booking.EventID = cfg.EventID
			// 	booking.UserName = username
			// 	go saveBooking(*booking)
			// }

			return true
		}

		LogToFile("🔁 No URL change or success indicator, retrying captcha... (%d/%d)", j+1, maxCaptchaRetries)
		fastReloadPage(ctx)
	}

	return false
}

func fastReloadPage(ctx context.Context) {
	_ = chromedp.Run(ctx,
		chromedp.Reload(),
		chromedp.Sleep(1*time.Second),
	)
}

func fastProcessCaptcha(ctx context.Context) (string, error) {
	var base64Data string

	// Fast captcha image capture
	err := chromedp.Run(ctx,
		chromedp.WaitVisible("#TicketForm_verifyCode-image", chromedp.ByID),
		chromedp.Evaluate(`
            (function() {
                try {
                    const img = document.querySelector('#TicketForm_verifyCode-image');
                    if (!img.complete) return "image_not_loaded";
                    const canvas = document.createElement('canvas');
                    canvas.width = img.naturalWidth;
                    canvas.height = img.naturalHeight;
                    const ctx = canvas.getContext('2d');
                    ctx.drawImage(img, 0, 0);
                    return canvas.toDataURL('image/png');
                } catch (error) {
                    return "error: " + error.message;
                }
            })()
        `, &base64Data),
	)
	if err != nil || base64Data == "" {
		return "", fmt.Errorf("captcha image capture failed: %w", err)
	}

	if base64Data == "image_not_loaded" {
		return "", fmt.Errorf("captcha image not loaded yet")
	}
	if strings.HasPrefix(base64Data, "error:") {
		return "", fmt.Errorf("javascript error: %s", base64Data)
	}

	parts := strings.Split(base64Data, ",")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid base64 format")
	}

	imageBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("base64 decode failed: %w", err)
	}

	// Fast OCR request
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	apiKey := os.Getenv("OCR_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("missing OCR_API_KEY")
	}

	writer.WriteField("apikey", apiKey)
	writer.WriteField("language", "eng")
	writer.WriteField("OCREngine", "2")

	part, err := writer.CreateFormFile("file", "captcha.png")
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(imageBytes); err != nil {
		return "", fmt.Errorf("failed to write image data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.ocr.space/parse/image", body)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("OCR request failed: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode OCR response: %w", err)
	}

	// Better error handling for OCR response
	if parsed, ok := result["ParsedResults"].([]interface{}); ok && len(parsed) > 0 {
		if data, ok := parsed[0].(map[string]interface{}); ok {
			if text, ok := data["ParsedText"].(string); ok {
				cleanedText := strings.ToLower(strings.TrimSpace(text))
				if cleanedText == "" {
					return "", fmt.Errorf("OCR returned empty text")
				}
				return cleanedText, nil
			}
		}
	}

	// Check for OCR error messages
	if errMsg, ok := result["ErrorMessage"].([]interface{}); ok && len(errMsg) > 0 {
		return "", fmt.Errorf("OCR API error: %v", errMsg)
	}

	return "", fmt.Errorf("no text found in OCR response")
}

func saveBooking(booking Booking) {
	fileMutex.Lock()
	defer fileMutex.Unlock()

	// Create bookings directory if it doesn't exist
	if err := os.MkdirAll("data", 0755); err != nil {
		LogToFile("❌ Failed to create data directory: %v", err)
		return
	}

	var bookings []Booking
	data, err := os.ReadFile("data/bookings.json")
	if err != nil && !os.IsNotExist(err) {
		LogToFile("❌ Error reading bookings file: %v", err)
		return
	}

	if len(data) > 0 {
		if err := json.Unmarshal(data, &bookings); err != nil {
			LogToFile("❌ Error unmarshaling existing bookings: %v", err)
			// Continue with empty bookings array
			bookings = []Booking{}
		}
	}

	bookings = append(bookings, booking)
	updatedData, err := json.MarshalIndent(bookings, "", "  ")
	if err != nil {
		LogToFile("❌ Error marshaling bookings: %v", err)
		return
	}

	if err := os.WriteFile("data/bookings.json", updatedData, 0644); err != nil {
		LogToFile("❌ Error writing bookings file: %v", err)
		return
	}

	LogToFile("✅ Booking saved to file. Order: %s", booking.OrderNumber)
}

func handlePreSaleCode(ctx context.Context, preSaleCode string) error {
	LogToFile("🔍 Checking for pre-sale code form...")

	// Wait a bit for page to load
	time.Sleep(1 * time.Second)

	var hasForm bool
	err := chromedp.Run(ctx,
		chromedp.Evaluate(`
            (function() {
                const form = document.getElementById('form-ticket-verify');
                return form !== null && form.offsetParent !== null;
            })()
        `, &hasForm),
	)
	if err != nil {
		return fmt.Errorf("failed to check for pre-sale form: %w", err)
	}

	if !hasForm {
		LogToFile("✅ No pre-sale code form found, continuing...")
		return nil
	}

	LogToFile("🔑 Pre-sale code form detected, entering code...")

	if preSaleCode == "" {
		return fmt.Errorf("pre-sale code form found but no code provided")
	}

	// Fill and submit the pre-sale code form
	err = chromedp.Run(ctx,
		chromedp.WaitVisible("#form-ticket-verify", chromedp.ByID),
		chromedp.SetValue("input[name='checkCode']", preSaleCode, chromedp.ByQuery),
		chromedp.Click("#form-ticket-verify button[type='submit']", chromedp.ByQuery),
		chromedp.Sleep(1*time.Second), // Wait for form submission
	)
	if err != nil {
		return fmt.Errorf("failed to submit pre-sale code: %w", err)
	}

	LogToFile("✅ Pre-sale code submitted successfully")
	return nil
}

func generateUUID() string {
	// Simple UUID v4 generator
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// Update GetUserName to use TIXUISID
func GetUserName(session_id string) (string, error) {
	options := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("blink-settings", "imagesEnabled=false"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		// chromedp.Flag("headless", false), // Headless for validation
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancel()

	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	timeoutCtx, timeoutCancel := context.WithTimeout(browserCtx, 30*time.Second)
	defer timeoutCancel()

	var username string

	err := chromedp.Run(timeoutCtx,
		// Set both cookies for compatibility
		network.SetCookie("SID", session_id).WithDomain("tixcraft.com").WithPath("/"),
		network.SetCookie("TIXUISID", session_id).WithDomain("tixcraft.com").WithPath("/"),
		chromedp.Navigate("https://tixcraft.com"),
		chromedp.WaitVisible("#header", chromedp.ByQueryAll),
		chromedp.Text(".user-name", &username, chromedp.ByQuery),
	)

	if err != nil {
		return "", fmt.Errorf("navigation failed: %w", err)
	}

	if strings.TrimSpace(username) == "" {
		return "", fmt.Errorf("could not get username - invalid session ID")
	}

	fmt.Println(username)

	return username, nil
}
