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

	maxAttempts := 10
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		log.Printf("Captcha attempt %d/%d...", attempt, maxAttempts)

		captchaText, err := processCaptcha(ctx)
		if err != nil {
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

	// Set ticket quantity
	log.Println("Setting ticket quantity...")
	err = chromedp.Run(ctx,
		chromedp.WaitVisible("#ticketPriceList", chromedp.ByQueryAll),
		chromedp.SetValue("#ticketPriceList select", perOrderTicket, chromedp.ByQuery),
	)
	if err != nil {
		return false, fmt.Errorf("error setting ticket quantity: %w", err)
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

// ScraperWithLoop wraps the scraper with loop functionality
func ScraperWithLoop(base_url string, event_id string, ticket_id string, filter string, per_order_ticket string, max_tickets string, loop bool, session_id string) {
	if !loop {
		// Single run mode
		Scraper(base_url, event_id, ticket_id, filter, per_order_ticket, session_id)
		return
	}

	// Loop mode: calculate number of iterations
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

	numLoops := (maxTickets + quantity - 1) / quantity // Ceiling division
	log.Printf("Loop mode enabled: Running %d iterations to buy %d tickets (target: %d)", numLoops, quantity*numLoops, maxTickets)

	for i := 1; i <= numLoops; {
		log.Printf("=== Starting iteration %d/%d ===", i, numLoops)
		success := Scraper(base_url, event_id, ticket_id, filter, per_order_ticket, session_id)

		if !success {
			log.Printf("Iteration %d failed, retrying...", i)
			time.Sleep(3 * time.Second)
			continue // Retry the same iteration
		}

		log.Printf("Iteration %d completed successfully", i)
		i++ // Only increment on success

		if i <= numLoops {
			log.Printf("Waiting 3 seconds before next iteration...")
			time.Sleep(3 * time.Second)
		}
	}

	log.Printf("=== All %d iterations complete successfully! ===", numLoops)
}

// Scraper main function with simplified flow
func Scraper(base_url string, event_id string, ticket_id string, filter string, per_order_ticket string, session_id string) bool {
	log.Println("Starting scraper...")

	options := append(
		chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", false),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.UserAgent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
	)

	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, cancel = context.WithTimeout(ctx, 200*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/%s/%s", base_url, event_id, ticket_id)
	log.Printf("Scraping url: %s\n", url)

	var seatVal string

	// STEP 1: Select seat
	log.Println("Step 1: Selecting seat...")
	err := chromedp.Run(ctx,
		network.SetCookie("SID", session_id).
			WithDomain("tixcraft.com").
			WithPath("/"),
		chromedp.Navigate(url),
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
		log.Println("Error during seat selection:", err)
		return false
	}

	log.Printf("Selected seat: %s", seatVal)

	// Main retry loop for captcha + reservation
	maxRetries := 10
	for retry := 1; retry <= maxRetries; retry++ {
		log.Printf("\n=== Attempt %d/%d ===", retry, maxRetries)

		// STEP 2: Bypass captcha
		captchaText, err := bypassCaptcha(ctx)
		if err != nil {
			log.Printf("Failed to bypass captcha: %v", err)
			continue
		}

		// STEP 3: Reserve seat
		success, err := reserveSeat(ctx, captchaText, per_order_ticket, session_id, seatVal, event_id, ticket_id)
		if success {
			// STEP 4: Checkout and extract info
			bookingInfo, err := checkoutAndExtractInfo(ctx)
			if err != nil {
				log.Printf("Error during checkout: %v", err)
				// Still consider it a success since reservation worked
			} else {
				// Add the session and seat info to the booking
				bookingInfo.SessionID = session_id
				bookingInfo.Seat = seatVal
				bookingInfo.EventID = event_id
				bookingInfo.TicketID = ticket_id
				bookingInfo.NumOfTickets = per_order_ticket

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

		err = chromedp.Run(ctx,
			chromedp.Reload(),
			chromedp.Sleep(2*time.Second),
		)
		if err != nil {
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
}

// checkoutAndExtractInfo extracts booking details from cart and clicks reSelect
func checkoutAndExtractInfo(ctx context.Context) (*Booking, error) {
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
	// booking := Booking{
	// 	SessionID:    sessionID,
	// 	Seat:         seat,
	// 	EventID:      eventID,
	// 	TicketID:     ticketID,
	// 	NumOfTickets: numOfTickets,
	// }

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