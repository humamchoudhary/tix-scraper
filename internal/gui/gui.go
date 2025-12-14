package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"io"
	"log"
	"os"
	"strings"
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
	// Modern color scheme optimized for readability
	bgColor       = color.NRGBA{R: 22, G: 24, B: 35, A: 255}
	sidebarBg     = color.NRGBA{R: 28, G: 30, B: 42, A: 255}
	cardBg        = color.NRGBA{R: 36, G: 39, B: 54, A: 255}
	borderColor   = color.NRGBA{R: 59, G: 66, B: 82, A: 255}
	textColor     = color.NRGBA{R: 229, G: 233, B: 240, A: 255}
	accentColor   = color.NRGBA{R: 136, G: 192, B: 208, A: 255}
	successColor  = color.NRGBA{R: 163, G: 190, B: 140, A: 255}
	runningColor  = color.NRGBA{R: 235, G: 203, B: 139, A: 255}
	dangerColor   = color.NRGBA{R: 191, G: 97, B: 106, A: 255}
	disabledColor = color.NRGBA{R: 129, G: 137, B: 153, A: 255}
	highlightBg   = color.NRGBA{R: 46, G: 52, B: 64, A: 255}
	purpleAccent  = color.NRGBA{R: 180, G: 142, B: 173, A: 255}
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
	IsRunning   bool   `json:"-"`
}

type User struct {
	SID      string `json:"sid"`
	Username string `json:"username"`
}

type GUI struct {
	th             *material.Theme
	w              *app.Window
	bots           []*Bot
	selectedBot    int
	addBotBtn      widget.Clickable
	saveAllBtn     widget.Clickable // Added save all button
	logView        *LogView
	usersView      *UsersView
	showUsers      bool
	botsTabBtn     widget.Clickable
	usersTabBtn    widget.Clickable
	mu             sync.Mutex
	isMobile       bool // Track if we're on a small screen
	mainScrollList widget.List
}

type Bot struct {
	config    BotConfig
	selectBtn widget.Clickable
	deleteBtn widget.Clickable

	// Editors
	nameEditor       TextField
	userDropdown     Dropdown
	eventIDEditor    TextField
	ticketIDEditor   TextField
	filterEditor     TextField
	quantityEditor   TextField
	maxTicketsEditor TextField
	preSaleEditor    TextField

	// Schedule widgets
	scheduleCheckbox widget.Bool
	dateEditor       TextField // Format: YYYY-MM-DD
	hourEditor       TextField // 00-23
	minuteEditor     TextField // 00-59

	loopCheckbox widget.Bool
	runBtn       widget.Clickable
	cancel       context.CancelFunc
}

// New TextField type to handle value extraction
type TextField struct {
	widget.Editor
	lastValue string
}

func (tf *TextField) Value() string {
	return tf.Editor.Text()
}

func (tf *TextField) SetValue(value string) {
	tf.Editor.SetText(value)
	tf.lastValue = value
}

func (tf *TextField) Changed() bool {
	return tf.Value() != tf.lastValue
}

func (tf *TextField) Update() {
	tf.lastValue = tf.Value()
}

type UsersView struct {
	gui           *GUI
	list          widget.List
	users         []User
	deleteButtons []widget.Clickable
	deleteAllBtn  widget.Clickable
	refreshBtn    widget.Clickable
	mu            sync.Mutex
	sidEditor     TextField
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
					return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx C) D {
						label := material.Body1(th, d.Options[d.selected])
						label.Color = textColor
						return label.Layout(gtx)
					})
				})
			})
		}),
		layout.Rigid(func(gtx C) D {
			if !d.isOpen {
				return D{}
			}

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

			rect := clip.Rect{Max: dims.Size}
			paint.FillShape(gtx.Ops, cardBg, rect.Op())
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
		th:          th,
		selectedBot: -1,
		logView:     &LogView{},
		usersView:   &UsersView{},
		showUsers:   false,
		mainScrollList: widget.List{
			List: layout.List{Axis: layout.Vertical},
		},
	}

	g.usersView.gui = g
	g.loadBots()
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
	}

	// Initialize text fields with proper values
	bot.nameEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.eventIDEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.ticketIDEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.filterEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.quantityEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.maxTicketsEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.preSaleEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.dateEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.hourEditor = TextField{Editor: widget.Editor{SingleLine: true}}
	bot.minuteEditor = TextField{Editor: widget.Editor{SingleLine: true}}

	bot.nameEditor.SetValue(bot.config.Name)
	bot.loopCheckbox.Value = bot.config.Loop
	bot.scheduleCheckbox.Value = bot.config.Schedule

	g.bots = append(g.bots, bot)
	g.selectedBot = len(g.bots) - 1
}

