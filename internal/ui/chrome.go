// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"image/color"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Auto-hide control chrome (RDP-style top-center pill).
const (
	chromeHitZoneY  float32 = 10 // host-only strip at top of guest surface
	chromeTopMargin float32 = 4
	chromePeekDelay         = 2 * time.Second
	chromeHideDelay         = 1 * time.Second
)

// chromeTheme sizes the control pill a notch below default Fyne chrome and
// forces light-on-dark labels (white text/icons on the dark pill).
type chromeTheme struct {
	fyne.Theme
}

func (t chromeTheme) Size(n fyne.ThemeSizeName) float32 {
	base := t.Theme.Size(n)
	switch n {
	case theme.SizeNameText, theme.SizeNameCaptionText:
		return 12
	case theme.SizeNameInlineIcon:
		return 16
	case theme.SizeNameInnerPadding:
		return 4
	case theme.SizeNamePadding:
		return 4
	case theme.SizeNameInputBorder, theme.SizeNameInputRadius:
		return 3
	default:
		return base
	}
}

func (t chromeTheme) Color(n fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	// White labels/icons on the dark pill; soft hover/press fills.
	// Fyne buttons use ColorNameForeground for label text and ColorNameButton
	// for the face (MediumImportance). Keep faces transparent so the pill shows.
	switch n {
	case theme.ColorNameForeground,
		theme.ColorNamePrimary,
		theme.ColorNamePlaceHolder,
		theme.ColorNameHyperlink:
		return color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	case theme.ColorNameDisabled:
		// Still readable white-ish; not grey-on-black.
		return color.NRGBA{R: 230, G: 230, B: 235, A: 255}
	case theme.ColorNameButton, theme.ColorNameInputBackground, theme.ColorNameBackground:
		return color.NRGBA{R: 0, G: 0, B: 0, A: 0}
	case theme.ColorNameHover:
		return color.NRGBA{R: 70, G: 72, B: 82, A: 255}
	case theme.ColorNamePressed:
		return color.NRGBA{R: 50, G: 52, B: 62, A: 255}
	default:
		return t.Theme.Color(n, v)
	}
}

// inChromeHitZone reports whether a pad-local Y is inside the top reveal strip.
func inChromeHitZone(y float32) bool {
	return y <= chromeHitZoneY
}

// topCenterLayout places children at horizontal center, topMargin from the top.
type topCenterLayout struct {
	topMargin float32
}

func (l topCenterLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		m := o.MinSize()
		x := (size.Width - m.Width) / 2
		if x < 0 {
			x = 0
		}
		o.Resize(m)
		o.Move(fyne.NewPos(x, l.topMargin))
	}
}

func (l topCenterLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	min := fyne.NewSize(0, 0)
	for _, o := range objects {
		if o == nil {
			continue
		}
		m := o.MinSize()
		if m.Width > min.Width {
			min.Width = m.Width
		}
		if m.Height > min.Height {
			min.Height = m.Height
		}
	}
	min.Height += l.topMargin
	return min
}

// controlChrome is the floating daily-use control bar overlaid on the guest.
type controlChrome struct {
	overlay fyne.CanvasObject // topCenter container (full area, non-blocking)
	pill    *chromePill

	pinBtn *widget.Button

	mu        sync.Mutex
	pinned    bool
	menuOpen  bool
	hideTimer *time.Timer
}

// chromePill is the visible pill; Hoverable so pointer-over cancels auto-hide.
type chromePill struct {
	widget.BaseWidget
	ui      *sessionUI
	content fyne.CanvasObject
}

func newChromePill(ui *sessionUI, content fyne.CanvasObject) *chromePill {
	p := &chromePill{ui: ui, content: content}
	p.ExtendBaseWidget(p)
	return p
}

func (p *chromePill) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(p.content)
}

func (p *chromePill) MinSize() fyne.Size {
	p.ExtendBaseWidget(p)
	if p.content == nil {
		return fyne.NewSize(0, 0)
	}
	return p.content.MinSize()
}

func (p *chromePill) MouseIn(*desktop.MouseEvent) {
	if p.ui != nil {
		p.ui.chromePointerEnter()
	}
}

func (p *chromePill) MouseMoved(*desktop.MouseEvent) {
	if p.ui != nil {
		p.ui.chromePointerEnter()
	}
}

func (p *chromePill) MouseOut() {
	if p.ui != nil {
		p.ui.chromePointerLeave()
	}
}

var _ desktop.Hoverable = (*chromePill)(nil)

// chromeIconBtn is a medium pill button with white themed labels (see chromeTheme).
func chromeIconBtn(label string, icon fyne.Resource, fn func()) *widget.Button {
	b := widget.NewButtonWithIcon(label, icon, fn)
	// MediumImportance: High is loud blue; Low greys out text on dark bg.
	b.Importance = widget.MediumImportance
	return b
}

