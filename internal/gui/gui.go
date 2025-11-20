package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"sync"
	"time"

	"tix-scraper/internal/services"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

var (
	// Modern color scheme inspired by Dracula + Nord
	bgColor       = color.NRGBA{R: 22, G: 24, B: 35, A: 255}    // Deep background
	sidebarBg     = color.NRGBA{R: 28, G: 30, B: 42, A: 255}    // Sidebar background
	cardBg        = color.NRGBA{R: 36, G: 39, B: 54, A: 255}    // Card background
	borderColor   = color.NRGBA{R: 59, G: 66, B: 82, A: 255}    // Subtle border
	textColor     = color.NRGBA{R: 229, G: 233, B: 240, A: 255} // Light text
	accentColor   = color.NRGBA{R: 136, G: 192, B: 208, A: 255} // Soft blue accent
	successColor  = color.NRGBA{R: 163, G: 190, B: 140, A: 255} // Muted green
	runningColor  = color.NRGBA{R: 235, G: 203, B: 139, A: 255} // Warm orange
	dangerColor   = color.NRGBA{R: 191, G: 97, B: 106, A: 255}  // Soft red
	disabledColor = color.NRGBA{R: 129, G: 137, B: 153, A: 255} // Muted gray
	highlightBg   = color.NRGBA{R: 46, G: 52, B: 64, A: 255}    // Highlight background
	purpleAccent  = color.NRGBA{R: 180, G: 142, B: 173, A: 255} // Soft purple
)

type BotConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	User       User   `json:"user"`
	SID        string `json:"sid"`
	EventID    string `json:"event_id"`
	TicketID   string `json:"ticket_id"`
	Filter     string `json:"filter"`
	Quantity   string `json:"quantity"`
	MaxTickets string `json:"max_tickets"`
	Loop       bool   `json:"loop"`
	IsRunning  bool   `json:"-"`
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

type User struct {
	SID      string `json:"sid"`
	Username string `json:"username"`
}

type GUI struct {
	th                   *material.Theme
	w                    *app.Window
	bots                 []*Bot
	selectedBot          int
	addBotBtn            widget.Clickable
	logView              *LogView
	bookingsView         *BookingsView
	usersView            *UsersView
	showBookings         bool
	showUsers            bool
	bookingsTabBtn       widget.Clickable
	botsTabBtn           widget.Clickable
	usersTabBtn          widget.Clickable
	mu                   sync.Mutex
	OcrSpaceAPIKeyEditor widget.Editor
}

type Bot struct {
	config    BotConfig
	selectBtn widget.Clickable
	deleteBtn widget.Clickable

	// Editors
	nameEditor       widget.Editor
	userDropdown     Dropdown
	eventIDEditor    widget.Editor
	ticketIDEditor   widget.Editor
	filterEditor     widget.Editor
	quantityEditor   widget.Editor
	maxTicketsEditor widget.Editor
	loopCheckbox     widget.Bool

	runBtn widget.Clickable

	cancel context.CancelFunc
}

type BookingsView struct {
	gui           *GUI
	list          widget.List
	bookings      []Booking
	deleteButtons []widget.Clickable
	deleteAllBtn  widget.Clickable
	refreshBtn    widget.Clickable
	mu            sync.Mutex
}

type UsersView struct {
	gui           *GUI
	list          widget.List
	users         []User
	deleteButtons []widget.Clickable
	deleteAllBtn  widget.Clickable
	refreshBtn    widget.Clickable
	mu            sync.Mutex
	sidEditor     widget.Editor
	validateBtn   widget.Clickable
	validating    bool
}

type Dropdown struct {
	Options    []string
	selected   int
	clickable  widget.Clickable
	list       widget.List
	isOpen     bool
	clickables []widget.Clickable
}