func (g *GUI) Run() {
	g.w = new(app.Window)

	// Set responsive window size
	g.w.Option(
		app.Title("Tix Scraper - Multi Bot & Accounts"),
		app.Size(unit.Dp(1000), unit.Dp(700)),   // Smaller default size
		app.MinSize(unit.Dp(400), unit.Dp(500)), // Minimum size for mobile
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

			// Detect mobile/small screen
			g.isMobile = gtx.Constraints.Max.X < 800

			g.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (g *GUI) Layout(gtx C) D {
	paint.Fill(gtx.Ops, bgColor)

	// Responsive layout - stack vertically on mobile
	if g.isMobile {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				// Mobile header with tabs
				return g.layoutMobileHeader(gtx)
			}),
			layout.Flexed(1, func(gtx C) D {
				return g.layoutMain(gtx)
			}),
		)
	}

	// Desktop layout
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return g.layoutSidebar(gtx)
		}),
		layout.Flexed(1, func(gtx C) D {
			return g.layoutMain(gtx)
		}),
	)
}

func (g *GUI) layoutMobileHeader(gtx C) D {
	return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				label := material.H6(g.th, "🎫 Tix Scraper")
				label.Color = accentColor
				return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, label.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				return g.layoutTabButtons(gtx)
			}),
		)
	})
}

func (g *GUI) layoutSidebar(gtx C) D {
	sidebarWidth := unit.Dp(280)
	if g.isMobile {
		sidebarWidth = unit.Dp(200)
	}

	gtx.Constraints.Max.X = gtx.Dp(sidebarWidth)
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	paint.FillShape(gtx.Ops, sidebarBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// App Title
			layout.Rigid(func(gtx C) D {
				label := material.H6(g.th, "🎫 Tix Scraper")
				label.Color = accentColor
				return layout.Inset{Bottom: unit.Dp(20)}.Layout(gtx, label.Layout)
			}),
			// Tab Buttons
			layout.Rigid(func(gtx C) D {
				return g.layoutTabButtons(gtx)
			}),
			// Content based on selected tab
			layout.Flexed(1, func(gtx C) D {
				if g.showUsers {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx C) D {
							label := material.Caption(g.th, fmt.Sprintf("%d Users", len(g.usersView.users)))
							label.Color = disabledColor
							return layout.Inset{Top: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, label.Layout)
						}),
					)
				}
				return g.layoutBotList(gtx)
			}),
			// Action Buttons
			layout.Rigid(func(gtx C) D {
				if g.showUsers {
					return D{}
				}

				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						if g.saveAllBtn.Clicked(gtx) {
							g.saveBots()
							g.logView.Write([]byte("💾 All bots saved successfully\n"))
							g.w.Invalidate()
						}

						btn := material.Button(g.th, &g.saveAllBtn, "💾 Save All Bots")
						btn.Background = successColor
						btn.Color = bgColor
						btn.CornerRadius = unit.Dp(8)
						btn.TextSize = unit.Sp(14)
						return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, btn.Layout)
					}),
					layout.Rigid(func(gtx C) D {
						if g.addBotBtn.Clicked(gtx) {
							g.addBot()
							g.logView.Write([]byte("🤖 New bot added\n"))
							g.w.Invalidate()
						}

						btn := material.Button(g.th, &g.addBotBtn, "✚ Add Bot")
						btn.Background = accentColor
						btn.Color = bgColor
						btn.CornerRadius = unit.Dp(8)
						btn.TextSize = unit.Sp(14)
						return btn.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func (g *GUI) layoutTabButtons(gtx C) D {
	if g.botsTabBtn.Clicked(gtx) {
		g.showUsers = false
		g.w.Invalidate()
	}

	if g.usersTabBtn.Clicked(gtx) {
		g.showUsers = true
		g.usersView.loadUsers()
		g.w.Invalidate()
	}

	minHeight := gtx.Dp(36)
	if g.isMobile {
		minHeight = gtx.Dp(32)
	}

	layoutTab := func(btn *widget.Clickable, labelText string, selected bool) D {
		bg := cardBg
		txtColor := disabledColor
		if selected {
			bg = accentColor
			txtColor = bgColor
		}

		return btn.Layout(gtx, func(gtx C) D {
			gtx.Constraints.Min.Y = minHeight

			if g.isMobile {
				gtx.Constraints.Min.X = gtx.Dp(80)
				gtx.Constraints.Max.X = gtx.Dp(80)
			} else {
				gtx.Constraints.Min.X = gtx.Dp(100)
				gtx.Constraints.Max.X = gtx.Dp(100)
			}

			rect := image.Rectangle{Max: gtx.Constraints.Max}
			defer clip.UniformRRect(rect, gtx.Dp(8)).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, bg)

			return layout.Center.Layout(gtx, func(gtx C) D {
				label := material.Caption(g.th, labelText)
				label.Color = txtColor
				if g.isMobile {
					label.TextSize = unit.Sp(12)
				}
				return label.Layout(gtx)
			})
		})
	}

	return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Flexed(1, func(gtx C) D { return layoutTab(&g.botsTabBtn, "Bots", !g.showUsers) }),
		layout.Rigid(layout.Spacer{Width: unit.Dp(6)}.Layout),
		layout.Flexed(1, func(gtx C) D { return layoutTab(&g.usersTabBtn, "Accounts", g.showUsers) }),
	)
}

