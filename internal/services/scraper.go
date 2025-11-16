package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
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
	return strings.Contains(text, filter)
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

// Scraper main function
func Scraper(base_url string, event_id string, ticket_id string, filter string, per_order_ticket string, sesssion_id string) {
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

	var clicked bool
	var imageBase64 string

	var cookies []*network.Cookie
	log.Println("Step 1: Select seat...")
	err := chromedp.Run(ctx,

		network.SetCookie("SID", sesssion_id).
			WithDomain("tixcraft.com").
			WithPath("/"),
		chromedp.Navigate(url),

		chromedp.WaitVisible("#selectseat", chromedp.ByQuery),
		chromedp.WaitVisible(".area-list", chromedp.ByQueryAll),

		chromedp.Sleep(2*time.Second),

		network.Enable(),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			cookies, err = network.GetCookies().Do(ctx)
			return err
		}),
		chromedp.EvaluateAsDevTools(fmt.Sprintf(`
			(function(){
				const filter="%s";
				const links = document.querySelectorAll(".area-list a");
				for (let a of links){
					if (a.innerText.includes(filter)){
						a.click();
						return true;
					}
				}
				return false;
			})()
		`, filter), &clicked),
	)

	if err != nil {
		log.Println("Error during seat selection:", err)
		return
	}

	for _, c := range cookies {
		if c.Name == "SID" {
			log.Println("SID:", c.Value)
		}
	}

	var ocr_text string

	log.Println("Step 2: Process captcha...")
	for {
		log.Println("Processing captcha...")
		err := chromedp.Run(ctx,
			chromedp.WaitVisible("#TicketForm_verifyCode-image", chromedp.ByID),
			chromedp.Sleep(2*time.Second),
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
			log.Println("Error getting captcha image:", err)
			return
		}

		imageData, _ := base64.StdEncoding.DecodeString(imageBase64)
		os.WriteFile("temp.png", imageData, 0644)

		enhancedPath, _ := enhanceImage("temp.png")
		t_client := gosseract.NewClient()
		defer t_client.Close()
		t_client.SetLanguage("eng")
		t_client.SetPageSegMode(gosseract.PSM_AUTO)
		t_client.SetImage(enhancedPath)

		text, _ := t_client.Text()
		text = strings.TrimSpace(text)
		log.Println("OCR Result:", text)

		if len(text) == 4 {
			ocr_text = text
			log.Println("Captcha is valid!")
			break
		}

		log.Println("Captcha invalid, reloading page...")
		// Reload page to refresh captcha
		chromedp.Run(ctx,
			chromedp.Reload(),
			chromedp.Sleep(2*time.Second),
		)
	}

	log.Println("Step 3: Submit form...")
	for {
		var currentURL string
		var alertText string

		log.Println("Setting ticket quantity...")
		err = chromedp.Run(ctx,

			chromedp.Location(&currentURL),
			chromedp.WaitVisible("#ticketPriceList", chromedp.ByQueryAll),
			chromedp.SetValue("#ticketPriceList select", per_order_ticket, chromedp.ByQuery),
		)
		if err != nil {
			log.Println("Error setting ticket quantity:", err)
			return
		}

		// Listen for website alert dialog
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

		log.Println("Filling captcha and submitting...")
		err = chromedp.Run(ctx,
			chromedp.WaitVisible("#TicketForm_verifyCode", chromedp.ByQuery),
			chromedp.SetValue("#TicketForm_verifyCode", ocr_text, chromedp.ByQuery),
			chromedp.Click("#TicketForm_agree", chromedp.ByQuery),
			chromedp.Sleep(2*time.Second),
			chromedp.Click(`//button[text()='Submit']`, chromedp.BySearch),
		)
		if err != nil {
			log.Println("Error submitting form:", err)
			return
		}

		log.Println("Current URL:", currentURL)
		time.Sleep(2 * time.Second)

		var newURL string
		err = chromedp.Run(ctx, chromedp.Location(&newURL))
		if err != nil {
			log.Println("Error getting new URL:", err)
			return
		}

		if newURL != currentURL {
			log.Println("Form submitted successfully! URL changed:", newURL)
			break
		}

		log.Printf("Alert: %s", alertText)
		if alertText != "" {
			log.Println("Website alert detected:", alertText)
			log.Println("Reloading page for new captcha...")

			// Reload page to refresh captcha
			chromedp.Run(ctx,
				chromedp.Reload(),
				chromedp.Sleep(2*time.Second),
			)

			// Wait for captcha image to appear
			chromedp.Run(ctx,
				chromedp.WaitVisible("#TicketForm_verifyCode-image", chromedp.ByID),
				chromedp.Sleep(1*time.Second),
			)

			// Re-run OCR for new captcha
			log.Println("Re-running OCR for new captcha...")
			imageBase64 = ""
			chromedp.Run(ctx,
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

			imageData, _ := base64.StdEncoding.DecodeString(imageBase64)
			os.WriteFile("temp.png", imageData, 0644)

			enhancedPath, _ := enhanceImage("temp.png")
			t_client := gosseract.NewClient()
			defer t_client.Close()
			t_client.SetLanguage("eng")
			t_client.SetPageSegMode(gosseract.PSM_AUTO)
			t_client.SetImage(enhancedPath)

			text, _ := t_client.Text()
			text = strings.TrimSpace(text)
			ocr_text = text
			log.Println("New OCR Result:", ocr_text)
		}
	}

	log.Println("Scraper finished.")
	select {} // keep browser open
}

