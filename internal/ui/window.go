// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"log"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"github.com/maskraven/virt-viewer/pkg/spice"
)

// sessionUI owns the Fyne window, grab state, and hotkey handling for one
// live SPICE client.
type sessionUI struct {
	app     fyne.App
	win     fyne.Window
	view    *guestView
	surface *Surface
	grab    Grab
	bind    Bindings

	client *spice.Client
	inputs *spice.Inputs

	mu     sync.Mutex
	mods   uint8 // currently pressed modifiers (host)
	fs     bool
	closed bool
}

func newSessionUI(a fyne.App, surface *Surface, bind Bindings, title string, startFullscreen bool) *sessionUI {
	if title == "" {
		title = "remote-viewer"
	}
	view := newGuestView(surface)
	win := a.NewWindow(title)

	ui := &sessionUI{
		app:     a,
		win:     win,
		view:    view,
		surface: surface,
		bind:    bind,
		fs:      startFullscreen,
	}

	pad := newMousePad(ui, view)
	win.SetContent(container.NewStack(pad))
	win.Resize(fyne.NewSize(1024, 768))
	win.SetMaster()

	if startFullscreen {
		win.SetFullScreen(true)
	}
	ui.wireKeys()
	return ui
}

// AttachClient sets the live client/inputs after Connect.
func (ui *sessionUI) AttachClient(c *spice.Client) {
	ui.client = c
	if c != nil {
		ui.inputs = c.Inputs()
		if t := c.Title(); t != "" {
			ui.win.SetTitle(t)
		}
	}
}

func (ui *sessionUI) wireKeys() {
	// Prefer desktop.Canvas for raw key down/up (modifiers + non-printables).
	if dc, ok := ui.win.Canvas().(desktop.Canvas); ok {
		dc.SetOnKeyDown(ui.onKeyDown)
		dc.SetOnKeyUp(ui.onKeyUp)
		return
	}
	ui.win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		ui.onKeyDown(ev)
	})
}

func (ui *sessionUI) onKeyDown(ev *fyne.KeyEvent) {
	if ev == nil {
		return
	}
	name := ev.Name
	keyID := fyneKeyName(name)

	ui.mu.Lock()
	if isModifierKey(name) {
		ui.mods |= ModsFromKeyName(keyID)
	}
	mods := ui.mods
	ui.mu.Unlock()

	// Hotkeys work even when not grabbed (local client actions).
	if !isModifierKey(name) {
		switch ui.bind.Match(mods, keyID) {
		case ActionSecureAttention:
			if err := InjectCAD(ui.inputs); err != nil {
				log.Printf("ui: secure-attention CAD: %v", err)
			}
			return
		case ActionReleaseCursor:
			ui.releaseGrab()
			return
		case ActionToggleFullscreen:
			ui.toggleFullscreen()
			return
		}
	}

	if !ui.grab.Active() {
		return
	}
	if sc := fyneKeyScancode(name); sc != 0 && ui.inputs != nil {
		_ = ui.inputs.KeyDown(sc)
	}
}

func (ui *sessionUI) onKeyUp(ev *fyne.KeyEvent) {
	if ev == nil {
		return
	}
	name := ev.Name
	keyID := fyneKeyName(name)

	ui.mu.Lock()
	if isModifierKey(name) {
		ui.mods &^= ModsFromKeyName(keyID)
	}
	ui.mu.Unlock()

	if !ui.grab.Active() {
		return
	}
	if sc := fyneKeyScancode(name); sc != 0 && ui.inputs != nil {
		_ = ui.inputs.KeyUp(sc)
	}
}

func (ui *sessionUI) releaseGrab() {
	ui.grab.Release()
}

func (ui *sessionUI) enterGrab() {
	ui.grab.Grab()
}

func (ui *sessionUI) toggleFullscreen() {
	ui.mu.Lock()
	ui.fs = !ui.fs
	fs := ui.fs
	ui.mu.Unlock()
	ui.win.SetFullScreen(fs)
}

func (ui *sessionUI) updateScaleHint() {
	if ui.win == nil || ui.view == nil {
		return
	}
	ui.view.SetScaleHint(ui.win.Canvas().Scale())
}

// mousePad is a desktop mouse-aware host for the guest view.
type mousePad struct {
	widget.BaseWidget
	ui   *sessionUI
	view *guestView
}

func newMousePad(ui *sessionUI, view *guestView) *mousePad {
	p := &mousePad{ui: ui, view: view}
	p.ExtendBaseWidget(p)
	return p
}

func (p *mousePad) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.NewStack(p.view))
}

func (p *mousePad) MinSize() fyne.Size {
	return p.view.MinSize()
}

func (p *mousePad) Resize(size fyne.Size) {
	p.BaseWidget.Resize(size)
	p.ui.updateScaleHint()
}

func (p *mousePad) Cursor() desktop.Cursor {
	if p.ui.grab.Active() {
		return desktop.HiddenCursor
	}
	return desktop.DefaultCursor
}

func (p *mousePad) MouseDown(ev *desktop.MouseEvent) {
	if !p.ui.grab.Active() {
		p.ui.enterGrab()
		p.Refresh()
		return
	}
	if p.ui.inputs == nil || ev == nil {
		return
	}
	btn := mapMouseButton(ev.Button)
	if btn != 0 {
		_ = p.ui.inputs.MouseButton(btn, true)
	}
}

func (p *mousePad) MouseUp(ev *desktop.MouseEvent) {
	if !p.ui.grab.Active() || p.ui.inputs == nil || ev == nil {
		return
	}
	btn := mapMouseButton(ev.Button)
	if btn != 0 {
		_ = p.ui.inputs.MouseButton(btn, false)
	}
}

func (p *mousePad) MouseMoved(ev *desktop.MouseEvent) {
	if !p.ui.grab.Active() || p.ui.inputs == nil || ev == nil {
		return
	}
	gw, gh := p.ui.surface.Size()
	if gw <= 0 || gh <= 0 {
		return
	}
	sz := p.Size()
	if sz.Width <= 0 || sz.Height <= 0 {
		return
	}
	x := int32(float32(gw) * (ev.Position.X / sz.Width))
	y := int32(float32(gh) * (ev.Position.Y / sz.Height))
	_ = p.ui.inputs.MouseMove(x, y)
}

func (p *mousePad) MouseIn(*desktop.MouseEvent) {}
func (p *mousePad) MouseOut()                   {}

func (p *mousePad) Scrolled(ev *fyne.ScrollEvent) {
	if !p.ui.grab.Active() || p.ui.inputs == nil || ev == nil {
		return
	}
	delta := int(ev.Scrolled.DY)
	if delta == 0 && ev.Scrolled.DX != 0 {
		delta = int(ev.Scrolled.DX)
	}
	if delta != 0 {
		_ = p.ui.inputs.MouseWheel(delta)
	}
}

func mapMouseButton(b desktop.MouseButton) uint8 {
	// SPICE: 1=left, 2=middle, 3=right.
	switch b {
	case desktop.MouseButtonPrimary:
		return 1
	case desktop.MouseButtonTertiary:
		return 2
	case desktop.MouseButtonSecondary:
		return 3
	default:
		return 0
	}
}

var (
	_ desktop.Hoverable  = (*mousePad)(nil)
	_ desktop.Mouseable  = (*mousePad)(nil)
	_ desktop.Cursorable = (*mousePad)(nil)
	_ fyne.Scrollable    = (*mousePad)(nil)
)