func (g *GUI) layoutBotList(gtx C) D {
	list := &widget.List{
		List: layout.List{Axis: layout.Vertical},
	}

	// Check for delete button clicks FIRST, before rendering
	var deletedIndex = -1
	for i := 0; i < len(g.bots); i++ {
		if g.bots[i].deleteBtn.Clicked(gtx) && len(g.bots) > 1 {
			deletedIndex = i
			break
		}
	}

	// Process deletion if needed
	if deletedIndex >= 0 {
		botName := g.bots[deletedIndex].config.Name

		// Cancel bot if running
		if g.bots[deletedIndex].cancel != nil {
			g.bots[deletedIndex].cancel()
		}

		// Remove bot
		g.bots = append(g.bots[:deletedIndex], g.bots[deletedIndex+1:]...)

		// Adjust selected bot index
		if g.selectedBot >= len(g.bots) {
			g.selectedBot = len(g.bots) - 1
		} else if g.selectedBot > deletedIndex {
			g.selectedBot--
		}

		g.saveBots()
		g.logView.Write([]byte(fmt.Sprintf("🗑️ Bot '%s' deleted\n", botName)))
		g.w.Invalidate()
		return D{}
	}

	// Check for select button clicks
	for i := 0; i < len(g.bots); i++ {
		if g.bots[i].selectBtn.Clicked(gtx) {
			g.selectedBot = i
			g.w.Invalidate()
		}
	}

	return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
		return material.List(g.th, list).Layout(gtx, len(g.bots), func(gtx C, i int) D {
			bot := g.bots[i]

			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
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

	minHeight := gtx.Dp(50)
	if g.isMobile {
		minHeight = gtx.Dp(45)
	}

	return widget.Border{
		Color:        borderCol,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(8),
	}.Layout(gtx, func(gtx C) D {
		rect := image.Rectangle{Max: image.Pt(gtx.Constraints.Max.X, minHeight)}
		defer clip.UniformRRect(rect, gtx.Dp(8)).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, bgCol)

		return layout.Stack{}.Layout(gtx,
			layout.Expanded(func(gtx C) D {
				return D{}
			}),
			layout.Stacked(func(gtx C) D {
				return bot.selectBtn.Layout(gtx, func(gtx C) D {
					padding := unit.Dp(12)
					if g.isMobile {
						padding = unit.Dp(8)
					}

					return layout.UniformInset(padding).Layout(gtx, func(gtx C) D {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx C) D {
										label := material.Body2(g.th, bot.nameEditor.Value())
										label.Color = textColor
										if g.isMobile {
											label.TextSize = unit.Sp(13)
										}
										return label.Layout(gtx)
									}),
									layout.Rigid(func(gtx C) D {
										return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx C) D {
											if bot.config.IsRunning {
												status := material.Caption(g.th, "● Running")
												status.Color = successColor
												status.TextSize = unit.Sp(10)
												return status.Layout(gtx)
											}
											status := material.Caption(g.th, "○ Idle")
											status.Color = disabledColor
											status.TextSize = unit.Sp(10)
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
									CornerRadius: unit.Dp(4),
								}.Layout(gtx, func(gtx C) D {
									defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(4)).Push(gtx.Ops).Pop()
									return bot.deleteBtn.Layout(gtx, func(gtx C) D {
										padding := unit.Dp(4)
										if g.isMobile {
											padding = unit.Dp(3)
										}
										return layout.UniformInset(padding).Layout(gtx, func(gtx C) D {
											label := material.Caption(g.th, "✕")
											label.Color = dangerColor
											label.TextSize = unit.Sp(10)
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
	if g.showUsers {
		return g.usersView.Layout(gtx)
	}

	if g.selectedBot < 0 || g.selectedBot >= len(g.bots) {
		return D{}
	}

	bot := g.bots[g.selectedBot]

	return material.List(g.th, &g.mainScrollList).Layout(gtx, 1, func(gtx C, i int) D {
		// Set a minimum height for the inner content
		gtx.Constraints.Max.Y = gtx.Dp(1100) // Content will be at least 800dp tall

		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return g.layoutHeader(gtx, bot)
			}),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						// Bot config section with minimum height
						gtx.Constraints.Min.Y = gtx.Dp(400)
						return g.layoutBotConfig(gtx, bot)
					}),
					layout.Flexed(1, func(gtx C) D {
						// Logs section with minimum height
						gtx.Constraints.Min.Y = gtx.Dp(300)
						return g.layoutLogs(gtx)
					}),
				)
			}),
		)
	})
}

func (g *GUI) layoutHeader(gtx C, bot *Bot) D {
	padding := unit.Dp(20)
	if g.isMobile {
		padding = unit.Dp(16)
	}

	return layout.UniformInset(padding).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				label := material.H5(g.th, bot.nameEditor.Value())
				label.Color = accentColor
				if g.isMobile {
					label.TextSize = unit.Sp(18)
				}
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if bot.runBtn.Clicked(gtx) {
					if bot.config.IsRunning {
						bot.cancel()
						bot.config.IsRunning = false
						g.logView.Write([]byte(fmt.Sprintf("⏹️ Bot '%s' stopped\n", bot.nameEditor.Value())))
						g.w.Invalidate()
					} else {
						g.startBot(bot)
					}
				}

				btnText := "▶ Start"
				btnColor := successColor

				if bot.config.IsRunning {
					btnText = "■ Stop"
					btnColor = runningColor
				}

				btn := material.Button(g.th, &bot.runBtn, btnText)
				btn.Background = btnColor
				btn.Color = bgColor
				btn.CornerRadius = unit.Dp(8)
				if g.isMobile {
					btn.TextSize = unit.Sp(13)
				}

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
						return g.layoutFormRow(gtx, "🔑 Pre-Sale Code", &bot.preSaleEditor)
					}),
					layout.Rigid(func(gtx C) D {
						return g.layoutFormRow(gtx, "📍 Area Filter", &bot.filterEditor)
					}),
					layout.Rigid(func(gtx C) D {
						if g.isMobile {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									return g.layoutFormRow(gtx, "📊 Quantity", &bot.quantityEditor)
								}),
								layout.Rigid(func(gtx C) D {
									return g.layoutFormRow(gtx, "🎯 Max Tickets", &bot.maxTicketsEditor)
								}),
							)
						}

						return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
							layout.Flexed(1, func(gtx C) D {
								return g.layoutFormRow(gtx, "📊 Quantity", &bot.quantityEditor)
							}),
							layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
							layout.Flexed(1, func(gtx C) D {
								return g.layoutFormRow(gtx, "🎯 Max Tickets", &bot.maxTicketsEditor)
							}),
						)
					}),
					// SCHEDULER SECTION
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
							return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
								layout.Rigid(func(gtx C) D {
									cb := material.CheckBox(g.th, &bot.scheduleCheckbox, "⏰ Schedule Start")
									cb.Color = accentColor
									cb.IconColor = textColor
									if g.isMobile {
										cb.TextSize = unit.Sp(13)
									}
									return cb.Layout(gtx)
								}),
							)
						})
					}),
					layout.Rigid(func(gtx C) D {
						if !bot.scheduleCheckbox.Value {
							return D{}
						}
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return g.layoutFormRow(gtx, "📅 Date (YYYY-MM-DD)", &bot.dateEditor)
							}),
							layout.Rigid(func(gtx C) D {
								if g.isMobile {
									return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
										layout.Rigid(func(gtx C) D {
											return g.layoutFormRow(gtx, "🕐 Hour (00-23)", &bot.hourEditor)
										}),
										layout.Rigid(func(gtx C) D {
											return g.layoutFormRow(gtx, "📋 Minute (00-59)", &bot.minuteEditor)
										}),
									)
								}
								return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
									layout.Flexed(1, func(gtx C) D {
										return g.layoutFormRow(gtx, "🕐 Hour (00-23)", &bot.hourEditor)
									}),
									layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
									layout.Flexed(1, func(gtx C) D {
										return g.layoutFormRow(gtx, "📋 Minute (00-59)", &bot.minuteEditor)
									}),
								)
							}),
							layout.Rigid(func(gtx C) D {
								// Show the calculated scheduled time
								scheduledTime := g.calculateScheduledTime(bot)
								if scheduledTime != "" {
									return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
										label := material.Caption(g.th, "⏰ Scheduled for: "+scheduledTime)
										label.Color = successColor
										return label.Layout(gtx)
									})
								}
								return D{}
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx C) D {
							cb := material.CheckBox(g.th, &bot.loopCheckbox, "🔄 Enable Loop Mode")
							cb.Color = accentColor
							cb.IconColor = textColor
							if g.isMobile {
								cb.TextSize = unit.Sp(13)
							}
							return cb.Layout(gtx)
						})
					}),
				)
			})
		})
	})
}

