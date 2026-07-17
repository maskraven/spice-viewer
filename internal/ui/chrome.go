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
	chromeTopMargin float32 = 6
	chromePeekDelay         = 2 * time.Second
	chromeHideDelay         = 1 * time.Second
)

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

// installChrome builds the overlay stack object and wires ui.chrome.
// Returns the overlay CanvasObject to place above the mouse pad in a Stack.
func (ui *sessionUI) installChrome() fyne.CanvasObject {
	pinBtn := widget.NewButtonWithIcon("Pin", theme.ViewRestoreIcon(), func() {
		ui.toggleChromePin()
	})
	pinBtn.Importance = widget.LowImportance

	cadBtn := widget.NewButtonWithIcon("CAD", theme.ConfirmIcon(), func() {
		ui.chromePointerEnter()
		ui.sendKeys(SendKeyPreset{Label: "Ctrl+Alt+Del", Keys: CADScancodes()})
	})
	ungrabBtn := widget.NewButtonWithIcon("Ungrab", theme.CancelIcon(), func() {
		ui.chromePointerEnter()
		ui.releaseGrab()
		ui.refreshStatus()
	})
	fsBtn := widget.NewButtonWithIcon("Fullscreen", theme.ViewFullScreenIcon(), func() {
		ui.chromePointerEnter()
		ui.toggleFullscreen()
		ui.refreshStatus()
	})
	copyBtn := widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		ui.chromePointerEnter()
		ui.copyFromGuest()
	})
	pasteBtn := widget.NewButtonWithIcon("Paste", theme.ContentPasteIcon(), func() {
		ui.chromePointerEnter()
		ui.pasteToGuest()
	})
	typeBtn := widget.NewButtonWithIcon("Type…", theme.DocumentCreateIcon(), func() {
		ui.chromePointerEnter()
		ui.showTypeTextDialog()
	})

	var sendBtn *widget.Button
	sendBtn = widget.NewButton("Send Keys ▾", func() {
		ui.showChromeSendKeysMenu(sendBtn)
	})
	var moreBtn *widget.Button
	moreBtn = widget.NewButton("More ▾", func() {
		ui.showChromeMoreMenu(moreBtn)
	})

	row := container.NewHBox(
		pinBtn,
		cadBtn,
		ungrabBtn,
		fsBtn,
		copyBtn,
		pasteBtn,
		typeBtn,
		sendBtn,
		moreBtn,
	)

	bg := canvas.NewRectangle(color.NRGBA{R: 28, G: 28, B: 32, A: 210})
	bg.CornerRadius = 8
	bg.StrokeColor = color.NRGBA{R: 80, G: 80, B: 90, A: 180}
	bg.StrokeWidth = 1

	// Stack: background sized to content via a padded HBox wrapper.
	padded := container.NewPadded(row)
	pillBody := container.NewStack(bg, padded)

	pill := newChromePill(ui, pillBody)
	overlay := container.New(&topCenterLayout{topMargin: chromeTopMargin}, pill)

	ui.chrome = &controlChrome{
		overlay: overlay,
		pill:    pill,
		pinBtn:  pinBtn,
	}
	return overlay
}

func (ui *sessionUI) showChromeSendKeysMenu(anchor fyne.CanvasObject) {
	if ui.win == nil {
		return
	}
	items := make([]*fyne.MenuItem, 0, len(StandardSendKeys())+2)
	for _, p := range StandardSendKeys() {
		preset := p
		items = append(items, fyne.NewMenuItem(preset.Label, func() {
			ui.sendKeys(preset)
		}))
	}
	items = append(items, fyne.NewMenuItemSeparator())
	items = append(items, fyne.NewMenuItem("Type text…", func() {
		ui.showTypeTextDialog()
	}))
	ui.showChromePopup(fyne.NewMenu("", items...), anchor)
}

func (ui *sessionUI) showChromeMoreMenu(anchor fyne.CanvasObject) {
	if ui.win == nil {
		return
	}
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
		if pinned {
			c.pinBtn.SetText("Unpin")
			c.pinBtn.Importance = widget.HighImportance
		} else {
			c.pinBtn.SetText("Pin")
			c.pinBtn.Importance = widget.LowImportance
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