func (d *Dropdown) Layout(gtx C, th *material.Theme) D {
	if d.clickable.Clicked(gtx) {
		d.isOpen = !d.isOpen
	}

	if len(d.clickables) != len(d.Options) {
		d.clickables = make([]widget.Clickable, len(d.Options))
	}

	for i := range d.Options {
		if d.clickables[i].Clicked(gtx) {
			d.selected = i
			d.isOpen = false
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			border := widget.Border{
				Color:        borderColor,
				CornerRadius: unit.Dp(8),
				Width:        unit.Dp(1),
			}
			return border.Layout(gtx, func(gtx C) D {
				return d.clickable.Layout(gtx, func(gtx C) D {
					return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx C) D {
						return material.Body1(th, d.Options[d.selected]).Layout(gtx)
					})
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			if !d.isOpen {
				return D{}
			}
			// paint a background for the list
			macro := op.Record(gtx.Ops)
			dims := layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx C) D {
				var children []layout.FlexChild
				for i := range d.Options {
					index := i
					children = append(children, layout.Rigid(func(gtx C) D {
						return material.Button(th, &d.clickables[index], d.Options[index]).Layout(gtx)
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			})
			call := macro.Stop()
			// draw the background
			rect := clip.Rect{Max: dims.Size}
			paint.FillShape(gtx.Ops, cardBg, rect.Op())
			// draw the list
			call.Add(gtx.Ops)

			return dims
		}),
	)
}
func NewGUI() *GUI {
	th := material.NewTheme()
	th.Palette.Bg = bgColor
	th.Palette.Fg = textColor

	g := &GUI{
		th:           th,
		selectedBot:  -1,
		logView:      &LogView{},
		bookingsView: &BookingsView{},
		usersView:    &UsersView{},
		showBookings: false,
	}

	g.bookingsView.gui = g
	g.usersView.gui = g
	g.loadBots()
	g.bookingsView.loadBookings()
	g.usersView.loadUsers()

	if len(g.bots) == 0 {
		g.addBot()
		g.selectedBot = 0
	}

	return g
}

func (g *GUI) addBot() {
	bot := &Bot{
		config: BotConfig{
			ID:   fmt.Sprintf("bot_%d", time.Now().Unix()),
			Name: fmt.Sprintf("Bot #%d", len(g.bots)+1),
		},
		nameEditor:       widget.Editor{SingleLine: true},
		eventIDEditor:    widget.Editor{SingleLine: true},
		ticketIDEditor:   widget.Editor{SingleLine: true},
		filterEditor:     widget.Editor{SingleLine: true},
		quantityEditor:   widget.Editor{SingleLine: true},
		maxTicketsEditor: widget.Editor{SingleLine: true},
	}

	bot.nameEditor.SetText(bot.config.Name)
	bot.eventIDEditor.SetText(bot.config.EventID)
	bot.ticketIDEditor.SetText(bot.config.TicketID)
	bot.filterEditor.SetText(bot.config.Filter)
	bot.quantityEditor.SetText(bot.config.Quantity)
	bot.maxTicketsEditor.SetText(bot.config.MaxTickets)
	bot.loopCheckbox.Value = bot.config.Loop

	g.bots = append(g.bots, bot)
	g.selectedBot = len(g.bots) - 1
}

func (g *GUI) Run() {
	g.w = new(app.Window)
	g.w.Option(
		app.Title("Tix Scraper - Multi Bot & Bookings"),
		app.Size(unit.Dp(1200), unit.Dp(750)),
	)
	g.logView.gui = g

	go func() {
		if err := g.loop(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func (g *GUI) loop() error {
	var ops op.Ops
	for {
		switch e := g.w.Event().(type) {
		case app.DestroyEvent:
			g.saveBots()
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			g.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (g *GUI) Layout(gtx C) D {

	paint.Fill(gtx.Ops, bgColor)
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return g.layoutSidebar(gtx)
		}),
		layout.Flexed(1, func(gtx C) D {
			return g.layoutMain(gtx)
		}),
	)
}

func (g *GUI) layoutSidebar(gtx C) D {
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(280))
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	paint.FillShape(gtx.Ops, sidebarBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// App Title
			layout.Rigid(func(gtx C) D {
				label := material.H5(g.th, "🎫 Tix Scraper")
				label.Color = accentColor
				return layout.Inset{Bottom: unit.Dp(24)}.Layout(gtx, label.Layout)
			}),
			// Tab Buttons
			layout.Rigid(func(gtx C) D {
				return g.layoutTabButtons(gtx)
			}),
			// Content based on selected tab
			layout.Flexed(1, func(gtx C) D {
				if g.showBookings {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							label := material.Body1(g.th, fmt.Sprintf("%d Bookings", len(g.bookingsView.bookings)))
							label.Color = disabledColor
							label.TextSize = unit.Sp(13)
							return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, label.Layout)
						}),
					)
				}
				if g.showUsers {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							label := material.Body1(g.th, fmt.Sprintf("%d Users", len(g.usersView.users)))
							label.Color = disabledColor
							label.TextSize = unit.Sp(13)
							return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, label.Layout)
						}),
					)
				}
				return g.layoutBotList(gtx)
			}),
			// Action Button
			layout.Rigid(func(gtx C) D {
				if g.showBookings || g.showUsers {
					return D{}
				}

				if g.addBotBtn.Clicked(gtx) {
					g.addBot()
					g.saveBots()
					g.w.Invalidate()
				}

				btn := material.Button(g.th, &g.addBotBtn, "✚ Add Bot")
				btn.Background = accentColor
				btn.Color = bgColor
				btn.CornerRadius = unit.Dp(8)
				return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, btn.Layout)
			}),
		)
	})
}