// Helper function to calculate and display scheduled time
func (g *GUI) calculateScheduledTime(bot *Bot) string {
	if !bot.scheduleCheckbox.Value {
		return ""
	}

	dateStr := bot.dateEditor.Value()
	hourStr := bot.hourEditor.Value()
	minuteStr := bot.minuteEditor.Value()

	if dateStr == "" || hourStr == "" || minuteStr == "" {
		return ""
	}

	// Always use local system time
	loc := time.Local

	// Parse the scheduled datetime in local time
	scheduled, err := time.ParseInLocation("2006-01-02 15:04", fmt.Sprintf("%s %s:%s", dateStr, hourStr, minuteStr), loc)
	if err != nil {
		return "Invalid date/time"
	}

	return scheduled.Format("2006-01-02 15:04:05 Local Time")
}

func (g *GUI) layoutFormRow(gtx C, label string, field *TextField) D {
	return layout.Inset{Bottom: unit.Dp(14)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				l := material.Caption(g.th, label)
				l.Color = purpleAccent
				if g.isMobile {
					l.TextSize = unit.Sp(12)
				} else {
					l.TextSize = unit.Sp(13)
				}
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx C) D {
				ed := material.Editor(g.th, &field.Editor, "")
				ed.Color = textColor
				ed.HintColor = disabledColor
				if g.isMobile {
					ed.TextSize = unit.Sp(14)
				}
				return ed.Layout(gtx)
			}),
		)
	})
}

