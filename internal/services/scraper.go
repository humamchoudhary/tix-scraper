package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/ioutil"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/disintegration/imaging"
	"github.com/otiai10/gosseract/v2"
)

// SeatFilter checks if the text contains the filter string
func SeatFilter(text string, filter string) bool {
	return !strings.Contains(text, filter)
}

// enhanceImage processes image to improve OCR accuracy
func enhanceImage(inputPath string) (string, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return "", err
	}

	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	if width < 1000 {
		scale := 1000.0 / float64(width)
		img = imaging.Resize(img, int(float64(width)*scale), int(float64(height)*scale), imaging.Lanczos)
	}

	img = imaging.AdjustContrast(img, 20)
	img = imaging.Sharpen(img, 1.0)
	img = imaging.Grayscale(img)

	outputPath := "temp_enhanced.png"
	out, err := os.Create(outputPath)
	if err != nil {
		return "", err
	}
	defer out.Close()

	err = png.Encode(out, img)
	if err != nil {
		return "", err
	}

	return outputPath, nil
}

// isValidCaptcha validates captcha text: exactly 4 alphabetic characters
func isValidCaptcha(text string) bool {
	text = strings.TrimSpace(text)
	if len(text) != 4 {
		return false
	}
	// Check if all characters are alphabets
	matched, _ := regexp.MatchString("^[a-zA-Z]{4}$", text)
	return matched
}

// processCaptcha handles captcha image extraction and OCR
func processCaptcha(ctx context.Context) (string, error) {
	// Check for cancellation before processing
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("captcha processing cancelled")
	default:
	}

	var imageBase64 string

	err := chromedp.Run(ctx,
		chromedp.WaitVisible("#TicketForm_verifyCode-image", chromedp.ByID),
		chromedp.Sleep(1*time.Second),
		chromedp.Evaluate(`
			(function() {
				const img = document.querySelector('#TicketForm_verifyCode-image');
				const canvas = document.createElement('canvas');
				canvas.width = img.naturalWidth || img.width;
				canvas.height = img.naturalHeight || img.height;
				const ctx = canvas.getContext('2d');
				ctx.drawImage(img, 0, 0);
				return canvas.toDataURL('image/png').split(',')[1];
			})()
		`, &imageBase64),
	)
	if err != nil {
		return "", fmt.Errorf("error getting captcha image: %w", err)
	}

	// Check for cancellation after chromedp operations
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("captcha processing cancelled")
	default:
	}

	imageData, err := base64.StdEncoding.DecodeString(imageBase64)
	if err != nil {
		return "", fmt.Errorf("error decoding captcha image: %w", err)
	}

	if err := os.WriteFile("temp.png", imageData, 0644); err != nil {
		return "", fmt.Errorf("error writing temp image: %w", err)
	}

	enhancedPath, err := enhanceImage("temp.png")
	if err != nil {
		return "", fmt.Errorf("error enhancing image: %w", err)
	}

	// Check for cancellation before OCR
	select {
	case <-ctx.Done():
		return "", fmt.Errorf("captcha processing cancelled")
	default:
	}

	t_client := gosseract.NewClient()
	defer t_client.Close()
	t_client.SetLanguage("eng")
	t_client.SetPageSegMode(gosseract.PSM_AUTO)
	t_client.SetImage(enhancedPath)

	text, err := t_client.Text()
	if err != nil {
		return "", fmt.Errorf("error performing OCR: %w", err)
	}

	return strings.TrimSpace(text), nil
}