func (g *GUI) layoutTabButtons(gtx C) D {
	if g.botsTabBtn.Clicked(gtx) {
		g.showBookings = false
		g.showUsers = false
		g.w.Invalidate()
	}

	if g.bookingsTabBtn.Clicked(gtx) {
		g.showBookings = true
		g.showUsers = false
		g.bookingsView.loadBookings()
		g.w.Invalidate()
	}

	if g.usersTabBtn.Clicked(gtx) {
		g.showUsers = true
		g.showBookings = false
		g.usersView.loadUsers()
		g.w.Invalidate()
	}

	minHeight := gtx.Dp(40) // desired minimum height

	// Helper to create a full-size button
	layoutTab := func(btn *widget.Clickable, labelText string, selected bool) D {
		bg := cardBg
		txtColor := disabledColor
		if selected {
			bg = accentColor
			txtColor = bgColor
		}

		return btn.Layout(gtx, func(gtx C) D {
			// Ensure minimum height
			gtx.Constraints.Min.Y = minHeight

			gtx.Constraints.Min.X = gtx.Dp(80) // minimum width
			gtx.Constraints.Max.X = gtx.Dp(80)
			// Draw full background with corner radius
			rect := image.Rectangle{Max: gtx.Constraints.Max} // public type
			defer clip.UniformRRect(rect, gtx.Dp(8)).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, bg)

			// Center the label
			return layout.Center.Layout(gtx, func(gtx C) D {
				label := material.Body2(g.th, labelText)
				label.Color = txtColor
				return label.Layout(gtx)
			})
		})
	}

	return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D { return layoutTab(&g.botsTabBtn, "Bots", !g.showBookings && !g.showUsers) }),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx C) D { return layoutTab(&g.bookingsTabBtn, "Bookings", g.showBookings) }),
		layout.Rigid(layout.Spacer{Width: unit.Dp(8)}.Layout),
		layout.Flexed(1, func(gtx C) D { return layoutTab(&g.usersTabBtn, "Accounts", g.showUsers) }),
	)
}

func (g *GUI) layoutBotList(gtx C) D {
	list := &widget.List{
		List: layout.List{Axis: layout.Vertical},
	}

	return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
		return material.List(g.th, list).Layout(gtx, len(g.bots), func(gtx C, i int) D {
			bot := g.bots[i]

			if bot.selectBtn.Clicked(gtx) {
				g.selectedBot = i
				g.w.Invalidate()
			}

			if bot.deleteBtn.Clicked(gtx) && len(g.bots) > 1 {
				g.bots = append(g.bots[:i], g.bots[i+1:]...)
				if g.selectedBot >= len(g.bots) {
					g.selectedBot = len(g.bots) - 1
				}
				g.saveBots()
				g.w.Invalidate()
			}

			return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx C) D {
				return g.layoutBotCard(gtx, bot, i)
			})
		})
	})
}

func (g *GUI) layoutBotCard(gtx C, bot *Bot, index int) D {
	isSelected := g.selectedBot == index

	bgCol := cardBg
	borderCol := borderColor
	if isSelected {
		bgCol = highlightBg
		borderCol = accentColor
	}

	minHeight := gtx.Dp(60) // Minimum height for a bot card

	return widget.Border{
		Color:        borderCol,
		Width:        unit.Dp(2),
		CornerRadius: unit.Dp(10),
	}.Layout(gtx, func(gtx C) D {
		rect := image.Rectangle{Max: image.Pt(gtx.Constraints.Max.X, minHeight)}
		defer clip.UniformRRect(rect, gtx.Dp(10)).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, bgCol)

		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx C) D {
				// Empty expanded layer just ensures min height
				return D{}
			}),
			layout.Stacked(func(gtx C) D {
				return bot.selectBtn.Layout(gtx, func(gtx C) D {
					return layout.UniformInset(unit.Dp(14)).Layout(gtx, func(gtx C) D {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical, Spacing: layout.SpaceBetween}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										label := material.Body1(g.th, bot.config.Name)
										label.Color = textColor
										label.TextSize = unit.Sp(15)
										return label.Layout(gtx)
									}),
									layout.Rigid(func(gtx C) D {
										return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx C) D {
											if bot.config.IsRunning {
												status := material.Caption(g.th, "● Running")
												status.Color = successColor
												status.TextSize = unit.Sp(12)
												return status.Layout(gtx)
											}
											status := material.Caption(g.th, "○ Idle")
											status.Color = disabledColor
											status.TextSize = unit.Sp(12)
											return status.Layout(gtx)
										})
									}),
								)
							}),
							layout.Rigid(func(gtx C) D {
								if len(g.bots) <= 1 {
									return D{}
								}

								return widget.Border{
									Color:        dangerColor,
									Width:        unit.Dp(1),
									CornerRadius: unit.Dp(6),
								}.Layout(gtx, func(gtx C) D {
									defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(6)).Push(gtx.Ops).Pop()
									return bot.deleteBtn.Layout(gtx, func(gtx C) D {
										return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx C) D {
											label := material.Body2(g.th, "✕")
											label.Color = dangerColor
											label.TextSize = unit.Sp(12)
											return label.Layout(gtx)
										})
									})
								})
							}),
						)
					})
				})
			}),
		)
	})
}

