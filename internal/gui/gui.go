package gui

import (
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
	bgColor       = color.NRGBA{R: 26, G: 27, B: 38, A: 255}    // Dark background
	sidebarBg     = color.NRGBA{R: 31, G: 32, B: 45, A: 255}    // Sidebar background
	cardBg        = color.NRGBA{R: 40, G: 42, B: 54, A: 255}    // Card background
	borderColor   = color.NRGBA{R: 68, G: 71, B: 90, A: 255}    // Border color
	textColor     = color.NRGBA{R: 248, G: 248, B: 242, A: 255} // Light text
	accentColor   = color.NRGBA{R: 139, G: 233, B: 253, A: 255} // Cyan accent
	successColor  = color.NRGBA{R: 80, G: 250, B: 123, A: 255}  // Green
	runningColor  = color.NRGBA{R: 255, G: 184, B: 108, A: 255} // Orange
	disabledColor = color.NRGBA{R: 98, G: 114, B: 164, A: 255}  // Muted purple
)

type BotConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SID        string `json:"sid"`
	EventID    string `json:"event_id"`
	TicketID   string `json:"ticket_id"`
	Filter     string `json:"filter"`
	Quantity   string `json:"quantity"`
	MaxTickets string `json:"max_tickets"`
	Loop       bool   `json:"loop"`
	IsRunning  bool   `json:"-"`
}

type GUI struct {
	th          *material.Theme
	w           *app.Window
	bots        []*Bot
	selectedBot int
	addBotBtn   widget.Clickable
	logView     *LogView
	mu          sync.Mutex
}

type Bot struct {
	config    BotConfig
	selectBtn widget.Clickable
	deleteBtn widget.Clickable

	// Editors
	nameEditor       widget.Editor
	sidEditor        widget.Editor
	eventIDEditor    widget.Editor
	ticketIDEditor   widget.Editor
	filterEditor     widget.Editor
	quantityEditor   widget.Editor
	maxTicketsEditor widget.Editor
	loopCheckbox     widget.Bool

	runBtn widget.Clickable
}

func NewGUI() *GUI {
	th := material.NewTheme()
	th.Palette.Bg = bgColor
	th.Palette.Fg = textColor

	g := &GUI{
		th:          th,
		selectedBot: -1,
		logView:     &LogView{},
	}

	g.loadBots()

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
		sidEditor:        widget.Editor{SingleLine: true},
		eventIDEditor:    widget.Editor{SingleLine: true},
		ticketIDEditor:   widget.Editor{SingleLine: true},
		filterEditor:     widget.Editor{SingleLine: true},
		quantityEditor:   widget.Editor{SingleLine: true},
		maxTicketsEditor: widget.Editor{SingleLine: true},
	}

	bot.nameEditor.SetText(bot.config.Name)
	bot.sidEditor.SetText(bot.config.SID)
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
		app.Title("Tix Scraper - Multi Bot"),
		app.Size(unit.Dp(1000), unit.Dp(700)),
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
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(250))
	gtx.Constraints.Min.X = gtx.Constraints.Max.X

	// Draw sidebar background
	paint.FillShape(gtx.Ops, sidebarBg, clip.Rect{Max: gtx.Constraints.Max}.Op())

	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				label := material.H6(g.th, "BOTS")
				label.Color = accentColor
				return layout.Inset{Bottom: unit.Dp(16)}.Layout(gtx, label.Layout)
			}),
			layout.Flexed(1, func(gtx C) D {
				return g.layoutBotList(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if g.addBotBtn.Clicked(gtx) {
					g.addBot()
					g.saveBots()
					g.w.Invalidate()
				}

				btn := material.Button(g.th, &g.addBotBtn, "+ Add Bot")
				btn.Background = accentColor
				btn.Color = bgColor
				return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, btn.Layout)
			}),
		)
	})
}

func (g *GUI) layoutBotList(gtx C) D {
	list := &widget.List{
		List: layout.List{Axis: layout.Vertical},
	}

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

		return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
			return g.layoutBotCard(gtx, bot, i)
		})
	})
}

func (g *GUI) layoutBotCard(gtx C, bot *Bot, index int) D {
	isSelected := g.selectedBot == index

	bgCol := cardBg
	if isSelected {
		bgCol = color.NRGBA{R: 68, G: 71, B: 90, A: 255}
	}

	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(8),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, bgCol)

		return bot.selectBtn.Layout(gtx, func(gtx C) D {
			return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								label := material.Body1(g.th, bot.config.Name)
								label.Color = textColor
								label.TextSize = unit.Sp(14)
								return label.Layout(gtx)
							}),
							layout.Rigid(func(gtx C) D {
								if bot.config.IsRunning {
									status := material.Caption(g.th, "● Running")
									status.Color = successColor
									status.TextSize = unit.Sp(11)
									return status.Layout(gtx)
								}
								status := material.Caption(g.th, "○ Idle")
								status.Color = disabledColor
								status.TextSize = unit.Sp(11)
								return status.Layout(gtx)
							}),
						)
					}),
					layout.Rigid(func(gtx C) D {
						if len(g.bots) <= 1 {
							return D{}
						}

						btn := material.IconButton(g.th, &bot.deleteBtn, nil, "Delete")
						btn.Color = color.NRGBA{R: 255, G: 85, B: 85, A: 255}
						btn.Size = unit.Dp(20)
						return btn.Layout(gtx)
					}),
				)
			})
		})
	})
}