func (g *GUI) layoutLogs(gtx C) D {
	horizontalPadding := unit.Dp(20)
	if g.isMobile {
		horizontalPadding = unit.Dp(16)
	}

	return layout.Inset{Left: horizontalPadding, Right: horizontalPadding, Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
		return widget.Border{
			Color:        borderColor,
			Width:        unit.Dp(1),
			CornerRadius: unit.Dp(10),
		}.Layout(gtx, func(gtx C) D {
			defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(10)).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, cardBg)

			innerPadding := unit.Dp(16)
			if g.isMobile {
				innerPadding = unit.Dp(12)
			}

			return layout.UniformInset(innerPadding).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						label := material.Body2(g.th, "📋 LOGS")
						label.Color = accentColor
						if g.isMobile {
							label.TextSize = unit.Sp(13)
						}
						return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, label.Layout)
					}),
					layout.Flexed(1, func(gtx C) D {
						return g.logView.Layout(gtx)
					}),
				)
			})
		})
	})
}

// Updated startBot function to extract values from input fields
func (g *GUI) startBot(bot *Bot) {
	ctx, cancel := context.WithCancel(context.Background())
	bot.cancel = cancel

	// Update bot config with current field values before starting
	bot.config.Name = bot.nameEditor.Value()
	bot.config.EventID = bot.eventIDEditor.Value()
	bot.config.TicketID = bot.ticketIDEditor.Value()
	bot.config.Filter = bot.filterEditor.Value()
	bot.config.Quantity = bot.quantityEditor.Value()
	bot.config.MaxTickets = bot.maxTicketsEditor.Value()
	bot.config.PreSaleCode = bot.preSaleEditor.Value()
	bot.config.Schedule = bot.scheduleCheckbox.Value
	bot.config.StartDate = bot.dateEditor.Value()
	bot.config.StartTime = fmt.Sprintf("%s:%s", bot.hourEditor.Value(), bot.minuteEditor.Value())
	bot.config.Loop = bot.loopCheckbox.Value

	// Update user selection
	if bot.userDropdown.selected < len(g.usersView.users) {
		bot.config.User = g.usersView.users[bot.userDropdown.selected]
		bot.config.SID = g.usersView.users[bot.userDropdown.selected].SID
	}

	bot.config.IsRunning = true
	g.w.Invalidate()

	go func() {
		defer func() {
			bot.config.IsRunning = false
			g.w.Invalidate()
			g.logView.Write([]byte("🛑 Bot stopped\n"))
		}()

		// Validate inputs
		if bot.config.SID == "" {
			g.logView.Write([]byte("❌ No user selected\n"))
			return
		}

		// Connect GUI log writer to services package
		services.SetGUIWriter(&BotLogWriter{
			gui:     g,
			botName: bot.config.Name,
		})

		// Handle scheduling
		if bot.config.Schedule {
			if err := waitForScheduledTime(ctx, bot.config.StartDate, bot.config.StartTime, bot.config.Name, g); err != nil {
				g.logView.Write([]byte(fmt.Sprintf("❌ Schedule error: %v\n", err)))
				return
			}
		}

		// Check if context was cancelled during waiting
		select {
		case <-ctx.Done():
			g.logView.Write([]byte("🛑 Bot cancelled before starting\n"))
			return
		default:
		}

		// Start the actual scraper
		logWriter := &BotLogWriter{gui: g, botName: bot.config.Name}

		// Set up multi-writer for both GUI and stdout
		multiWriter := io.MultiWriter(logWriter, os.Stderr)
		log.SetOutput(multiWriter)
		defer log.SetOutput(os.Stderr)

		cfg := services.ScraperConfig{
			BaseURL:        "https://tixcraft.com/ticket/area",
			EventID:        bot.config.EventID,
			TicketID:       bot.config.TicketID,
			Filter:         bot.config.Filter,
			PerOrderTicket: bot.config.Quantity,
			MaxTickets:     bot.config.MaxTickets,
			PreSaleCode:    bot.config.PreSaleCode,
			SessionID:      bot.config.SID,
			Loop:           bot.config.Loop,
		}

		g.logView.Write([]byte(fmt.Sprintf("🚀 Starting bot: %s\n", bot.config.Name)))
		services.RunScraper(ctx, cfg)
	}()
}