func (g *GUI) layoutMain(gtx C) D {
	if g.showBookings {
		return g.bookingsView.Layout(gtx)
	}
	if g.showUsers {
		return g.usersView.Layout(gtx)
	}

	if g.selectedBot < 0 || g.selectedBot >= len(g.bots) {
		return D{}
	}

	bot := g.bots[g.selectedBot]

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return g.layoutHeader(gtx, bot)
		}),
		layout.Flexed(1, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return g.layoutBotConfig(gtx, bot)
				}),
				layout.Flexed(1, func(gtx C) D {
					return g.layoutLogs(gtx)
				}),
			)
		}),
	)
}

func (g *GUI) layoutHeader(gtx C, bot *Bot) D {
	return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				label := material.H4(g.th, bot.config.Name)
				label.Color = accentColor
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if bot.runBtn.Clicked(gtx) {
					if bot.config.IsRunning {
						bot.cancel() // stop scraper
						bot.config.IsRunning = false
						g.w.Invalidate()
					} else {
						g.startBot(bot) // start scraper
					}
				}

				btnText := "▶ Start Bot"
				btnColor := successColor

				if bot.config.IsRunning {
					btnText = "■ Stop"
					btnColor = runningColor
				}

				btn := material.Button(g.th, &bot.runBtn, btnText)
				btn.Background = btnColor
				btn.Color = bgColor
				btn.CornerRadius = unit.Dp(8)

				if bot.config.IsRunning {
					gtx = gtx.Disabled()
				}

				return btn.Layout(gtx)
			}),
		)
	})
}

func (g *GUI) layoutBotConfig(gtx C, bot *Bot) D {
	return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(24), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
		return widget.Border{
			Color:        borderColor,
			Width:        unit.Dp(1),
			CornerRadius: unit.Dp(12),
		}.Layout(gtx, func(gtx C) D {
			defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, cardBg)

			return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return g.layoutFormRow(gtx, "🤖 Bot Name", &bot.nameEditor)
					}),
					layout.Rigid(func(gtx C) D {
						return g.layoutUserDropdown(gtx, bot)
					}),
					layout.Rigid(func(gtx C) D {
						return g.layoutFormRow(gtx, "🎟️ Event ID", &bot.eventIDEditor)
					}),
					layout.Rigid(func(gtx C) D {
						return g.layoutFormRow(gtx, "🎫 Ticket ID", &bot.ticketIDEditor)
					}),
					layout.Rigid(func(gtx C) D {
						return g.layoutFormRow(gtx, "📍 Area Filter", &bot.filterEditor)
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D {
								return g.layoutFormRow(gtx, "📊 Quantity", &bot.quantityEditor)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(20)}.Layout),
							layout.Flexed(1, func(gtx C) D {
								return g.layoutFormRow(gtx, "🎯 Max Tickets", &bot.maxTicketsEditor)
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									cb := material.CheckBox(g.th, &bot.loopCheckbox, "🔄 Enable Loop Mode")
									cb.Color = accentColor
									cb.IconColor = textColor
									return cb.Layout(gtx)
								}),
							)
						})
					}),
				)
			})
		})
	})
}

func (g *GUI) layoutFormRow(gtx C, label string, editor *widget.Editor) D {
	return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				l := material.Caption(g.th, label)
				l.Color = purpleAccent
				l.TextSize = unit.Sp(13)
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				ed := material.Editor(g.th, editor, "")
				ed.Color = textColor
				ed.HintColor = disabledColor
				return ed.Layout(gtx)
			}),
		)
	})
}