// installChrome builds the overlay stack object and wires ui.chrome.
// Returns the overlay CanvasObject to place above the mouse pad in a Stack.
// CAD lives under Keys ▾ (and the secure-attention hotkey), not on the pill/More.
func (ui *sessionUI) installChrome() fyne.CanvasObject {
	// Pin: labeled + check icon (not window-restore/eye). Shows keep-bar-visible state.
	pinBtn := chromeIconBtn("Pin", theme.CheckButtonIcon(), func() {
		ui.toggleChromePin()
	})

	ungrabBtn := chromeIconBtn("Ungrab", theme.CancelIcon(), func() {
		ui.chromePointerEnter()
		ui.releaseGrab()
		ui.refreshStatus()
	})
	fsBtn := chromeIconBtn("Full", theme.ViewFullScreenIcon(), func() {
		ui.chromePointerEnter()
		ui.toggleFullscreen()
		ui.refreshStatus()
	})
	copyBtn := chromeIconBtn("Copy", theme.ContentCopyIcon(), func() {
		ui.chromePointerEnter()
		ui.copyFromGuest()
	})
	pasteBtn := chromeIconBtn("Paste", theme.ContentPasteIcon(), func() {
		ui.chromePointerEnter()
		ui.pasteToGuest()
	})
	typeBtn := chromeIconBtn("Type", theme.DocumentCreateIcon(), func() {
		ui.chromePointerEnter()
		ui.showTypeTextDialog()
	})

	var sendBtn *widget.Button
	sendBtn = chromeIconBtn("Keys", theme.MenuDropDownIcon(), func() {
		ui.showChromeSendKeysMenu(sendBtn)
	})
	// Icon-only More: MoreHorizontalIcon is already "…"; do not also set text "···".
	var moreBtn *widget.Button
	moreBtn = chromeIconBtn("", theme.MoreHorizontalIcon(), func() {
		ui.showChromeMoreMenu(moreBtn)
	})

	row := container.NewHBox(
		pinBtn,
		ungrabBtn,
		fsBtn,
		copyBtn,
		pasteBtn,
		typeBtn,
		sendBtn,
		moreBtn,
	)

	bg := canvas.NewRectangle(color.NRGBA{R: 28, G: 28, B: 32, A: 230})
	bg.CornerRadius = 6
	bg.StrokeColor = color.NRGBA{R: 90, G: 90, B: 100, A: 180}
	bg.StrokeWidth = 1

	// Slightly roomier pad + white-on-dark theme.
	padded := container.NewPadded(row)
	pillBody := container.NewStack(bg, padded)
	themed := container.NewThemeOverride(pillBody, chromeTheme{Theme: theme.Current()})

	pill := newChromePill(ui, themed)
	overlay := container.New(&topCenterLayout{topMargin: chromeTopMargin}, pill)

	ui.chrome = &controlChrome{
		overlay: overlay,
		pill:    pill,
		pinBtn:  pinBtn,
	}
	return overlay
}

// keysMenuTheme shrinks Keys popup text/padding (PopUpMenu uses default theme size).
type keysMenuTheme struct {
	fyne.Theme
}

func (t keysMenuTheme) Size(n fyne.ThemeSizeName) float32 {
	switch n {
	case theme.SizeNameText, theme.SizeNameCaptionText, theme.SizeNameHeadingText:
		return 11
	case theme.SizeNameInlineIcon:
		return 12
	case theme.SizeNameInnerPadding, theme.SizeNamePadding:
		return 3
	case theme.SizeNameScrollBar:
		return 8
	default:
		return t.Theme.Size(n)
	}
}

