// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"log"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/maskraven/virt-viewer/pkg/spice"
)

// sessionUI owns the Fyne window, grab state, menus, and hotkey handling for
// one live SPICE client.
type sessionUI struct {
	app     fyne.App
	win     fyne.Window
	view    *guestView
	surface *Surface
	pad     *mousePad
	grab    Grab
	bind    Bindings

	client *spice.Client
	inputs *spice.Inputs
	chrome *controlChrome
	// statusStrip is the bottom compact bar; hidden in fullscreen.
	statusStrip *statusBar

	// statusAction is a transient message shown after the base status line
	// (e.g. "Sent Windows / Super"). Cleared by timer or overwritten.
	statusAction      string
	statusActionTimer *time.Timer

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
	ui.pad = pad
	ui.installMenus()
	chromeOverlay := ui.installChrome()

	// Guest fills the area; chrome overlays (does not shrink the display).
	// Bottom: compact single-line status strip (left-aligned).
	stage := container.NewStack(pad, chromeOverlay)
	ui.statusStrip = newStatusBar("Connecting…")
	content := container.NewBorder(nil, ui.statusStrip, nil, nil, stage)
	win.SetContent(content)
	win.Resize(fyne.NewSize(1024, 768))
	win.SetMaster()

	if startFullscreen {
		win.SetFullScreen(true)
		if ui.statusStrip != nil {
			ui.statusStrip.Hide()
		}
	}
	ui.wireKeys()
	ui.refreshStatus()
	// Default unpinned: brief peek then auto-hide.
	ui.peekChrome()
	return ui
}

// statusBar is a hairline + status line.
// Text is left-aligned and vertically middle of the strip (not stuck to the bottom).
type statusBar struct {
	widget.BaseWidget
	text    *canvas.Text
	sep     *canvas.Rectangle
	content *fyne.Container // left + vertical-middle around text
}

func newStatusBar(initial string) *statusBar {
	txt := canvas.NewText(initial, theme.Color(theme.ColorNameForeground))
	txt.TextSize = theme.Size(theme.SizeNameCaptionText)
	txt.Alignment = fyne.TextAlignLeading
	sep := canvas.NewRectangle(theme.Color(theme.ColorNameSeparator))
	// Place text at its natural MinSize, left + vertical middle of the content band.
	content := container.New(&leftVCenterLayout{padX: 8}, txt)
	s := &statusBar{text: txt, sep: sep, content: content}
	s.ExtendBaseWidget(s)
	return s
}

// SetLine updates the visible status string.
func (s *statusBar) SetLine(line string) {
	if s == nil || s.text == nil {
		return
	}
	s.text.Text = line
	s.Refresh()
}

func (s *statusBar) CreateRenderer() fyne.WidgetRenderer {
	s.ExtendBaseWidget(s)
	return &statusBarRenderer{bar: s, objects: []fyne.CanvasObject{s.sep, s.content}}
}

func (s *statusBar) MinSize() fyne.Size {
	s.ExtendBaseWidget(s)
	th := s.Theme()
	sepH := th.Size(theme.SizeNameSeparatorThickness)
	if sepH < 1 {
		sepH = 1
	}
	// Content band ≈ one caption line + equal air above/below for true middle.
	textH := th.Size(theme.SizeNameCaptionText)
	if s.text != nil {
		if mh := s.text.MinSize().Height; mh > textH {
			textH = mh
		}
	}
	padY := float32(6) // explicit vertical breathing room around the midline
	return fyne.NewSize(48, sepH+textH+padY*2)
}

// leftVCenterLayout places the child at natural height, left pad, vertical middle.
// Critical: do not stretch the child taller than MinSize — that parks canvas.Text
// on the bottom of a tall box.
type leftVCenterLayout struct {
	padX float32
}

func (l *leftVCenterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 || objects[0] == nil {
		return
	}
	o := objects[0]
	m := o.MinSize()
	h := m.Height
	if h <= 0 {
		h = theme.Size(theme.SizeNameCaptionText)
	}
	if h > size.Height {
		h = size.Height
	}
	// Vertical middle of the content area (equal space above and below).
	y := (size.Height - h) / 2
	if y < 0 {
		y = 0
	}
	// Width: use full band for long lines; height stays natural so glyphs stay middle.
	w := size.Width - l.padX
	if w < m.Width {
		// still allow shrink for narrow windows
		if w < 0 {
			w = 0
		}
	}
	o.Resize(fyne.NewSize(w, h))
	o.Move(fyne.NewPos(l.padX, y))
}