func (g *GUI) layoutLogs(gtx C) D {
	return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(24), Top: unit.Dp(16), Bottom: unit.Dp(24)}.Layout(gtx, func(gtx C) D {
		return widget.Border{
			Color:        borderColor,
			Width:        unit.Dp(1),
			CornerRadius: unit.Dp(12),
		}.Layout(gtx, func(gtx C) D {
			defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, cardBg)

			return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						label := material.Body1(g.th, "📋 LOGS")
						label.Color = accentColor
						label.TextSize = unit.Sp(14)
						return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, label.Layout)
					}),
					layout.Flexed(1, func(gtx C) D {
						return g.logView.Layout(gtx)
					}),
				)
			})
		})
	})
}

// BookingsView methods
func (bv *BookingsView) loadBookings() {
	bv.mu.Lock()
	defer bv.mu.Unlock()

	data, err := os.ReadFile("bookings.json")
	if err != nil {
		bv.bookings = []Booking{}
		return
	}

	var bookings []Booking
	if err := json.Unmarshal(data, &bookings); err != nil {
		bv.bookings = []Booking{}
		return
	}

	bv.bookings = bookings
	bv.deleteButtons = make([]widget.Clickable, len(bookings))
}

func (bv *BookingsView) saveBookings() {
	data, err := json.MarshalIndent(bv.bookings, "", "  ")
	if err != nil {
		log.Printf("Error marshalling bookings: %v", err)
		return
	}

	if err := os.WriteFile("bookings.json", data, 0644); err != nil {
		log.Printf("Error writing bookings: %v", err)
	}
}

func (uv *UsersView) loadUsers() {
	uv.mu.Lock()
	defer uv.mu.Unlock()

	data, err := os.ReadFile("users.json")
	if err != nil {
		uv.users = []User{}
		return
	}

	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		uv.users = []User{}
		return
	}

	uv.users = users
	// Reinitialize deleteButtons slice
	uv.deleteButtons = make([]widget.Clickable, len(users))
}

func (uv *UsersView) saveUsers() {
	data, err := json.MarshalIndent(uv.users, "", "  ")
	if err != nil {
		log.Printf("Error marshalling users: %v", err)
		return
	}

	if err := os.WriteFile("users.json", data, 0644); err != nil {
		log.Printf("Error writing users: %v", err)
	}
}

func (uv *UsersView) Layout(gtx C) D {
	uv.mu.Lock()
	defer uv.mu.Unlock()

	if uv.refreshBtn.Clicked(gtx) {
		uv.loadUsers()
		uv.gui.w.Invalidate()
	}

	if uv.deleteAllBtn.Clicked(gtx) {
		uv.users = []User{}
		uv.saveUsers()
		uv.gui.w.Invalidate()
	}

	// Handle individual delete buttons
	// Make sure deleteButtons is properly sized
	if len(uv.deleteButtons) != len(uv.users) {
		uv.deleteButtons = make([]widget.Clickable, len(uv.users))
	}

	for i := len(uv.deleteButtons) - 1; i >= 0; i-- {
		if uv.deleteButtons[i].Clicked(gtx) {
			uv.users = append(uv.users[:i], uv.users[i+1:]...)
			uv.saveUsers()
			uv.gui.w.Invalidate()
			break
		}
	}

	if uv.validateBtn.Clicked(gtx) {
		uv.validating = true
		go func() {
			username, err := services.GetUserName(uv.sidEditor.Text())
			if err != nil {
				log.Printf("Error validating user: %v", err)
				uv.gui.w.Invalidate()
				uv.validating = false
				return
			}
			uv.users = append(uv.users, User{
				SID:      uv.sidEditor.Text(),
				Username: username,
			})
			uv.saveUsers()
			uv.sidEditor.SetText("")
			uv.gui.w.Invalidate()
			uv.validating = false
		}()
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx C) D {
			return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						label := material.H4(uv.gui.th, fmt.Sprintf("👥 Users (%d)", len(uv.users)))
						label.Color = accentColor
						return label.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						btn := material.Button(uv.gui.th, &uv.refreshBtn, "🔄 Refresh")
						btn.Background = accentColor
						btn.Color = bgColor
						btn.CornerRadius = unit.Dp(8)
						return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, btn.Layout)
					}),
					layout.Rigid(func(gtx C) D {
						if len(uv.users) == 0 {
							return D{}
						}
						btn := material.Button(uv.gui.th, &uv.deleteAllBtn, "🗑️ Delete All")
						btn.Background = dangerColor
						btn.Color = bgColor
						btn.CornerRadius = unit.Dp(8)
						return btn.Layout(gtx)
					}),
				)
			})
		}),
		// Add user form
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(24), Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
				return widget.Border{
					Color:        borderColor,
					Width:        unit.Dp(1),
					CornerRadius: unit.Dp(12),
				}.Layout(gtx, func(gtx C) D {
					defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
					paint.Fill(gtx.Ops, cardBg)

					return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx C) D {
						btnText := "✔ Validate & Save"
						btnColor := successColor
						if uv.validating {
							btnText = "Validating..."
							btnColor = runningColor
							gtx = gtx.Disabled()
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return uv.gui.layoutFormRow(gtx, "🔑 SID Cookie", &uv.sidEditor)
							}),
							layout.Rigid(func(gtx C) D {
								btn := material.Button(uv.gui.th, &uv.validateBtn, btnText)
								btn.Background = btnColor
								btn.Color = bgColor
								btn.CornerRadius = unit.Dp(8)
								return btn.Layout(gtx)
							}),
						)
					})
				})
			})
		}),
		// Users List
		layout.Flexed(1, func(gtx C) D {
			return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(24), Bottom: unit.Dp(24)}.Layout(gtx, func(gtx C) D {
				if len(uv.users) == 0 {
					return uv.layoutEmptyState(gtx)
				}

				uv.list.Axis = layout.Vertical
				return material.List(uv.gui.th, &uv.list).Layout(gtx, len(uv.users), func(gtx C, i int) D {
					return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
						return uv.layoutUserCard(gtx, i)
					})
				})
			})
		}),
	)
}