func (ui *sessionUI) showChromeSendKeysMenu(anchor fyne.CanvasObject) {
	if ui.win == nil {
		return
	}
	// Compact custom list: Fyne PopUpMenu text is oversized for a dense key list.
	var pop *widget.PopUp
	closePop := func() {
		if pop != nil {
			pop.Hide()
		}
		if ui.chrome != nil {
			ui.chrome.setMenuOpen(false)
			ui.scheduleChromeHide()
		}
	}

	rows := make([]fyne.CanvasObject, 0, len(StandardSendKeys())+2)
	for _, p := range StandardSendKeys() {
		preset := p
		btn := widget.NewButton(preset.Label, func() {
			closePop()
			ui.sendKeys(preset)
		})
		btn.Alignment = widget.ButtonAlignLeading
		btn.Importance = widget.LowImportance
		rows = append(rows, btn)
	}
	typeBtn := widget.NewButton("Type text…", func() {
		closePop()
		ui.showTypeTextDialog()
	})
	typeBtn.Alignment = widget.ButtonAlignLeading
	typeBtn.Importance = widget.LowImportance
	rows = append(rows, widget.NewSeparator(), typeBtn)

	list := container.NewVBox(rows...)
	// Cap height so a long list scrolls; keep width modest.
	scroll := container.NewVScroll(list)
	scroll.SetMinSize(fyne.NewSize(200, 280))

	bg := canvas.NewRectangle(color.NRGBA{R: 32, G: 32, B: 36, A: 245})
	bg.CornerRadius = 4
	bg.StrokeColor = color.NRGBA{R: 90, G: 90, B: 100, A: 200}
	bg.StrokeWidth = 1
	body := container.NewStack(bg, container.NewPadded(scroll))
	themed := container.NewThemeOverride(body, keysMenuTheme{Theme: theme.Current()})

	if ui.chrome != nil {
		ui.chrome.setMenuOpen(true)
		ui.chrome.cancelHide()
	}
	pop = widget.NewPopUp(themed, ui.win.Canvas())
	// Dismiss when clicking outside (Fyne PopUp).
	// Position under the Keys button.
	if anchor != nil {
		pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
		pop.ShowAtPosition(fyne.NewPos(pos.X, pos.Y+anchor.Size().Height+2))
	} else {
		pop.Show()
	}
}

func (ui *sessionUI) showChromeMoreMenu(anchor fyne.CanvasObject) {
	if ui.win == nil {
		return
	}
	// CAD is already first in Keys ▾ (StandardSendKeys); do not duplicate here.
	items := []*fyne.MenuItem{
		fyne.NewMenuItem("Grab keyboard & mouse", func() {
			ui.enterGrab()
			ui.refreshStatus()
		}),
		fyne.NewMenuItem("Keyboard shortcuts", func() {
			ui.showKeyboardShortcuts()
		}),
		fyne.NewMenuItem("About remote-viewer", func() {
			ui.showAbout()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() {
			ui.win.Close()
		}),
	}
	ui.showChromePopup(fyne.NewMenu("", items...), anchor)
}

func (ui *sessionUI) showChromePopup(menu *fyne.Menu, anchor fyne.CanvasObject) {
	if ui.chrome != nil {
		ui.chrome.setMenuOpen(true)
		ui.chrome.cancelHide()
	}
	pop := widget.NewPopUpMenu(menu, ui.win.Canvas())
	pop.OnDismiss = func() {
		pop.Hide()
		if ui.chrome != nil {
			ui.chrome.setMenuOpen(false)
			ui.scheduleChromeHide()
		}
	}
	if anchor != nil {
		pop.ShowAtRelativePosition(fyne.NewPos(0, anchor.Size().Height), anchor)
		return
	}
	pop.Show()
}

func (c *controlChrome) setMenuOpen(open bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.menuOpen = open
	c.mu.Unlock()
}

func (c *controlChrome) isPinned() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pinned
}

func (c *controlChrome) isVisible() bool {
	if c == nil || c.pill == nil {
		return false
	}
	return c.pill.Visible()
}

// interactionBottom is the pad-local Y below which the chrome strip ends.
func (c *controlChrome) interactionBottom() float32 {
	if c == nil || c.pill == nil {
		return chromeHitZoneY
	}
	return chromeTopMargin + c.pill.Size().Height + 2
}

// pointInRect reports whether pos is inside [origin, origin+size).
func pointInRect(pos, origin fyne.Position, size fyne.Size) bool {
	return pos.X >= origin.X && pos.X < origin.X+size.Width &&
		pos.Y >= origin.Y && pos.Y < origin.Y+size.Height
}

// blocksAt reports whether a pad-local point is over the visible pill.
func (c *controlChrome) blocksAt(pos fyne.Position) bool {
	if c == nil || c.pill == nil || !c.pill.Visible() {
		return false
	}
	return pointInRect(pos, c.pill.Position(), c.pill.Size())
}

func (c *controlChrome) cancelHide() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.hideTimer != nil {
		c.hideTimer.Stop()
		c.hideTimer = nil
	}
}

func (ui *sessionUI) chromePointerEnter() {
	if ui.chrome == nil {
		return
	}
	ui.chrome.cancelHide()
	ui.showChrome()
}

func (ui *sessionUI) chromePointerLeave() {
	ui.scheduleChromeHide()
}

// showChrome makes the pill visible (does not start a hide timer).
func (ui *sessionUI) showChrome() {
	if ui.chrome == nil || ui.chrome.pill == nil {
		return
	}
	ui.chrome.pill.Show()
	if ui.chrome.overlay != nil {
		ui.chrome.overlay.Refresh()
	}
}