func (l *leftVCenterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	min := fyne.NewSize(l.padX, 0)
	for _, o := range objects {
		if o == nil {
			continue
		}
		m := o.MinSize()
		if m.Width+l.padX > min.Width {
			min.Width = m.Width + l.padX
		}
		if m.Height > min.Height {
			min.Height = m.Height
		}
	}
	return min
}

// statusBarRenderer: separator on top; content fills the rest (v-middle text).
type statusBarRenderer struct {
	bar     *statusBar
	objects []fyne.CanvasObject
}

func (r *statusBarRenderer) Destroy() {}
func (r *statusBarRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *statusBarRenderer) Layout(size fyne.Size) {
	th := r.bar.Theme()
	sepH := th.Size(theme.SizeNameSeparatorThickness)
	if sepH < 1 {
		sepH = 1
	}
	r.bar.sep.FillColor = theme.Color(theme.ColorNameSeparator)
	r.bar.sep.Resize(fyne.NewSize(size.Width, sepH))
	r.bar.sep.Move(fyne.NewPos(0, 0))

	if r.bar.text != nil {
		r.bar.text.Color = theme.Color(theme.ColorNameForeground)
		r.bar.text.TextSize = th.Size(theme.SizeNameCaptionText)
		r.bar.text.Alignment = fyne.TextAlignLeading
	}
	if r.bar.content != nil {
		// Content band under the hairline — layout centers text vertically here.
		r.bar.content.Resize(fyne.NewSize(size.Width, size.Height-sepH))
		r.bar.content.Move(fyne.NewPos(0, sepH))
	}
}

func (r *statusBarRenderer) MinSize() fyne.Size {
	return r.bar.MinSize()
}

func (r *statusBarRenderer) Refresh() {
	if r.bar.sep != nil {
		r.bar.sep.FillColor = theme.Color(theme.ColorNameSeparator)
		r.bar.sep.Refresh()
	}
	if r.bar.text != nil {
		r.bar.text.Color = theme.Color(theme.ColorNameForeground)
		r.bar.text.Refresh()
	}
	if r.bar.content != nil {
		r.bar.content.Refresh()
	}
	canvas.Refresh(r.bar)
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
	ui.refreshStatus()
}

func (ui *sessionUI) wireKeys() {
	// Prefer desktop.Canvas for raw key down/up (modifiers + letters).
	// Do not also wire TypedRune here — that double-fires printables on some OS.
	if dc, ok := ui.win.Canvas().(desktop.Canvas); ok {
		dc.SetOnKeyDown(ui.onKeyDown)
		dc.SetOnKeyUp(ui.onKeyUp)
		return
	}
	ui.win.Canvas().SetOnTypedKey(func(ev *fyne.KeyEvent) {
		ui.onKeyDown(ev)
	})
	// No desktop canvas: TypedRune is the only path for many letters.
	ui.win.Canvas().SetOnTypedRune(ui.onTypedRune)
}

func (ui *sessionUI) onTypedRune(r rune) {
	if ui.inputs == nil {
		return
	}
	if r < 0x20 {
		return
	}
	sc := letterScancode(r)
	if sc == 0 {
		sc = digitScancode(r)
	}
	if sc == 0 {
		return
	}
	if !ui.grab.Active() {
		ui.enterGrab()
	}
	// TypedRune is press-only; synthesize down/up for the guest.
	if err := ui.inputs.KeyDown(sc); err != nil {
		log.Printf("ui: key down (rune): %v", err)
		return
	}
	if err := ui.inputs.KeyUp(sc); err != nil {
		log.Printf("ui: key up (rune): %v", err)
	}
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
		case ActionToggleChrome:
			ui.toggleChromeVisible()
			return
		}
	}

	if !ui.grab.Active() {
		// First key while unfocused-grab: auto-grab so typing works without an
		// extra click (still click-to-grab for mouse-only paths).
		ui.enterGrab()
	}
	if sc := fyneKeyScancode(name); sc != 0 && ui.inputs != nil {
		if err := ui.inputs.KeyDown(sc); err != nil {
			log.Printf("ui: key down: %v", err)
		}
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
		if err := ui.inputs.KeyUp(sc); err != nil {
			log.Printf("ui: key up: %v", err)
		}
	}
}