func (uv *UsersView) layoutEmptyState(gtx C) D {
	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(12),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, cardBg)

		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					label := material.H6(uv.gui.th, "📭")
					label.TextSize = unit.Sp(48)
					return label.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D {
					label := material.Body1(uv.gui.th, "No users yet")
					label.Color = disabledColor
					return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, label.Layout)
				}),
			)
		})
	})
}

func (uv *UsersView) layoutUserCard(gtx C, index int) D {
	user := uv.users[index]

	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(12),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, cardBg)

		return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header with order number and delete button
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							label := material.H6(uv.gui.th, "👤 "+user.Username)
							label.Color = accentColor
							label.TextSize = unit.Sp(16)
							return label.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							btn := material.Button(uv.gui.th, &uv.deleteButtons[index], "🗑️ Delete")
							btn.Background = dangerColor
							btn.Color = bgColor
							btn.CornerRadius = unit.Dp(6)
							btn.TextSize = unit.Sp(12)
							return btn.Layout(gtx)
						}),
					)
				}),
				// Divider
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
						paint.FillShape(gtx.Ops, borderColor, clip.Rect{
							Max: image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(unit.Dp(1))},
						}.Op())
						return D{Size: image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(unit.Dp(1))}}
					})
				}),
				// User details
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return uv.gui.layoutInfoRow(gtx, "SID", user.SID)
						}),
					)
				}),
			)
		})
	})
}

func (bv *BookingsView) Layout(gtx C) D {
	bv.mu.Lock()
	defer bv.mu.Unlock()

	if bv.refreshBtn.Clicked(gtx) {
		bv.loadBookings()
		bv.gui.w.Invalidate()
	}

	if bv.deleteAllBtn.Clicked(gtx) {
		bv.bookings = []Booking{}
		bv.saveBookings()
		bv.gui.w.Invalidate()
	}

	// Handle individual delete buttons
	for i := range bv.deleteButtons {
		if bv.deleteButtons[i].Clicked(gtx) {
			bv.bookings = append(bv.bookings[:i], bv.bookings[i+1:]...)
			bv.deleteButtons = append(bv.deleteButtons[:i], bv.deleteButtons[i+1:]...)
			bv.saveBookings()
			bv.gui.w.Invalidate()
			break
		}
	}

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// Header
		layout.Rigid(func(gtx C) D {
			return layout.UniformInset(unit.Dp(24)).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						label := material.H4(bv.gui.th, fmt.Sprintf("🎫 Bookings (%d)", len(bv.bookings)))
						label.Color = accentColor
						return label.Layout(gtx)
					}),
					layout.Rigid(func(gtx C) D {
						btn := material.Button(bv.gui.th, &bv.refreshBtn, "🔄 Refresh")
						btn.Background = accentColor
						btn.Color = bgColor
						btn.CornerRadius = unit.Dp(8)
						return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, btn.Layout)
					}),
					layout.Rigid(func(gtx C) D {
						if len(bv.bookings) == 0 {
							return D{}
						}
						btn := material.Button(bv.gui.th, &bv.deleteAllBtn, "🗑️ Delete All")
						btn.Background = dangerColor
						btn.Color = bgColor
						btn.CornerRadius = unit.Dp(8)
						return btn.Layout(gtx)
					}),
				)
			})
		}),
		// Bookings List
		layout.Flexed(1, func(gtx C) D {
			return layout.Inset{Left: unit.Dp(24), Right: unit.Dp(24), Bottom: unit.Dp(24)}.Layout(gtx, func(gtx C) D {
				if len(bv.bookings) == 0 {
					return bv.layoutEmptyState(gtx)
				}

				bv.list.Axis = layout.Vertical
				return material.List(bv.gui.th, &bv.list).Layout(gtx, len(bv.bookings), func(gtx C, i int) D {
					return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
						return bv.layoutBookingCard(gtx, i)
					})
				})
			})
		}),
	)
}