func (g *GUI) layoutMain(gtx C) D {
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
	return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Flexed(1, func(gtx C) D {
				label := material.H4(g.th, bot.config.Name)
				label.Color = accentColor
				return label.Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				if bot.runBtn.Clicked(gtx) && !bot.config.IsRunning {
					g.startBot(bot)
				}

				btnText := "Start Bot"
				btnColor := successColor

				if bot.config.IsRunning {
					btnText = "Running..."
					btnColor = runningColor
				}

				btn := material.Button(g.th, &bot.runBtn, btnText)
				btn.Background = btnColor
				btn.Color = bgColor

				if bot.config.IsRunning {
					gtx = gtx.Disabled()
				}

				return btn.Layout(gtx)
			}),
		)
	})
}

func (g *GUI) layoutBotConfig(gtx C, bot *Bot) D {
	return widget.Border{
		Color:        borderColor,
		Width:        unit.Dp(1),
		CornerRadius: unit.Dp(8),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, cardBg)

		return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx C) D {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return g.layoutFormRow(gtx, "Bot Name", &bot.nameEditor)
				}),
				layout.Rigid(func(gtx C) D {
					return g.layoutFormRow(gtx, "SID Cookie", &bot.sidEditor)
				}),
				layout.Rigid(func(gtx C) D {
					return g.layoutFormRow(gtx, "Event ID", &bot.eventIDEditor)
				}),
				layout.Rigid(func(gtx C) D {
					return g.layoutFormRow(gtx, "Ticket ID", &bot.ticketIDEditor)
				}),
				layout.Rigid(func(gtx C) D {
					return g.layoutFormRow(gtx, "Area Filter", &bot.filterEditor)
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Flexed(1, func(gtx C) D {
							return g.layoutFormRow(gtx, "Quantity", &bot.quantityEditor)
						}),
						layout.Rigid(layout.Spacer{Width: unit.Dp(16)}.Layout),
						layout.Flexed(1, func(gtx C) D {
							return g.layoutFormRow(gtx, "Max Tickets", &bot.maxTicketsEditor)
						}),
					)
				}),
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								cb := material.CheckBox(g.th, &bot.loopCheckbox, "Enable Loop Mode")
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
}

func (g *GUI) layoutFormRow(gtx C, label string, editor *widget.Editor) D {
	return layout.Inset{Bottom: unit.Dp(12)}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				l := material.Caption(g.th, label)
				l.Color = color.NRGBA{R: 189, G: 147, B: 249, A: 255}
				l.TextSize = unit.Sp(12)
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
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
	return layout.Inset{Top: unit.Dp(20)}.Layout(gtx, func(gtx C) D {
		return widget.Border{
			Color:        borderColor,
			Width:        unit.Dp(1),
			CornerRadius: unit.Dp(8),
		}.Layout(gtx, func(gtx C) D {
			defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, cardBg)

			return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						label := material.Body1(g.th, "LOGS")
						label.Color = accentColor
						label.TextSize = unit.Sp(13)
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

func (g *GUI) startBot(bot *Bot) {
	bot.config.Name = bot.nameEditor.Text()
	bot.config.SID = bot.sidEditor.Text()
	bot.config.EventID = bot.eventIDEditor.Text()
	bot.config.TicketID = bot.ticketIDEditor.Text()
	bot.config.Filter = bot.filterEditor.Text()
	bot.config.Quantity = bot.quantityEditor.Text()
	bot.config.MaxTickets = bot.maxTicketsEditor.Text()
	bot.config.Loop = bot.loopCheckbox.Value

	g.saveBots()

	bot.config.IsRunning = true
	g.w.Invalidate()

	go func() {
		defer func() {
			bot.config.IsRunning = false
			g.w.Invalidate()
		}()

		logWriter := &BotLogWriter{
			gui:     g,
			botName: bot.config.Name,
		}
		log.SetOutput(logWriter)
		defer log.SetOutput(os.Stderr)

		services.ScraperWithLoop(
			"https://tixcraft.com/ticket/area",
			bot.config.EventID,
			bot.config.TicketID,
			bot.config.Filter,
			bot.config.Quantity,
			bot.config.MaxTickets,
			bot.config.Loop,
			bot.config.SID,
		)
	}()
}

func (g *GUI) saveBots() {
	configs := make([]BotConfig, len(g.bots))
	for i, bot := range g.bots {
		configs[i] = BotConfig{
			ID:         bot.config.ID,
			Name:       bot.nameEditor.Text(),
			SID:        bot.sidEditor.Text(),
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
			sidEditor:        widget.Editor{SingleLine: true},
			eventIDEditor:    widget.Editor{SingleLine: true},
			ticketIDEditor:   widget.Editor{SingleLine: true},
			filterEditor:     widget.Editor{SingleLine: true},
			quantityEditor:   widget.Editor{SingleLine: true},
			maxTicketsEditor: widget.Editor{SingleLine: true},
		}

		bot.nameEditor.SetText(cfg.Name)
		bot.sidEditor.SetText(cfg.SID)
		bot.eventIDEditor.SetText(cfg.EventID)
		bot.ticketIDEditor.SetText(cfg.TicketID)
		bot.filterEditor.SetText(cfg.Filter)
		bot.quantityEditor.SetText(cfg.Quantity)
		bot.maxTicketsEditor.SetText(cfg.MaxTickets)
		bot.loopCheckbox.Value = cfg.Loop

		g.bots = append(g.bots, bot)
	}
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
		CornerRadius: unit.Dp(4),
	}.Layout(gtx, func(gtx C) D {
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(4))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, color.NRGBA{R: 20, G: 21, B: 30, A: 255})

		if len(l.logs) == 0 {
			return layout.Center.Layout(gtx, func(gtx C) D {
				label := material.Body2(l.gui.th, "No logs yet...")
				label.Color = disabledColor
				return label.Layout(gtx)
			})
		}

		return material.List(l.gui.th, &l.list).Layout(gtx, len(l.logs), func(gtx C, i int) D {
			return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx C) D {
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
