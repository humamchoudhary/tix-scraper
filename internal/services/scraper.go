package services

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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

func RunScraper(ctx context.Context, cfg ScraperConfig) {
	// 1. Setup Chrome Allocator (Do this ONCE)
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		// chromedp.Flag("headless", false),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()

	// 2. Create Browser Context ONCE for all iterations
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	// Ensure browser is closed when parent context is done
	go func() {
		<-ctx.Done()
		browserCancel()
	}()

	// 3. Determine Loop Count
	numLoops := 1
	quantity, _ := strconv.Atoi(cfg.PerOrderTicket)
	maxTickets, _ := strconv.Atoi(cfg.MaxTickets)

	if cfg.Loop && quantity > 0 && maxTickets > 0 {
		numLoops = (maxTickets + quantity - 1) / quantity
		log.Printf("Loop mode: Running %d iterations", numLoops)
	}

	// 4. Execute Logic
	isFirstIteration := true
	for i := 1; i <= numLoops; {
		select {
		case <-ctx.Done():
			log.Println("Scraper stopped by user.")
			return
		default:
		}

		log.Printf("=== Iteration %d/%d ===", i, numLoops)

		// FIXED: Pass browserCtx instead of creating new timeout context
		// The timeout should be on operations, not the browser itself
		success := executeBookingFlow(browserCtx, ctx, cfg, isFirstIteration)
		isFirstIteration = false // Only first iteration dismisses cookie banner

		if success {
			i++
			if i <= numLoops {
				log.Println("Success! Waiting 3 seconds before next...")
				time.Sleep(3 * time.Second)
			}
		} else {
			log.Println("Iteration failed. Retrying in 2 seconds...")
			time.Sleep(2 * time.Second)
		}
	}
	log.Println("All iterations complete.")
}