func (bv *BookingsView) layoutEmptyState(gtx C) D {
	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(12),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, cardBg)

		return layout.Center.Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					label := material.H6(bv.gui.th, "📭")
					label.TextSize = unit.Sp(48)
					return label.Layout(gtx)
				}),
				layout.Rigid(func(gtx C) D {
					label := material.Body1(bv.gui.th, "No bookings yet")
					label.Color = disabledColor
					return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, label.Layout)
				}),
			)
		})
	})
}

func (bv *BookingsView) layoutBookingCard(gtx C, index int) D {
	booking := bv.bookings[index]

	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(12),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, cardBg)

		return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header with order number and delete button
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							label := material.H6(bv.gui.th, "🎟️ Order #"+booking.OrderNumber)
							label.Color = accentColor
							label.TextSize = unit.Sp(16)
							return label.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							btn := material.Button(bv.gui.th, &bv.deleteButtons[index], "🗑️ Delete")
							btn.Background = dangerColor
							btn.Color = bgColor
							btn.CornerRadius = unit.Dp(6)
							btn.TextSize = unit.Sp(12)
							return btn.Layout(gtx)
						}),
					)
				}),
				// Divider
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
						paint.FillShape(gtx.Ops, borderColor, clip.Rect{
							Max: image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(unit.Dp(1))},
						}.Op())
						return D{Size: image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(unit.Dp(1))}}
					})
				}),
				// Event details
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "User", booking.UserName)
						}),
						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "📅 Event", booking.EventName)
						}),

						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "🕐 Date", booking.EventDate)
						}),
						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "📍 Venue", booking.EventVenue)
						}),
						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "🎫 Section", booking.Section)
						}),
						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "💺 Seat", booking.SeatInfo)
						}),
						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "🎟️ Ticket", booking.TicketInfo)
						}),
						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "🔢 Quantity", booking.TicketQty)
						}),

						layout.Rigid(func(gtx C) D {
							return bv.gui.layoutInfoRow(gtx, "💵 Total", booking.Total)
						}),
					)
				}),
			)
		})
	})
}

func (g *GUI) layoutInfoRow(gtx C, label, value string) D {
	if value == "" {
		return D{}
	}

	return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				l := material.Body2(g.th, label)
				l.Color = purpleAccent
				l.TextSize = unit.Sp(13)
				return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(110))
					return l.Layout(gtx)
				})
			}),
			layout.Flexed(1, func(gtx C) D {
				v := material.Body2(g.th, value)
				v.Color = textColor
				v.TextSize = unit.Sp(13)
				return v.Layout(gtx)
			}),
		)
	})
}

func (g *GUI) startBot(bot *Bot) {
	ctx, cancel := context.WithCancel(context.Background())
	bot.cancel = cancel

	bot.config.IsRunning = true
	g.w.Invalidate()

	go func() {
		defer func() {
			bot.config.IsRunning = false
			g.w.Invalidate()
		}()

		logWriter := &BotLogWriter{gui: g, botName: bot.config.Name}
		log.SetOutput(logWriter)
		defer log.SetOutput(os.Stderr)

		cfg := services.ScraperConfig{
			BaseURL: "https://tixcraft.com/ticket/area",

			EventID:        bot.config.EventID,
			TicketID:       bot.config.TicketID,
			Filter:         bot.config.Filter,     // Filter for seat area
			PerOrderTicket: bot.config.Quantity,   // How many to buy at once
			MaxTickets:     bot.config.MaxTickets, // Total tickets you want
			SessionID:      bot.config.User.SID,
			Loop:           true, // Keep retrying until success
		}

		services.RunScraper(
			ctx, // << add ctx
			cfg,
		)
	}()
}