func (ui *sessionUI) releaseGrab() {
	ui.grab.Release()
	if ui.pad != nil {
		ui.pad.resetMotion()
		ui.pad.Refresh()
	}
	ui.refreshStatus()
}

func (ui *sessionUI) enterGrab() {
	ui.grab.Grab()
	if ui.pad != nil {
		ui.pad.resetMotion()
		ui.pad.Refresh()
	}
	// Ensure the window receives key events after grab (macOS focus quirks).
	ui.win.Canvas().Focus(nil)
	ui.win.RequestFocus()
	ui.refreshStatus()
}

func (ui *sessionUI) toggleFullscreen() {
	ui.mu.Lock()
	ui.fs = !ui.fs
	fs := ui.fs
	ui.mu.Unlock()
	ui.win.SetFullScreen(fs)
	// Keep chrome pin across fullscreen; hide bottom status in FS for max area.
	if ui.statusStrip != nil {
		if fs {
			ui.statusStrip.Hide()
		} else {
			ui.statusStrip.Show()
		}
	}
	ui.refreshStatus()
}

func (ui *sessionUI) updateScaleHint() {
	if ui.win == nil || ui.view == nil {
		return
	}
	ui.view.SetScaleHint(ui.win.Canvas().Scale())
}

// resizeDebounce delays agent monitors-config so continuous window layout
// passes do not thrash the guest (looks like a frozen display).
var (
	resizeMu     sync.Mutex
	resizeTimer  *time.Timer
	lastResizeWH [2]uint32
)

// requestGuestResize asks the guest (via vdagent) to match the current pad
// size in logical pixels. Debounced 400ms; ignored when agent is offline.
func (ui *sessionUI) requestGuestResize() {
	if ui.client == nil || !ui.client.AgentActive() || ui.pad == nil {
		return
	}
	sz := ui.pad.Size()
	w, h := uint32(sz.Width+0.5), uint32(sz.Height+0.5)
	if w < 320 || h < 200 || w > 8192 || h > 8192 {
		return
	}
	resizeMu.Lock()
	defer resizeMu.Unlock()
	if lastResizeWH[0] == w && lastResizeWH[1] == h {
		return
	}
	if resizeTimer != nil {
		resizeTimer.Stop()
	}
	client := ui.client
	resizeTimer = time.AfterFunc(400*time.Millisecond, func() {
		resizeMu.Lock()
		lastResizeWH = [2]uint32{w, h}
		resizeMu.Unlock()
		if err := client.SetGuestDisplaySize(w, h); err != nil {
			log.Printf("ui: guest resize %dx%d: %v", w, h, err)
		}
	})
}

// mousePad is a desktop mouse-aware host for the guest view.
type mousePad struct {
	widget.BaseWidget
	ui   *sessionUI
	view *guestView

	// lastAbs tracks host absolute pointer for SERVER (relative) mouse mode.
	lastAbsX, lastAbsY float32
	hasLastAbs         bool
}

func newMousePad(ui *sessionUI, view *guestView) *mousePad {
	p := &mousePad{ui: ui, view: view}
	p.ExtendBaseWidget(p)
	return p
}

func (p *mousePad) resetMotion() {
	p.hasLastAbs = false
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
	// Debounce is natural (many resize events); agent ignores if unchanged.
	p.ui.requestGuestResize()
}

func (p *mousePad) Cursor() desktop.Cursor {
	if p.ui.grab.Active() {
		return desktop.HiddenCursor
	}
	return desktop.DefaultCursor
}

func (p *mousePad) MouseDown(ev *desktop.MouseEvent) {
	if ev != nil && p.ui.chromeBlocksClick(ev.Position) {
		// Host chrome hit zone / bar — do not grab or forward to guest.
		return
	}
	if !p.ui.grab.Active() {
		p.ui.enterGrab()
		// Seed position on grab so the next move has a relative baseline.
		if ev != nil {
			p.lastAbsX = ev.AbsolutePosition.X
			p.lastAbsY = ev.AbsolutePosition.Y
			p.hasLastAbs = true
			p.sendMousePosition(ev)
		}
		p.Refresh()
		return
	}
	if p.ui.inputs == nil || ev == nil {
		return
	}
	// Keep absolute position in sync before button events (CLIENT mode).
	p.sendMousePosition(ev)
	btn := mapMouseButton(ev.Button)
	if btn != 0 {
		if err := p.ui.inputs.MouseButton(btn, true); err != nil {
			log.Printf("ui: mouse button down: %v", err)
		}
	}
}