// waitForScheduledTime waits until the scheduled datetime
// waitForScheduledTime waits until the scheduled datetime
func waitForScheduledTime(ctx context.Context, startDate, startTime, botName string, gui *GUI) error {
	// Parse the scheduled datetime in local time
	scheduled, err := time.ParseInLocation("2006-01-02 15:04", fmt.Sprintf("%s %s", startDate, startTime), time.Local)
	if err != nil {
		return fmt.Errorf("invalid datetime format: %s %s (use YYYY-MM-DD and HH:MM format)", startDate, startTime)
	}

	now := time.Now()

	// If scheduled time is in the past, start immediately
	if scheduled.Before(now) {
		gui.logView.Write([]byte(fmt.Sprintf("⏰ Scheduled time %s has passed, starting immediately\n", scheduled.Format("2006-01-02 15:04"))))
		return nil
	}

	// Calculate wait duration
	waitDuration := scheduled.Sub(now)
	gui.logView.Write([]byte(fmt.Sprintf("⏰ Bot '%s' scheduled for %s (Local Time), waiting %v\n", botName, scheduled.Format("2006-01-02 15:04:05"), waitDuration)))

	// Create a timer that respects context cancellation
	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	select {
	case <-timer.C:
		gui.logView.Write([]byte(fmt.Sprintf("✅ Scheduled time reached, starting bot '%s'\n", botName)))
		return nil
	case <-ctx.Done():
		gui.logView.Write([]byte(fmt.Sprintf("🛑 Schedule cancelled for bot '%s'\n", botName)))
		return fmt.Errorf("schedule cancelled")
	}
}

// Updated saveBots to use current field values
func (g *GUI) saveBots() {
	configs := make([]BotConfig, len(g.bots))
	for i, bot := range g.bots {
		configs[i] = BotConfig{
			ID:          bot.config.ID,
			Name:        bot.nameEditor.Value(),
			User:        bot.config.User,
			SID:         bot.config.SID,
			EventID:     bot.eventIDEditor.Value(),
			TicketID:    bot.ticketIDEditor.Value(),
			Filter:      bot.filterEditor.Value(),
			Quantity:    bot.quantityEditor.Value(),
			MaxTickets:  bot.maxTicketsEditor.Value(),
			PreSaleCode: bot.preSaleEditor.Value(),
			Schedule:    bot.scheduleCheckbox.Value,
			StartDate:   bot.dateEditor.Value(),
			StartTime:   fmt.Sprintf("%s:%s", bot.hourEditor.Value(), bot.minuteEditor.Value()),
			Loop:        bot.loopCheckbox.Value,
		}
	}

	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		g.logView.Write([]byte(fmt.Sprintf("❌ Error marshalling bots: %v\n", err)))
		return
	}

	if err := os.WriteFile("data/bots_config.json", data, 0644); err != nil {
		g.logView.Write([]byte(fmt.Sprintf("❌ Error saving bots: %v\n", err)))
		return
	}

	g.logView.Write([]byte("💾 Bots saved successfully\n"))
}

func (g *GUI) loadBots() {
	data, err := os.ReadFile("data/bots_config.json")
	if err != nil {
		if !os.IsNotExist(err) {
			g.logView.Write([]byte(fmt.Sprintf("❌ Error loading bots: %v\n", err)))
		}
		return
	}

	var configs []BotConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		g.logView.Write([]byte(fmt.Sprintf("❌ Error parsing bots config: %v\n", err)))
		return
	}

	for _, cfg := range configs {
		bot := &Bot{
			config: cfg,
		}

		// Initialize text fields with values
		bot.nameEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.eventIDEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.ticketIDEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.filterEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.quantityEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.maxTicketsEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.preSaleEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.dateEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.hourEditor = TextField{Editor: widget.Editor{SingleLine: true}}
		bot.minuteEditor = TextField{Editor: widget.Editor{SingleLine: true}}

		bot.nameEditor.SetValue(cfg.Name)
		bot.eventIDEditor.SetValue(cfg.EventID)
		bot.ticketIDEditor.SetValue(cfg.TicketID)
		bot.filterEditor.SetValue(cfg.Filter)
		bot.quantityEditor.SetValue(cfg.Quantity)
		bot.maxTicketsEditor.SetValue(cfg.MaxTickets)
		bot.preSaleEditor.SetValue(cfg.PreSaleCode)
		bot.dateEditor.SetValue(cfg.StartDate)

		// Parse time components
		if cfg.StartTime != "" {
			parts := strings.Split(cfg.StartTime, ":")
			if len(parts) >= 2 {
				bot.hourEditor.SetValue(parts[0])
				bot.minuteEditor.SetValue(parts[1])
			}
		}

		bot.scheduleCheckbox.Value = cfg.Schedule
		bot.loopCheckbox.Value = cfg.Loop

		g.bots = append(g.bots, bot)
	}
}