// bypassCaptcha repeatedly attempts to solve captcha until valid
func bypassCaptcha(ctx context.Context) (string, error) {
	log.Println("Step 2: Bypassing captcha...")

	maxAttempts := 20
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Check for cancellation at the start of each attempt
		select {
		case <-ctx.Done():
			log.Println("Captcha bypass cancelled by user")
			return "", fmt.Errorf("captcha bypass cancelled")
		default:
		}

		log.Printf("Captcha attempt %d/%d...", attempt, maxAttempts)

		captchaText, err := processCaptcha(ctx)
		if err != nil {
			// Check if error is due to cancellation
			if ctx.Err() != nil {
				return "", fmt.Errorf("captcha bypass cancelled")
			}
			log.Printf("Error processing captcha: %v", err)
			continue
		}

		log.Printf("OCR result: '%s'", captchaText)

		if isValidCaptcha(captchaText) {
			log.Println("Valid captcha obtained!")
			return captchaText, nil
		}

		log.Printf("Invalid captcha (length: %d, alphabetic: %v). Reloading...",
			len(captchaText),
			regexp.MustCompile("^[a-zA-Z]+$").MatchString(captchaText))

		// Check for cancellation before reload
		select {
		case <-ctx.Done():
			log.Println("Captcha bypass cancelled by user")
			return "", fmt.Errorf("captcha bypass cancelled")
		default:
		}

		// Reload page to get new captcha
		err = chromedp.Run(ctx,
			chromedp.Reload(),
			chromedp.Sleep(2*time.Second),
		)
		if err != nil {
			return "", fmt.Errorf("error reloading page: %w", err)
		}
	}

	return "", fmt.Errorf("failed to obtain valid captcha after %d attempts", maxAttempts)
}

// reserveSeat attempts to reserve the seat with the given captcha
func reserveSeat(ctx context.Context, captchaText, perOrderTicket, sessionID, seatVal, eventID, ticketID string) (bool, error) {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		log.Println("Reservation cancelled by user")
		return false, fmt.Errorf("reservation cancelled")
	default:
	}

	log.Println("Step 3: Submitting Reservation...")

	var currentURL string
	var alertText string

	// Setup alert listener
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			alertText = e.Message
			go func() {
				if err := chromedp.Run(ctx, page.HandleJavaScriptDialog(true)); err != nil {
					log.Println("Error handling dialog:", err)
				}
			}()
		}
	})

	// Get current URL
	err := chromedp.Run(ctx, chromedp.Location(&currentURL))
	if err != nil {
		return false, fmt.Errorf("error getting current URL: %w", err)
	}

	// Check for cancellation before setting ticket quantity
	select {
	case <-ctx.Done():
		log.Println("Reservation cancelled by user")
		return false, fmt.Errorf("reservation cancelled")
	default:
	}

	// Set ticket quantity
	log.Println("Setting ticket quantity...")
	err = chromedp.Run(ctx,
		chromedp.WaitVisible("#ticketPriceList", chromedp.ByQueryAll),
		chromedp.SetValue("#ticketPriceList select", perOrderTicket, chromedp.ByQuery),
	)
	if err != nil {
		return false, fmt.Errorf("error setting ticket quantity: %w", err)
	}

	// Check for cancellation before filling captcha
	select {
	case <-ctx.Done():
		log.Println("Reservation cancelled by user")
		return false, fmt.Errorf("reservation cancelled")
	default:
	}

	// Fill captcha and submit
	log.Println("Filling captcha and submitting...")
	err = chromedp.Run(ctx,
		chromedp.WaitVisible("#TicketForm_verifyCode", chromedp.ByQuery),
		chromedp.SetValue("#TicketForm_verifyCode", captchaText, chromedp.ByQuery),
		chromedp.Click("#TicketForm_agree", chromedp.ByQuery),
		chromedp.Sleep(2*time.Second),
		chromedp.Click(`//button[text()='Submit']`, chromedp.BySearch),
		chromedp.Sleep(2*time.Second),
	)
	if err != nil {
		return false, fmt.Errorf("error submitting form: %w", err)
	}

	// Check for cancellation before verifying result
	select {
	case <-ctx.Done():
		log.Println("Reservation cancelled by user")
		return false, fmt.Errorf("reservation cancelled")
	default:
	}

	// Check if URL changed (success)
	var newURL string
	err = chromedp.Run(ctx, chromedp.Location(&newURL))
	if err != nil {
		return false, fmt.Errorf("error getting new URL: %w", err)
	}

	if newURL != currentURL {
		log.Println("Successfully Reserved seat!", newURL)
		return true, nil
	}

	// Check if alert was triggered (failure)
	if alertText != "" {
		log.Println("Website alert detected:", alertText)
		return false, fmt.Errorf("reservation failed: %s", alertText)
	}

	return false, fmt.Errorf("reservation failed: unknown error")
}