// hideChrome hides the pill when allowed (not pinned, no open menu).
func (ui *sessionUI) hideChrome() {
	if ui.chrome == nil || ui.chrome.pill == nil {
		return
	}
	ui.chrome.mu.Lock()
	if ui.chrome.pinned || ui.chrome.menuOpen {
		ui.chrome.mu.Unlock()
		return
	}
	ui.chrome.mu.Unlock()
	ui.chrome.pill.Hide()
	if ui.chrome.overlay != nil {
		ui.chrome.overlay.Refresh()
	}
}

// revealChrome shows the bar and cancels any pending hide.
func (ui *sessionUI) revealChrome() {
	if ui.chrome == nil {
		return
	}
	ui.chrome.cancelHide()
	ui.showChrome()
}

// scheduleChromeHide starts (or restarts) the auto-hide timer when unpinned.
func (ui *sessionUI) scheduleChromeHide() {
	if ui.chrome == nil {
		return
	}
	ui.scheduleChromeHideAfter(chromeHideDelay)
}

func (ui *sessionUI) scheduleChromeHideAfter(d time.Duration) {
	if ui.chrome == nil {
		return
	}
	c := ui.chrome
	c.mu.Lock()
	if c.pinned || c.menuOpen {
		c.mu.Unlock()
		return
	}
	if c.hideTimer != nil {
		c.hideTimer.Stop()
	}
	c.hideTimer = time.AfterFunc(d, func() {
		// Timer fires off the UI thread; hide on Fyne thread.
		if fyne.CurrentApp() != nil {
			fyne.Do(func() {
				ui.hideChrome()
			})
			return
		}
		ui.hideChrome()
	})
	c.mu.Unlock()
}

// peekChrome shows the bar briefly on session start (unpinned default).
func (ui *sessionUI) peekChrome() {
	ui.revealChrome()
	ui.scheduleChromeHideAfter(chromePeekDelay)
}

func (ui *sessionUI) toggleChromePin() {
	if ui.chrome == nil {
		return
	}
	c := ui.chrome
	c.mu.Lock()
	c.pinned = !c.pinned
	pinned := c.pinned
	c.mu.Unlock()

	if c.pinBtn != nil {
		c.pinBtn.Importance = widget.MediumImportance
		if pinned {
			c.pinBtn.SetText("Unpin")
			c.pinBtn.SetIcon(theme.CheckButtonCheckedIcon())
		} else {
			c.pinBtn.SetText("Pin")
			c.pinBtn.SetIcon(theme.CheckButtonIcon())
		}
		c.pinBtn.Refresh()
	}
	if pinned {
		ui.revealChrome()
		ui.setStatus("Control bar pinned")
	} else {
		ui.scheduleChromeHide()
		ui.setStatus("Control bar unpinned")
	}
}

// toggleChromeVisible is the hotkey action: show if hidden, hide if shown.
func (ui *sessionUI) toggleChromeVisible() {
	if ui.chrome == nil {
		return
	}
	if ui.chrome.isVisible() {
		// Force-hide even when pinned would be surprising; unpin first if needed.
		if ui.chrome.isPinned() {
			ui.toggleChromePin()
		}
		ui.chrome.cancelHide()
		if ui.chrome.pill != nil {
			ui.chrome.pill.Hide()
			if ui.chrome.overlay != nil {
				ui.chrome.overlay.Refresh()
			}
		}
		return
	}
	ui.revealChrome()
	if !ui.chrome.isPinned() {
		ui.scheduleChromeHide()
	}
}

// handleChromePointer processes pad-local pointer motion for reveal/hide.
// Returns true when the host chrome consumes the event (no guest mouse).
func (ui *sessionUI) handleChromePointer(pos fyne.Position) (consumeGuest bool) {
	if ui.chrome == nil {
		return false
	}
	if inChromeHitZone(pos.Y) {
		ui.revealChrome()
		return true
	}
	if ui.chrome.blocksAt(pos) {
		ui.chromePointerEnter()
		return true
	}
	// Keep visible while pointer is in the vertical band of the bar strip.
	if ui.chrome.isVisible() && pos.Y <= ui.chrome.interactionBottom() {
		ui.chrome.cancelHide()
		return false
	}
	ui.scheduleChromeHide()
	return false
}

// chromeBlocksClick reports whether a pad-local click is host-chrome only.
func (ui *sessionUI) chromeBlocksClick(pos fyne.Position) bool {
	if ui.chrome == nil {
		return false
	}
	if inChromeHitZone(pos.Y) {
		ui.revealChrome()
		return true
	}
	return ui.chrome.blocksAt(pos)
}