// UsersView methods with FIXED file operations
func (uv *UsersView) loadUsers() {
	uv.mu.Lock()
	defer uv.mu.Unlock()

	uv.users = []User{}
	uv.deleteButtons = []widget.Clickable{}

	// Ensure data directory exists
	if err := os.MkdirAll("data", 0755); err != nil {
		uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error creating data directory: %v\n", err)))
		return
	}

	filename := "data/users.json"

	// Check if file exists first
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		// File doesn't exist, create empty array
		emptyData, _ := json.MarshalIndent([]User{}, "", "  ")
		if err := os.WriteFile(filename, emptyData, 0644); err != nil {
			uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error creating users file: %v\n", err)))
		}
		return
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error reading users file: %v\n", err)))
		return
	}

	// Check if file is empty
	if len(data) == 0 {
		return
	}

	var users []User
	if err := json.Unmarshal(data, &users); err != nil {
		// If file is corrupted, create a fresh one
		uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error parsing users file, creating fresh one: %v\n", err)))
		emptyData, _ := json.MarshalIndent([]User{}, "", "  ")
		if err := os.WriteFile(filename, emptyData, 0644); err != nil {
			uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error creating fresh users file: %v\n", err)))
		}
		return
	}

	uv.users = users
}

func (uv *UsersView) saveUsers() {
	uv.mu.Lock()
	defer uv.mu.Unlock()

	// Ensure data directory exists
	if err := os.MkdirAll("data", 0755); err != nil {
		uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error creating data directory: %v\n", err)))
		return
	}

	data, err := json.MarshalIndent(uv.users, "", "  ")
	if err != nil {
		uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error marshalling users: %v\n", err)))
		return
	}

	// Write to temporary file first
	tempFilename := "data/users.json.tmp"
	filename := "data/users.json"

	if err := os.WriteFile(tempFilename, data, 0644); err != nil {
		uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error writing temp users file: %v\n", err)))
		return
	}

	// Atomic rename
	if err := os.Rename(tempFilename, filename); err != nil {
		uv.gui.logView.Write([]byte(fmt.Sprintf("❌ Error renaming temp users file: %v\n", err)))
		// Try to clean up temp file
		os.Remove(tempFilename)
		return
	}

	uv.gui.logView.Write([]byte("✅ Users saved successfully\n"))
}