// sleepWithCancel sleeps but exits early if ctx is cancelled.
// returns false if cancelled, true if completed normally.
func sleepWithCancel(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// ScraperWithLoop wraps the scraper with loop functionality
func ScraperWithLoop(
	ctx context.Context,
	base_url string,
	event_id string,
	ticket_id string,
	filter string,
	per_order_ticket string,
	max_tickets string,
	loop bool,
	session_id string,
) {

	// ------------------------------
	// Non-loop mode
	// ------------------------------
	if !loop {
		select {
		case <-ctx.Done():
			log.Println("Scraper stopped before starting (single run mode)")
			return
		default:
		}

		Scraper(ctx, base_url, event_id, ticket_id, filter, per_order_ticket, session_id)
		return
	}

	// ------------------------------
	// Loop mode
	// ------------------------------
	quantity, err := strconv.Atoi(per_order_ticket)
	if err != nil {
		log.Printf("Invalid quantity: %s", per_order_ticket)
		return
	}

	maxTickets, err := strconv.Atoi(max_tickets)
	if err != nil {
		log.Printf("Invalid max tickets: %s", max_tickets)
		return
	}

	if quantity <= 0 || maxTickets <= 0 {
		log.Println("Quantity and max tickets must be greater than 0")
		return
	}

	numLoops := (maxTickets + quantity - 1) / quantity
	log.Printf("Loop mode enabled: Running %d iterations to buy %d tickets (target: %d)",
		numLoops, quantity*numLoops, maxTickets)

	// ------------------------------
	// Main loop
	// ------------------------------
	for i := 1; i <= numLoops; {
		// Check for cancellation
		select {
		case <-ctx.Done():
			log.Println("Scraper stopped by user.")
			return
		default:
		}

		log.Printf("=== Starting iteration %d/%d ===", i, numLoops)

		success := Scraper(ctx, base_url, event_id, ticket_id, filter, per_order_ticket, session_id)

		// Retry failed iteration
		if !success {
			log.Printf("Iteration %d failed, retrying...", i)

			// Sleep with cancel support
			if !sleepWithCancel(ctx, 3*time.Second) {
				log.Println("Scraper stopped during retry wait.")
				return
			}

			continue
		}

		log.Printf("Iteration %d completed successfully", i)
		i++ // increment on success

		if i <= numLoops {
			log.Printf("Waiting 3 seconds before next iteration...")

			// Sleep with cancel support
			if !sleepWithCancel(ctx, 3*time.Second) {
				log.Println("Scraper stopped during delay between iterations.")
				return
			}
		}
	}

	log.Printf("=== All %d iterations complete successfully! ===", numLoops)
}

// Scraper main function with simplified flow
func Scraper(ctx context.Context, base_url string, event_id string, ticket_id string, filter string, per_order_ticket string, session_id string) bool {
	// Check for cancellation at the very start
	select {
	case <-ctx.Done():
		log.Println("Scraper stopped before starting")
		return false
	default:
	}

	log.Println("Starting scraper...")

	options := append(
		chromedp.DefaultExecAllocatorOptions[:],
		// chromedp.Flag("headless", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancel()

	// Create a new context that inherits cancellation from the parent ctx
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	defer browserCancel()

	// Create a child context that will be cancelled when either parent ctx or timeout occurs
	timeoutCtx, timeoutCancel := context.WithTimeout(browserCtx, 60*time.Second)
	defer timeoutCancel()

	// Monitor parent context cancellation
	go func() {
		<-ctx.Done()
		log.Println("Parent context cancelled, cancelling browser context...")
		browserCancel()
	}()

	url := fmt.Sprintf("%s/%s/%s", base_url, event_id, ticket_id)
	log.Printf("Scraping url: %s\n", url)

	log.Println("Navigating...")

	// Check for cancellation before navigation
	select {
	case <-ctx.Done():
		log.Println("Scraper stopped before navigation")
		return false
	default:
	}

	var username string
	err := chromedp.Run(timeoutCtx,
		network.SetCookie("SID", session_id).
			WithDomain("tixcraft.com").
			WithPath("/"),
		chromedp.Navigate(url),
		chromedp.WaitVisible("body", chromedp.ByQueryAll),
		chromedp.WaitVisible("#header", chromedp.ByQueryAll),
		chromedp.Text(".user-name", &username, chromedp.ByQuery),
	)

	fmt.Println(err)
	if err != nil || strings.TrimSpace(username) == "" {
		// Check if error is due to cancellation
		if ctx.Err() != nil {
			log.Println("Scraper stopped during navigation")
			return false
		}
		log.Println("❌ Stopping: user not found (not logged in or session expired), Please update the SID")
		return false
	}

	log.Println("Logged in as:", username)

	// Check for cancellation after login verification
	select {
	case <-ctx.Done():
		log.Println("Scraper stopped after login verification")
		return false
	default:
	}

	var seatVal string

	// STEP 1: Select seat
	log.Println("Step 1: Selecting seat...")

	// Check for cancellation before seat selection
	select {
	case <-ctx.Done():
		log.Println("Scraper stopped before seat selection")
		return false
	default:
	}

	err = chromedp.Run(timeoutCtx,
		chromedp.WaitVisible("#selectseat", chromedp.ByQuery),
		chromedp.WaitVisible(".area-list", chromedp.ByQueryAll),
		chromedp.Sleep(2*time.Second),
		network.Enable(),
		chromedp.EvaluateAsDevTools(fmt.Sprintf(`
			(function(){
				const filter = "%s";
				const links = document.querySelectorAll(".area-list a");

				for (let a of links){
					if (!a.innerText.includes(filter)){
						let directText = "";
						for (const node of a.childNodes) {
							if (node.nodeType === Node.TEXT_NODE) {
								directText += node.textContent.trim();
							}
						}
						a.click();
						return directText;
					}
				}
				return "";
			})()
		`, filter), &seatVal),
	)

	if err != nil {
		// Check if error is due to cancellation
		if ctx.Err() != nil {
			log.Println("Scraper stopped during seat selection")
			return false
		}
		log.Println("Error during seat selection:", err)
		return false
	}

	log.Printf("Selected seat: %s", seatVal)

	// Check for cancellation after seat selection
	select {
	case <-ctx.Done():
		log.Println("Scraper stopped after seat selection")
		return false
	default:
	}

	// Main retry loop for captcha + reservation
	maxRetries := 10
	for retry := 1; retry <= maxRetries; retry++ {
		// Check for cancellation at the start of each retry
		select {
		case <-ctx.Done():
			log.Println("Scraper stopped during retry loop")
			return false
		default:
		}

		log.Printf("\n=== Attempt %d/%d ===", retry, maxRetries)

		// STEP 2: Bypass captcha
		captchaText, err := bypassCaptcha(timeoutCtx)
		if err != nil {
			// Check if error is due to cancellation
			if ctx.Err() != nil {
				log.Println("Scraper stopped during captcha bypass")
				return false
			}
			log.Printf("Failed to bypass captcha: %v", err)
			continue
		}

		// Check for cancellation before reservation
		select {
		case <-ctx.Done():
			log.Println("Scraper stopped before reservation")
			return false
		default:
		}

		// STEP 3: Reserve seat
		success, err := reserveSeat(timeoutCtx, captchaText, per_order_ticket, session_id, seatVal, event_id, ticket_id)
		if success {
			// Check for cancellation before checkout
			select {
			case <-ctx.Done():
				log.Println("Scraper stopped before checkout (reservation succeeded)")
				return true // Consider it success since reservation worked
			default:
			}

			// STEP 4: Checkout and extract info
			bookingInfo, err := checkoutAndExtractInfo(timeoutCtx)
			if err != nil {
				// Check if error is due to cancellation
				if ctx.Err() != nil {
					log.Println("Scraper stopped during checkout")
					return true // Consider it success since reservation worked
				}
				log.Printf("Error during checkout: %v", err)
				// Still consider it a success since reservation worked
			} else {
				// Add the session and seat info to the booking
				bookingInfo.SessionID = session_id
				bookingInfo.Seat = seatVal
				bookingInfo.EventID = event_id
				bookingInfo.TicketID = ticket_id
				bookingInfo.NumOfTickets = per_order_ticket
				bookingInfo.UserName = username

				// Save complete booking info
				saveBooking(*bookingInfo)
			}

			log.Println("Scraper finished successfully.")
			time.Sleep(5 * time.Second)
			return true
		}

		// If reservation failed, log and retry
		log.Printf("Reservation failed: %v", err)
		log.Println("Reloading page for retry...")

		// Check for cancellation before reload
		select {
		case <-ctx.Done():
			log.Println("Scraper stopped before page reload")
			return false
		default:
		}

		err = chromedp.Run(timeoutCtx,
			chromedp.Reload(),
			chromedp.Sleep(2*time.Second),
		)
		if err != nil {
			// Check if error is due to cancellation
			if ctx.Err() != nil {
				log.Println("Scraper stopped during page reload")
				return false
			}
			log.Printf("Error reloading page: %v", err)
			return false
		}
	}

	log.Printf("Failed after %d attempts", maxRetries)
	return false
}

// Booking represents the structure for a single booking entry.
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

// checkoutAndExtractInfo extracts booking details from cart and clicks reSelect
func checkoutAndExtractInfo(ctx context.Context) (*Booking, error) {
	// Check for cancellation before starting
	select {
	case <-ctx.Done():
		log.Println("Checkout cancelled by user")
		return nil, fmt.Errorf("checkout cancelled")
	default:
	}

	log.Println("Step 4: Extracting checkout information...")

	var orderNumber, eventName, eventDate, eventVenue string
	var section, seatInfo, ticketInfo string
	var ticketQty, serviceFee, total string

	err := chromedp.Run(ctx,
		// Wait for cart table to be visible
		chromedp.WaitVisible("#cartList", chromedp.ByID),
		chromedp.Sleep(2*time.Second),

		// Extract order number
		chromedp.Evaluate(`
			(function() {
				const orderNum = document.querySelector('.hex_Order_number');
				return orderNum ? orderNum.textContent.trim() : '';
			})()
		`, &orderNumber),

		// Extract event name
		chromedp.Evaluate(`
			(function() {
				const name = document.querySelector('.ticketEventName');
				return name ? name.textContent.trim() : '';
			})()
		`, &eventName),

		// Extract event date
		chromedp.Evaluate(`
			(function() {
				const date = document.querySelector('.ticketEventDate');
				if (!date) return '';
				// Remove the icon and get text
				const text = date.textContent.trim();
				return text.replace(/\s+/g, ' ');
			})()
		`, &eventDate),

		// Extract event venue
		chromedp.Evaluate(`
			(function() {
				const venue = document.querySelector('.ticketEventVenue');
				if (!venue) return '';
				const text = venue.textContent.trim();
				return text.replace(/\s+/g, ' ');
			})()
		`, &eventVenue),

		// Extract section (2nd td in orderTicket row)
		chromedp.Evaluate(`
			(function() {
				const row = document.querySelector('tr.orderTicket');
				if (!row) return '';
				const tds = row.querySelectorAll('td');
				return tds[1] ? tds[1].textContent.trim() : '';
			})()
		`, &section),

		// Extract seat info (3rd td in orderTicket row)
		chromedp.Evaluate(`
			(function() {
				const row = document.querySelector('tr.orderTicket');
				if (!row) return '';
				const tds = row.querySelectorAll('td');
				return tds[2] ? tds[2].textContent.trim() : '';
			})()
		`, &seatInfo),

		// Extract ticket info (4th td in orderTicket row)
		chromedp.Evaluate(`
			(function() {
				const row = document.querySelector('tr.orderTicket');
				if (!row) return '';
				const tds = row.querySelectorAll('td');
				return tds[3] ? tds[3].textContent.trim() : '';
			})()
		`, &ticketInfo),

		// Extract ticket quantity
		chromedp.Evaluate(`
			(function() {
				const qty = document.querySelector('.text-primary.bold');
				return qty ? qty.textContent.trim() : '';
			})()
		`, &ticketQty),

		// Extract service fee
		chromedp.Evaluate(`
			(function() {
				const fee = document.querySelector('#orderFee');
				return fee ? fee.textContent.trim() : '';
			})()
		`, &serviceFee),

		// Extract total
		chromedp.Evaluate(`
			(function() {
				const total = document.querySelector('#orderAmount');
				return total ? total.textContent.trim() : '';
			})()
		`, &total),
	)

	if err != nil {
		return nil, fmt.Errorf("error extracting checkout info: %w", err)
	}

	// Check for cancellation after extraction
	select {
	case <-ctx.Done():
		log.Println("Checkout cancelled after extraction")
		return nil, fmt.Errorf("checkout cancelled")
	default:
	}

	booking := &Booking{
		OrderNumber: orderNumber,
		EventName:   eventName,
		EventDate:   eventDate,
		EventVenue:  eventVenue,
		Section:     section,
		SeatInfo:    seatInfo,
		TicketInfo:  ticketInfo,
		TicketQty:   ticketQty,
		ServiceFee:  serviceFee,
		Total:       total,
	}

	log.Printf("Extracted booking info:")
	log.Printf("  Order Number: %s", orderNumber)
	log.Printf("  Event: %s", eventName)
	log.Printf("  Date: %s", eventDate)
	log.Printf("  Venue: %s", eventVenue)
	log.Printf("  Section: %s", section)
	log.Printf("  Seat: %s", seatInfo)
	log.Printf("  Ticket: %s", ticketInfo)
	log.Printf("  Quantity: %s", ticketQty)
	log.Printf("  Service Fee: %s", serviceFee)
	log.Printf("  Total: %s", total)

	// Check for cancellation before clicking reSelect
	select {
	case <-ctx.Done():
		log.Println("Checkout cancelled before reSelect")
		return booking, nil // Return booking info even if cancelled
	default:
	}

	// Click reSelect button
	log.Println("Clicking reSelect button...")
	err = chromedp.Run(ctx,
		chromedp.WaitVisible("#reSelect", chromedp.ByID),
		chromedp.Click("#reSelect", chromedp.ByID),
		chromedp.Sleep(2*time.Second),
	)

	if err != nil {
		return nil, fmt.Errorf("error clicking reSelect: %w", err)
	}

	log.Println("Successfully clicked reSelect button")
	return booking, nil
}

func saveBooking(booking Booking) {
	// Read existing bookings
	var bookings []Booking
	data, err := ioutil.ReadFile("bookings.json")
	if err == nil {
		// File exists, unmarshal the data
		if err := json.Unmarshal(data, &bookings); err != nil {
			log.Printf("Error unmarshalling bookings.json: %v", err)
			return
		}
	} else if !os.IsNotExist(err) {
		// An error other than "not found" occurred
		log.Printf("Error reading bookings.json: %v", err)
		return
	}

	// Append the new booking
	bookings = append(bookings, booking)

	// Marshal the updated bookings slice
	updatedData, err := json.MarshalIndent(bookings, "", "  ")
	if err != nil {
		log.Printf("Error marshalling bookings: %v", err)
		return
	}

	// Write the data back to the file
	if err := ioutil.WriteFile("bookings.json", updatedData, 0644); err != nil {
		log.Printf("Error writing to bookings.json: %v", err)
	} else {
		log.Println("Successfully saved booking to bookings.json")
	}
}