func (g *GUI) saveBots() {
	configs := make([]BotConfig, len(g.bots))
	for i, bot := range g.bots {
		configs[i] = BotConfig{
			ID:         bot.config.ID,
			Name:       bot.nameEditor.Text(),
			User:       bot.config.User,
			SID:        bot.config.SID,
			EventID:    bot.eventIDEditor.Text(),
			TicketID:   bot.ticketIDEditor.Text(),
			Filter:     bot.filterEditor.Text(),
			Quantity:   bot.quantityEditor.Text(),
			MaxTickets: bot.maxTicketsEditor.Text(),
			Loop:       bot.loopCheckbox.Value,
		}
	}

	data, _ := json.MarshalIndent(configs, "", "  ")
	os.WriteFile("bots_config.json", data, 0644)
}

func (g *GUI) loadBots() {
	data, err := os.ReadFile("bots_config.json")
	if err != nil {
		return
	}

	var configs []BotConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return
	}

	for _, cfg := range configs {
		bot := &Bot{
			config:           cfg,
			nameEditor:       widget.Editor{SingleLine: true},
			eventIDEditor:    widget.Editor{SingleLine: true},
			ticketIDEditor:   widget.Editor{SingleLine: true},
			filterEditor:     widget.Editor{SingleLine: true},
			quantityEditor:   widget.Editor{SingleLine: true},
			maxTicketsEditor: widget.Editor{SingleLine: true},
		}

		bot.nameEditor.SetText(cfg.Name)
		bot.eventIDEditor.SetText(cfg.EventID)
		bot.ticketIDEditor.SetText(cfg.TicketID)
		bot.filterEditor.SetText(cfg.Filter)
		bot.quantityEditor.SetText(cfg.Quantity)
		bot.maxTicketsEditor.SetText(cfg.MaxTickets)
		bot.loopCheckbox.Value = cfg.Loop

		// Initialize user dropdown selection
		for i, user := range g.usersView.users {
			if user.SID == cfg.User.SID {
				bot.userDropdown.selected = i
				break
			}
		}

		g.bots = append(g.bots, bot)
	}
}

func (g *GUI) layoutUserDropdown(gtx C, bot *Bot) D {
	return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				l := material.Caption(g.th, "👤 User")
				l.Color = purpleAccent
				l.TextSize = unit.Sp(13)
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				if len(g.usersView.users) == 0 {
					return material.Body2(g.th, "No users available. Add users in the Accounts tab.").Layout(gtx)
				}

				// Create a slice of user names for the dropdown
				userNames := make([]string, len(g.usersView.users))
				for i, u := range g.usersView.users {
					userNames[i] = u.Username
				}
				bot.userDropdown.Options = userNames

				// if a user is selected, update the bot config
				if bot.userDropdown.selected < len(g.usersView.users) {
					bot.config.User = g.usersView.users[bot.userDropdown.selected]
					bot.config.SID = g.usersView.users[bot.userDropdown.selected].SID
				}

				return bot.userDropdown.Layout(gtx, g.th)
			}),
		)
	})
}

type LogView struct {
	gui   *GUI
	list  widget.List
	logs  []string
	dirty bool
	mu    sync.Mutex
}

func (l *LogView) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.logs = append(l.logs, time.Now().Format("15:04:05")+" "+string(p))
	l.dirty = true
	if l.gui != nil && l.gui.w != nil {
		l.gui.w.Invalidate()
	}
	return len(p), nil
}

func (l *LogView) Layout(gtx C) D {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.dirty {
		l.list.Position.First = len(l.logs) - 1
		l.list.Position.Offset = 1000000
		l.dirty = false
	}

	l.list.Axis = layout.Vertical

	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(8),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, color.NRGBA{R: 18, G: 20, B: 28, A: 255})

		if len(l.logs) == 0 {
			return layout.Center.Layout(gtx, func(gtx C) D {
				label := material.Body2(l.gui.th, "No logs yet...")
				label.Color = disabledColor
				return label.Layout(gtx)
			})
		}

		return material.List(l.gui.th, &l.list).Layout(gtx, len(l.logs), func(gtx C, i int) D {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx C) D {
				label := material.Body2(l.gui.th, l.logs[i])
				label.Color = textColor
				label.TextSize = unit.Sp(12)
				return label.Layout(gtx)
			})
		})
	})
}

type BotLogWriter struct {
	gui     *GUI
	botName string
}

func (w *BotLogWriter) Write(p []byte) (n int, err error) {
	message := fmt.Sprintf("[%s] %s", w.botName, string(p))
	return w.gui.logView.Write([]byte(message))
}