func executeBookingFlow(ctx context.Context, parentCtx context.Context, cfg ScraperConfig, isFirstIteration bool) bool {
	// --- Step 1: Navigate & Login Check ---
	url := fmt.Sprintf("%s/%s/%s", cfg.BaseURL, cfg.EventID, cfg.TicketID)
	var username string

	log.Println("Navigating to:", url)
	err := chromedp.Run(ctx,
		network.SetCookie("SID", cfg.SessionID).WithDomain("tixcraft.com").WithPath("/"),
		chromedp.Navigate(url),
		chromedp.WaitVisible("#header", chromedp.ByQuery),
		chromedp.Text(".user-name", &username, chromedp.ByQuery),
	)

	if err != nil || strings.TrimSpace(username) == "" {
		log.Println("❌ Login failed or User not found. Update SID.")
		return false
	}
	log.Printf("Logged in as: %s", username)

	// --- Step 2: Select Seat ---
	var seatVal string

	// Build actions list conditionally
	actions := []chromedp.Action{
		chromedp.WaitVisible("#selectseat", chromedp.ByQuery),
		chromedp.WaitVisible(".area-list", chromedp.ByQueryAll),
	}

	// Only dismiss cookie banner on first iteration
	if isFirstIteration {
		log.Println("First iteration: dismissing cookie banner if present")
		actions = append([]chromedp.Action{
			chromedp.ActionFunc(func(ctx context.Context) error {
				_ = chromedp.Click("//button[text()='Accept All']", chromedp.BySearch).Do(ctx)
				return nil
			}),
		}, actions...)
	}

	actions = append(actions,
		chromedp.EvaluateAsDevTools(fmt.Sprintf(`
            (function(){
                const filter = "%s";
                const links = document.querySelectorAll(".area-list a");
                for (let a of links){
                    if (!a.innerText.includes(filter)){
                        let directText = Array.from(a.childNodes)
                            .filter(node => node.nodeType === Node.TEXT_NODE)
                            .map(node => node.textContent.trim())
                            .join("");
                        
                        a.click();
                        return directText || a.innerText;
                    }
                }
                return "";
            })()
        `, cfg.Filter), &seatVal),
	)

	err = chromedp.Run(ctx, actions...)

	if err != nil {
		log.Printf("Error selecting seat: %v", err)
		return false
	}
	log.Printf("Selected seat: %s", seatVal)

	// --- Step 3: Captcha Loop ---
	maxCaptchaRetries := 10
	for j := 0; j < maxCaptchaRetries; j++ {
		if parentCtx.Err() != nil {
			return false
		}

		captchaText, err := processCaptcha(ctx)
		if err != nil {
			log.Printf("Captcha error: %v", err)
			reloadPage(ctx)
			continue
		}

		log.Printf("Attempting Captcha: %s", captchaText)

		// --- Step 4: Submit & Validate ---
		var currentURL, newURL string
		var alertMsg string

		chromedp.ListenTarget(ctx, func(ev interface{}) {
			if e, ok := ev.(*page.EventJavascriptDialogOpening); ok {
				alertMsg = e.Message
				go func() {
					_ = chromedp.Run(ctx, chromedp.ActionFunc(func(c context.Context) error {
						return page.HandleJavaScriptDialog(true).Do(c)
					}))
				}()
			}
		})

		err = chromedp.Run(ctx,
			chromedp.Location(&currentURL),
			chromedp.SetValue("#ticketPriceList select", cfg.PerOrderTicket, chromedp.ByQuery),
			chromedp.SetValue("#TicketForm_verifyCode", captchaText, chromedp.ByQuery),
			chromedp.Click("#TicketForm_agree", chromedp.ByQuery),
			chromedp.Click("button[type='submit']", chromedp.ByQuery),

			chromedp.Sleep(2000*time.Millisecond),
			chromedp.Location(&newURL),
		)

		if err != nil {
			log.Printf("Submission error: %v", err)
			reloadPage(ctx)
			continue
		}

		if alertMsg != "" {
			log.Printf("Website Alert: %s", alertMsg)
			reloadPage(ctx)
			continue
		}

		if newURL != currentURL {
			log.Println("🎉 Reservation Successful!")

			// --- Step 5: Checkout extraction ---
			booking, err := fastCheckoutExtract(ctx)
			if err == nil && booking != nil {
				booking.SessionID = cfg.SessionID
				booking.Seat = seatVal
				booking.EventID = cfg.EventID
				booking.UserName = username
				saveBooking(*booking)
			}

			return true
		}

		log.Println("URL didn't change, retrying captcha...")
		reloadPage(ctx)
	}

	return false
}

func reloadPage(ctx context.Context) {
	_ = chromedp.Run(ctx, chromedp.Reload(), chromedp.Sleep(2*time.Second))
}