func (p *mousePad) MouseUp(ev *desktop.MouseEvent) {
	if ev != nil && p.ui.chromeBlocksClick(ev.Position) {
		return
	}
	if !p.ui.grab.Active() || p.ui.inputs == nil || ev == nil {
		return
	}
	btn := mapMouseButton(ev.Button)
	if btn != 0 {
		if err := p.ui.inputs.MouseButton(btn, false); err != nil {
			log.Printf("ui: mouse button up: %v", err)
		}
	}
}

func (p *mousePad) MouseMoved(ev *desktop.MouseEvent) {
	if ev == nil {
		return
	}
	// Chrome hit-zone reveal / auto-hide before any guest forward.
	// Works while grab is active (host chrome still reachable at top edge).
	if p.ui.handleChromePointer(ev.Position) {
		// Reset relative baseline so re-entering the guest does not jump.
		p.hasLastAbs = false
		return
	}
	if !p.ui.grab.Active() || p.ui.inputs == nil {
		return
	}
	if p.ui.inputs.ClientMouse() {
		p.sendMousePosition(ev)
		return
	}

	// SERVER mode: SPICE expects relative deltas, not absolute widget coords.
	ax, ay := ev.AbsolutePosition.X, ev.AbsolutePosition.Y
	if !p.hasLastAbs {
		p.lastAbsX, p.lastAbsY = ax, ay
		p.hasLastAbs = true
		return
	}
	dx := int32(ax - p.lastAbsX)
	dy := int32(ay - p.lastAbsY)
	p.lastAbsX, p.lastAbsY = ax, ay
	if dx == 0 && dy == 0 {
		return
	}
	if err := p.ui.inputs.MouseMove(dx, dy); err != nil {
		log.Printf("ui: mouse move: %v", err)
	}
}

// sendMousePosition maps the widget-local pointer into guest surface coords and
// injects an absolute MouseMove (CLIENT mode) or seeds position for later use.
func (p *mousePad) sendMousePosition(ev *desktop.MouseEvent) {
	if p.ui.inputs == nil || ev == nil {
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
	// Clamp local position into the pad.
	lx, ly := ev.Position.X, ev.Position.Y
	if lx < 0 {
		lx = 0
	}
	if ly < 0 {
		ly = 0
	}
	if lx > sz.Width {
		lx = sz.Width
	}
	if ly > sz.Height {
		ly = sz.Height
	}
	x := int32(float32(gw) * (lx / sz.Width))
	y := int32(float32(gh) * (ly / sz.Height))
	if x >= int32(gw) && gw > 0 {
		x = int32(gw) - 1
	}
	if y >= int32(gh) && gh > 0 {
		y = int32(gh) - 1
	}
	// Only send absolute messages when in CLIENT mode (MouseMove branches).
	if p.ui.inputs.ClientMouse() {
		if err := p.ui.inputs.MouseMove(x, y); err != nil {
			log.Printf("ui: mouse position: %v", err)
		}
	}
}

func (p *mousePad) MouseIn(*desktop.MouseEvent) {}
func (p *mousePad) MouseOut() {
	p.hasLastAbs = false
	// Leaving the pad (e.g. toward OS chrome) schedules hide unless pinned.
	p.ui.scheduleChromeHide()
}

func (p *mousePad) Scrolled(ev *fyne.ScrollEvent) {
	if !p.ui.grab.Active() || p.ui.inputs == nil || ev == nil {
		return
	}
	delta := int(ev.Scrolled.DY)
	if delta == 0 && ev.Scrolled.DX != 0 {
		delta = int(ev.Scrolled.DX)
	}
	if delta != 0 {
		if err := p.ui.inputs.MouseWheel(delta); err != nil {
			log.Printf("ui: mouse wheel: %v", err)
		}
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