func (uv *UsersView) Layout(gtx C) D {
	// Handle refresh button (without lock)
	if uv.refreshBtn.Clicked(gtx) {
		uv.loadUsers()
		uv.gui.w.Invalidate()
		return D{}
	}

	// Handle delete all button (without lock)
	if uv.deleteAllBtn.Clicked(gtx) {
		uv.mu.Lock()
		uv.users = []User{}
		uv.deleteButtons = []widget.Clickable{}
		uv.mu.Unlock()
		uv.saveUsers()
		uv.gui.w.Invalidate()
		return D{}
	}

	// Ensure deleteButtons array matches users length
	uv.mu.Lock()
	if len(uv.deleteButtons) != len(uv.users) {
		uv.deleteButtons = make([]widget.Clickable, len(uv.users))
	}

	// Check for delete button clicks FIRST, before rendering
	var deletedIndex = -1
	for i := 0; i < len(uv.deleteButtons) && i < len(uv.users); i++ {
		if uv.deleteButtons[i].Clicked(gtx) {
			deletedIndex = i
			break
		}
	}
	uv.mu.Unlock()

	// Process deletion if needed (outside the lock)
	if deletedIndex >= 0 {
		uv.mu.Lock()
		// Double-check index is still valid
		if deletedIndex < len(uv.users) {
			username := uv.users[deletedIndex].Username

			// Remove user and button
			uv.users = append(uv.users[:deletedIndex], uv.users[deletedIndex+1:]...)
			uv.deleteButtons = append(uv.deleteButtons[:deletedIndex], uv.deleteButtons[deletedIndex+1:]...)

			uv.mu.Unlock()
			uv.saveUsers()
			uv.gui.logView.Write([]byte(fmt.Sprintf("🗑️ User '%s' deleted\n", username)))
		} else {
			uv.mu.Unlock()
		}
		uv.gui.w.Invalidate()
		return D{}
	}

	uv.mu.Lock()
	defer uv.mu.Unlock()

	// Handle validate button
	if uv.validateBtn.Clicked(gtx) && !uv.validating {
		sid := strings.TrimSpace(uv.sidEditor.Value())
		if sid == "" {
			uv.gui.logView.Write([]byte("❌ Please enter a SID\n"))
			return D{}
		}

		uv.validating = true
		uv.gui.w.Invalidate()

		go func() {
			defer func() {
				uv.validating = false
				uv.gui.w.Invalidate()
			}()

			username, err := services.GetUserName(sid)

			uv.mu.Lock()
			var message string
			var needsSave bool

			if err != nil {
				message = fmt.Sprintf("❌ Error validating user: %v\n", err)
			} else {
				userExists := false
				for _, user := range uv.users {
					if user.SID == sid {
						userExists = true
						break
					}
				}
				if !userExists {
					uv.users = append(uv.users, User{
						SID:      sid,
						Username: username,
					})
					uv.deleteButtons = append(uv.deleteButtons, widget.Clickable{})
					needsSave = true
					message = fmt.Sprintf("✅ User added: %s\n", username)
					uv.sidEditor.SetValue("")
				} else {
					message = "ℹ️ User already exists\n"
				}
			}
			uv.mu.Unlock()

			// Save AFTER unlocking
			if needsSave {
				uv.saveUsers()
			}

			// Write log and invalidate after unlock
			uv.gui.logView.Write([]byte(message))
			uv.gui.w.Invalidate()
		}()
		return D{}
	}

	// Also update deleteButtons array if it doesn't match users length
	if len(uv.deleteButtons) != len(uv.users) {
		uv.deleteButtons = make([]widget.Clickable, len(uv.users))
	}
	// Use a scrollable layout for users view
	list := &widget.List{
		List: layout.List{Axis: layout.Vertical},
	}

	return material.List(uv.gui.th, list).Layout(gtx, 1, func(gtx C, i int) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// Header
			layout.Rigid(func(gtx C) D {
				padding := unit.Dp(20)
				if uv.gui.isMobile {
					padding = unit.Dp(16)
				}
				return layout.UniformInset(padding).Layout(gtx, func(gtx C) D {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							label := material.H5(uv.gui.th, fmt.Sprintf("👥 Users (%d)", len(uv.users)))
							label.Color = accentColor
							if uv.gui.isMobile {
								label.TextSize = unit.Sp(18)
							}
							return label.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							btn := material.Button(uv.gui.th, &uv.refreshBtn, "🔄 Refresh")
							btn.Background = accentColor
							btn.Color = bgColor
							btn.CornerRadius = unit.Dp(8)
							if uv.gui.isMobile {
								btn.TextSize = unit.Sp(13)
							}
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
							if uv.gui.isMobile {
								btn.TextSize = unit.Sp(13)
							}
							return btn.Layout(gtx)
						}),
					)
				})
			}),
			// Add user form
			layout.Rigid(func(gtx C) D {
				horizontalPadding := unit.Dp(20)
				if uv.gui.isMobile {
					horizontalPadding = unit.Dp(16)
				}
				return layout.Inset{Left: horizontalPadding, Right: horizontalPadding, Bottom: unit.Dp(16)}.Layout(gtx, func(gtx C) D {
					return widget.Border{
						Color:        borderColor,
						Width:        unit.Dp(1),
						CornerRadius: unit.Dp(10),
					}.Layout(gtx, func(gtx C) D {
						defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(10)).Push(gtx.Ops).Pop()
						paint.Fill(gtx.Ops, cardBg)

						innerPadding := unit.Dp(20)
						if uv.gui.isMobile {
							innerPadding = unit.Dp(16)
						}

						return layout.UniformInset(innerPadding).Layout(gtx, func(gtx C) D {
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
									if uv.gui.isMobile {
										btn.TextSize = unit.Sp(13)
									}
									return btn.Layout(gtx)
								}),
							)
						})
					})
				})
			}),
			// Users List
			layout.Flexed(1, func(gtx C) D {
				horizontalPadding := unit.Dp(20)
				if uv.gui.isMobile {
					horizontalPadding = unit.Dp(16)
				}
				return layout.Inset{Left: horizontalPadding, Right: horizontalPadding, Bottom: unit.Dp(20)}.Layout(gtx, func(gtx C) D {
					if len(uv.users) == 0 {
						return uv.layoutEmptyState(gtx)
					}

					userList := &widget.List{
						List: layout.List{Axis: layout.Vertical},
					}
					return material.List(uv.gui.th, userList).Layout(gtx, len(uv.users), func(gtx C, i int) D {
						return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
							return uv.layoutUserCard(gtx, i)
						})
					})
				})
			}),
		)
	})
}

func (uv *UsersView) layoutEmptyState(gtx C) D {
	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(10),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(10)).Push(gtx.Ops).Pop()
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
	if index >= len(uv.users) {
		return D{}
	}

	user := uv.users[index]

	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(10),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(10)).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, cardBg)

		padding := unit.Dp(20)
		if uv.gui.isMobile {
			padding = unit.Dp(16)
		}

		return layout.UniformInset(padding).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				// Header with username and delete button
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							label := material.H6(uv.gui.th, "👤 "+user.Username)
							label.Color = accentColor
							if uv.gui.isMobile {
								label.TextSize = unit.Sp(16)
							}
							return label.Layout(gtx)
						}),
						layout.Rigid(func(gtx C) D {
							if index >= len(uv.deleteButtons) {
								return D{}
							}
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

func (g *GUI) layoutInfoRow(gtx C, label, value string) D {
	if value == "" {
		return D{}
	}

	return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				l := material.Body2(g.th, label)
				l.Color = purpleAccent
				if g.isMobile {
					l.TextSize = unit.Sp(12)
				} else {
					l.TextSize = unit.Sp(13)
				}
				return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Dp(unit.Dp(100))
					return l.Layout(gtx)
				})
			}),
			layout.Flexed(1, func(gtx C) D {
				v := material.Body2(g.th, value)
				v.Color = textColor
				if g.isMobile {
					v.TextSize = unit.Sp(12)
				} else {
					v.TextSize = unit.Sp(13)
				}
				return v.Layout(gtx)
			}),
		)
	})
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

				// Ensure selected index is valid
				if bot.userDropdown.selected >= len(g.usersView.users) {
					bot.userDropdown.selected = 0
				}

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
