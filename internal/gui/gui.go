package gui

import (
	"image/color"
	"log"
	"os"
	"time"

	"tix-scraper/internal/services"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

type GUI struct {
	th *material.Theme

	currentPage Page
	sidPage     *SIDPage
	inputsPage  *InputsPage
	w           *app.Window
}

type Page interface {
	Layout(gtx C) D
}

func NewGUI() *GUI {
	g := &GUI{
		th: material.NewTheme(),
	}
	g.sidPage = NewSIDPage(g)
	g.inputsPage = NewInputsPage(g)
	g.currentPage = g.sidPage
	return g
}

func (g *GUI) Run() {
	g.w = new(app.Window)
	g.w.Option(
		app.Title("Tix Scraper"),
		app.Size(unit.Dp(400), unit.Dp(700)),
	)
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
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			g.currentPage.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (g *GUI) SwitchTo(page Page) {
	g.currentPage = page
	g.w.Invalidate()
}

type SIDPage struct {
	gui       *GUI
	sidEditor widget.Editor
	nextBtn   widget.Clickable
}

func NewSIDPage(g *GUI) *SIDPage {
	return &SIDPage{
		gui:       g,
		sidEditor: widget.Editor{SingleLine: true},
	}
}

func (p *SIDPage) Layout(gtx C) D {
	return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx C) D {
		return layout.Flex{
			Axis:    layout.Vertical,
			Spacing: layout.SpaceBetween,
		}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return material.H1(p.gui.th, "Tix Scraper").Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return material.Body1(p.gui.th, "Enter your tixcraft.com SID cookie value.").Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Rigid(func(gtx C) D {
						return material.Editor(p.gui.th, &p.sidEditor, "SID").Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx C) D {
				for p.nextBtn.Clicked(gtx) {
					p.gui.inputsPage.sid = p.sidEditor.Text()
					p.gui.SwitchTo(p.gui.inputsPage)
				}
				return material.Button(p.gui.th, &p.nextBtn, "Next").Layout(gtx)
			}),
		)
	})
}

type InputsPage struct {
	gui                *GUI
	sid                string
	eventIDEditor      widget.Editor
	ticketIDEditor     widget.Editor
	filterEditor       widget.Editor
	quantityEditor     widget.Editor
	runBtn             widget.Clickable
	logView            *LogView
	isScraping         bool
	scraperLogRedirect *LogRedirect
}

func NewInputsPage(g *GUI) *InputsPage {
	logView := &LogView{gui: g}
	scraperLogRedirect := NewLogRedirect(logView)
	return &InputsPage{
		gui:                g,
		eventIDEditor:      widget.Editor{SingleLine: true},
		ticketIDEditor:     widget.Editor{SingleLine: true},
		filterEditor:       widget.Editor{SingleLine: true},
		quantityEditor:     widget.Editor{SingleLine: true},
		logView:            logView,
		scraperLogRedirect: scraperLogRedirect,
	}
}

func (p *InputsPage) Layout(gtx C) D {
	for p.runBtn.Clicked(gtx) && !p.isScraping {
		p.isScraping = true
		go func() {
			log.SetOutput(p.scraperLogRedirect)
			defer func() {
				log.SetOutput(os.Stderr)
				p.isScraping = false
				p.gui.w.Invalidate()
			}()
			services.Scraper(
				"https://tixcraft.com/ticket/area",
				p.eventIDEditor.Text(),
				p.ticketIDEditor.Text(),
				p.filterEditor.Text(),
				p.quantityEditor.Text(),
				p.sid,
			)
		}()
	}

	return layout.UniformInset(unit.Dp(20)).Layout(gtx, func(gtx C) D {
		return layout.Flex{
			Axis:    layout.Vertical,
			Spacing: layout.SpaceBetween,
		}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return material.H1(p.gui.th, "Scraper Inputs").Layout(gtx)
			}),
			layout.Rigid(func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.Editor(p.gui.th, &p.eventIDEditor, "Event ID (e.g. 25_twicebus)").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Rigid(material.Editor(p.gui.th, &p.ticketIDEditor, "Ticket ID (e.g. 20210)").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Rigid(material.Editor(p.gui.th, &p.filterEditor, "Area Filter (e.g. A12)").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Rigid(material.Editor(p.gui.th, &p.quantityEditor, "Quantity (e.g. 1)").Layout),
				)
			}),
			layout.Rigid(func(gtx C) D {
				btn := material.Button(p.gui.th, &p.runBtn, "Run Scraper")
				if p.isScraping {
					btn.Text = "Scraping..."
				}
				return btn.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx C) D {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(material.H6(p.gui.th, "Logs").Layout),
					layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
					layout.Flexed(1, p.logView.Layout),
				)
			}),
		)
	})
}

type LogView struct {
	gui   *GUI
	list  widget.List
	logs  []string
	dirty bool
}

func (l *LogView) Write(p []byte) (n int, err error) {
	l.logs = append(l.logs, time.Now().Format("15:04:05 ")+string(p))
	l.dirty = true
	l.gui.w.Invalidate()
	return len(p), nil
}

func (l *LogView) Layout(gtx C) D {
	if l.dirty {
		l.list.Position.First = len(l.logs) - 1
		l.list.Position.Offset = 1000000
		l.dirty = false
	}
	l.list.Axis = layout.Vertical
	border := widget.Border{Color: color.NRGBA{A: 255}, CornerRadius: unit.Dp(3), Width: unit.Dp(1)}
	return border.Layout(gtx, func(gtx C) D {
		return material.List(l.gui.th, &l.list).Layout(gtx, len(l.logs), func(gtx C, i int) D {
			return layout.UniformInset(unit.Dp(5)).Layout(gtx, material.Body1(l.gui.th, l.logs[i]).Layout)
		})
	})
}

type LogRedirect struct {
	logView *LogView
}

func NewLogRedirect(logView *LogView) *LogRedirect {
	return &LogRedirect{logView: logView}
}

func (r *LogRedirect) Write(p []byte) (n int, err error) {
	return r.logView.Write(p)
}