func processCaptcha(ctx context.Context) (string, error) {
	var base64Data string

	err := chromedp.Run(ctx,
		chromedp.WaitVisible("#TicketForm_verifyCode-image", chromedp.ByID),
		chromedp.Sleep(1000*time.Millisecond),
		chromedp.Evaluate(`
            (function() {
                const img = document.querySelector('#TicketForm_verifyCode-image');
                if (!img.complete || img.naturalWidth === 0) {
                    return ""; 
                }
                const canvas = document.createElement('canvas');
                canvas.width = img.naturalWidth;
                canvas.height = img.naturalHeight;
                const ctx = canvas.getContext('2d');
                ctx.drawImage(img, 0, 0);
                return canvas.toDataURL('image/png');
            })()
        `, &base64Data),
	)
	if err != nil {
		return "", fmt.Errorf("js execution failed: %w", err)
	}

	if base64Data == "" {
		return "", fmt.Errorf("image not loaded or empty")
	}

	parts := strings.Split(base64Data, ",")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid base64 format")
	}
	imageBytes, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %w", err)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = godotenv.Load()
	apiKey := os.Getenv("OCR_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("missing OCR_API_KEY")
	}

	writer.WriteField("apikey", apiKey)
	writer.WriteField("language", "eng")
	writer.WriteField("scale", "true")
	writer.WriteField("OCREngine", "2")

	part, err := writer.CreateFormFile("file", "captcha.png")
	if err != nil {
		return "", err
	}

	if _, err := part.Write(imageBytes); err != nil {
		return "", err
	}

	writer.Close()

	req, err := http.NewRequest("POST", "https://api.ocr.space/parse/image", body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("api request failed: %w", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if parsed, ok := result["ParsedResults"].([]interface{}); ok && len(parsed) > 0 {
		if data, ok := parsed[0].(map[string]interface{}); ok {
			if text, ok := data["ParsedText"].(string); ok {
				return strings.ToLower(strings.TrimSpace(text)), nil
			}
		}
	}

	return "", fmt.Errorf("no text found")
}

// FIXED: Use the same context passed in, don't spawn goroutine with new context
func fastCheckoutExtract(ctx context.Context) (*Booking, error) {
	var jsonStr string

	err := chromedp.Run(ctx,
		chromedp.WaitVisible("#cartList", chromedp.ByID),
		chromedp.Evaluate(`(function() {
			const getText = (sel) => {
				const el = document.querySelector(sel);
				return el ? el.innerText.trim() : "";
			};
			
			const getCell = (rowIdx, colIdx) => {
				const row = document.querySelectorAll('tr.orderTicket')[rowIdx];
				if(!row) return "";
				const cells = row.querySelectorAll('td');
				return cells[colIdx] ? cells[colIdx].innerText.trim() : "";
			};

			const data = {
				order_number: getText('.hex_Order_number'),
				event_name: getText('.ticketEventName'),
				event_date: getText('.ticketEventDate').replace(/\s+/g, ' '),
				event_venue: getText('.ticketEventVenue').replace(/\s+/g, ' '),
				ticket_qty: getText('.text-primary.bold'),
				service_fee: getText('#orderFee'),
				total: getText('#orderAmount'),
				section: getCell(0, 1),
				seat_info: getCell(0, 2),
				ticket_info: getCell(0, 3)
			};
			return JSON.stringify(data);
		})()`, &jsonStr),
	)

	if err != nil {
		return nil, err
	}

	var b Booking
	if err := json.Unmarshal([]byte(jsonStr), &b); err != nil {
		return nil, err
	}

	// FIXED: Click reSelect synchronously using the same context
	// This ensures navigation completes before next iteration
	log.Println("Clicking reSelect button...")
	err = chromedp.Run(ctx,
		chromedp.Click("#reSelect", chromedp.ByID),
		// chromedp.Sleep(2*time.Second), // Wait for navigation
	)
	if err != nil {
		log.Printf("Warning: Failed to click reSelect: %v", err)
	}

	return &b, nil
}

var fileMutex sync.Mutex

func saveBooking(booking Booking) {
	fileMutex.Lock()
	defer fileMutex.Unlock()

	var bookings []Booking
	data, err := os.ReadFile("bookings.json")
	if err == nil {
		_ = json.Unmarshal(data, &bookings)
	}

	bookings = append(bookings, booking)
	updatedData, _ := json.MarshalIndent(bookings, "", "  ")
	_ = os.WriteFile("bookings.json", updatedData, 0644)
	log.Println("✅ Booking saved to file.")
}

func GetUserName(session_id string) (string, error) {
	options := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("blink-settings", "imagesEnabled=false"),
		chromedp.Flag("disable-extensions", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		// chromedp.Flag("headless", false),
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
		network.SetCookie("SID", session_id).
			WithDomain("tixcraft.com").
			WithPath("/"),
		chromedp.Navigate("https://tixcraft.com"),
		chromedp.WaitVisible("#header", chromedp.ByQueryAll),
		chromedp.Text(".user-name", &username, chromedp.ByQuery),
	)

	if err != nil || strings.TrimSpace(username) == "" {
		return "", fmt.Errorf("could not get username")
	}

	return username, nil
}
