// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"image/color"
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/maskraven/virt-viewer/pkg/spice"
)

// installMenus builds the main menu bar. Daily controls live in the floating
// top-center chrome (installChrome); there is no always-on toolbar.
func (ui *sessionUI) installMenus() {
	// --- File ---
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("Quit", func() {
			ui.win.Close()
		}),
	)

	// --- View ---
	viewMenu := fyne.NewMenu("View",
		fyne.NewMenuItem("Fullscreen", func() {
			ui.toggleFullscreen()
			ui.refreshStatus()
		}),
		fyne.NewMenuItem("Release cursor / ungrab", func() {
			ui.releaseGrab()
			ui.refreshStatus()
		}),
		fyne.NewMenuItem("Grab keyboard & mouse", func() {
			ui.enterGrab()
			ui.refreshStatus()
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Show control bar", func() {
			ui.revealChrome()
			if ui.chrome != nil && !ui.chrome.isPinned() {
				ui.scheduleChromeHide()
			}
		}),
		fyne.NewMenuItem("Pin control bar", func() {
			if ui.chrome != nil && !ui.chrome.isPinned() {
				ui.toggleChromePin()
			} else {
				ui.revealChrome()
			}
		}),
		fyne.NewMenuItem("Unpin control bar", func() {
			if ui.chrome != nil && ui.chrome.isPinned() {
				ui.toggleChromePin()
			}
		}),
	)

	// --- Profile (SPICE preferred compression; product-level “profiles”) ---
	profileItems := []*fyne.MenuItem{}
	for _, p := range []spice.PerformanceProfile{
		spice.ProfileDefault,
		spice.ProfileLAN,
		spice.ProfileWAN,
		spice.ProfileQuality,
	} {
		prof := p
		item := fyne.NewMenuItem(prof.Label(), func() {
			ui.applyProfile(prof)
		})
		profileItems = append(profileItems, item)
	}
	profileMenu := fyne.NewMenu("Profile", profileItems...)

	// --- Send Keys (virt-viewer style) ---
	sendItems := make([]*fyne.MenuItem, 0, len(StandardSendKeys())+2)
	for _, p := range StandardSendKeys() {
		preset := p // capture
		sendItems = append(sendItems, fyne.NewMenuItem(preset.Label, func() {
			ui.sendKeys(preset)
		}))
	}
	sendItems = append(sendItems, fyne.NewMenuItemSeparator())
	sendItems = append(sendItems, fyne.NewMenuItem("Type text…", func() {
		ui.showTypeTextDialog()
	}))
	sendMenu := fyne.NewMenu("Send Keys", sendItems...)

	// --- Help ---
	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("Keyboard shortcuts", func() {
			ui.showKeyboardShortcuts()
		}),
		fyne.NewMenuItem("About remote-viewer", func() {
			ui.showAbout()
		}),
	)

	ui.win.SetMainMenu(fyne.NewMainMenu(fileMenu, viewMenu, profileMenu, sendMenu, helpMenu))
}

func (ui *sessionUI) applyProfile(p spice.PerformanceProfile) {
	ui.profile = p
	if ui.client != nil {
		if err := ui.client.ApplyPerformanceProfile(p); err != nil {
			log.Printf("ui: apply profile %s: %v", p.String(), err)
			ui.setStatus(fmt.Sprintf("Profile %s failed (server may ignore)", p.String()))
			return
		}
	}
	ui.setStatus(fmt.Sprintf("Profile: %s", p.Label()))
	ui.refreshStatus()
}

func (ui *sessionUI) showKeyboardShortcuts() {
	dialog.ShowInformation("Keyboard shortcuts", FormatSendKeyHelp(ui.bind), ui.win)
}

func (ui *sessionUI) showAbout() {
	dialog.ShowInformation("About",
		"remote-viewer — SPICE client (Proxmox-friendly)\n"+
			"Library: github.com/maskraven/virt-viewer\n"+
			"Display, inputs, cursor, audio, Send Keys, hotkeys.\n"+
			"Controls: top-center auto-hide bar (Pin · CAD · Ungrab · clipboard).\n"+
			"Clipboard: Copy/Paste via spice-vdagent; Type text fallback.",
		ui.win)
}

func (ui *sessionUI) hostClipboard() fyne.Clipboard {
	if ui.app != nil {
		return ui.app.Clipboard()
	}
	if a := fyne.CurrentApp(); a != nil {
		return a.Clipboard()
	}
	return nil
}

func (ui *sessionUI) pasteToGuest() {
	if ui.client == nil {
		dialog.ShowError(fmt.Errorf("not connected"), ui.win)
		return
	}
	cb := ui.hostClipboard()
	if cb == nil {
		dialog.ShowError(fmt.Errorf("clipboard unavailable"), ui.win)
		return
	}
	clip := foldClipboardText(cb.Content())
	if clip == "" {
		dialog.ShowInformation("Paste", "Host clipboard is empty (or only unsupported characters).", ui.win)
		return
	}

	// Prefer vdagent when fully active: offer clipboard, then inject Ctrl+V so
	// text appears in the focused guest control (spice-gtk only shares the
	// clipboard; users still expect the Paste button to paste).
	if ui.client.AgentActive() {
		if err := ui.client.SetHostClipboard(clip); err != nil {
			log.Printf("ui: agent paste failed, falling back to keystrokes: %v", err)
		} else {
			// Brief yield so the guest agent can process GRAB before REQUEST.
			time.Sleep(40 * time.Millisecond)
			if ui.inputs != nil {
				// Clear sticky Shift first (Ctrl+Shift+V is not a normal paste).
				ReleaseModifiers(ui.inputs)
				if err := InjectSequence(ui.inputs, []uint16{scanLCtrl, letterScancode('v')}); err != nil {
					log.Printf("ui: Ctrl+V after agent grab: %v", err)
					ReleaseModifiers(ui.inputs)
					ui.setStatus("Clipboard offered to guest — press Ctrl+V in guest")
					return
				}
				ReleaseModifiers(ui.inputs)
			}
			ui.setStatus(fmt.Sprintf("Pasted %d chars (agent)", len([]rune(clip))))
			return
		}
	}

	// No agent (or agent failed): type as US-QWERTY keystrokes.
	if ui.inputs == nil {
		dialog.ShowError(fmt.Errorf("inputs not connected; cannot type paste"), ui.win)
		return
	}
	// Ensure guest is receiving input (matches click-to-type path).
	if !ui.grab.Active() {
		ui.enterGrab()
	}
	n, err := TypeTextBestEffort(ui.inputs, clip)
	if n == 0 {
		if err != nil {
			dialog.ShowError(fmt.Errorf("paste via keystrokes failed: %w\nInstall SPICE Guest Tools / spice-vdagent for real clipboard", err), ui.win)
			return
		}
		dialog.ShowInformation("Paste", "Nothing could be typed (unsupported characters).\nInstall SPICE Guest Tools / spice-vdagent for full clipboard.", ui.win)
		return
	}
	if err != nil {
		ui.setStatus(fmt.Sprintf("Pasted %d chars via keys (some skipped)", n))
		return
	}
	ui.setStatus(fmt.Sprintf("Pasted %d chars via keystrokes (no agent)", n))
}

func (ui *sessionUI) copyFromGuest() {
	if ui.client == nil {
		dialog.ShowError(fmt.Errorf("not connected"), ui.win)
		return
	}
	if !ui.client.AgentActive() {
		dialog.ShowInformation("Copy from guest",
			"Guest agent is not connected.\nInstall and run spice-vdagent in the VM, then copy again.",
			ui.win)
		return
	}
	if err := ui.client.RequestGuestClipboard(); err != nil {
		dialog.ShowError(err, ui.win)
		return
	}
	ui.setStatus("Requested clipboard from guest…")
}

// onGuestClipboard puts guest text on the host clipboard.
func (ui *sessionUI) onGuestClipboard(text string) {
	cb := ui.hostClipboard()
	if cb == nil {
		return
	}
	cb.SetContent(text)
	ui.setStatus(fmt.Sprintf("Copied %d chars from guest", len([]rune(text))))
}

func (ui *sessionUI) sendKeys(p SendKeyPreset) {
	if ui.inputs == nil {
		dialog.ShowError(fmt.Errorf("not connected"), ui.win)
		return
	}
	// Inject even when ungrabbed — that is the point of Send Keys.
	if err := InjectSequence(ui.inputs, p.Keys); err != nil {
		log.Printf("ui: send keys %s: %v", p.Label, err)
		dialog.ShowError(err, ui.win)
		return
	}
	ui.setStatus(fmt.Sprintf("Sent %s", p.Label))
}

func (ui *sessionUI) showTypeTextDialog() {
	if ui.inputs == nil {
		dialog.ShowError(fmt.Errorf("not connected"), ui.win)
		return
	}
	if ui.win == nil {
		return
	}
	// Custom dark panel (not dialog.NewForm): dismiss on outside click,
	// matches Keys list styling (no white board, white text).
	entry := widget.NewMultiLineEntry()
	entry.SetPlaceHolder("US QWERTY keystrokes into the guest…")
	entry.Wrapping = fyne.TextWrapWord
	entry.SetMinRowsVisible(6)

	title := widget.NewLabel("Type text into guest")
	title.TextStyle = fyne.TextStyle{Bold: true}

	var closeDlg func()
	doType := func() {
		text := entry.Text
		closeDlg()
		if text == "" {
			return
		}
		if err := TypeText(ui.inputs, text); err != nil {
			log.Printf("ui: type text: %v", err)
			dialog.ShowError(err, ui.win)
			return
		}
		ui.setStatus(fmt.Sprintf("Typed %d characters", len([]rune(text))))
	}
	typeBtn := widget.NewButton("Type", doType)
	typeBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() { closeDlg() })
	cancelBtn.Importance = widget.MediumImportance
	buttons := container.NewHBox(cancelBtn, typeBtn)

	inner := container.NewBorder(
		title,
		buttons,
		nil, nil,
		entry,
	)
	bg := canvas.NewRectangle(color.NRGBA{R: 32, G: 32, B: 36, A: 255})
	bg.CornerRadius = 8
	bg.StrokeColor = color.NRGBA{R: 55, G: 58, B: 66, A: 255}
	bg.StrokeWidth = 1
	card := container.NewStack(bg, container.NewPadded(inner))
	card.Resize(fyne.NewSize(480, 280))

	// Center on canvas.
	cs := ui.win.Canvas().Size()
	pos := fyne.NewPos((cs.Width-480)/2, (cs.Height-280)/2)
	if pos.X < 8 {
		pos.X = 8
	}
	if pos.Y < 8 {
		pos.Y = 8
	}

	th := darkPanelTheme{Theme: theme.Current()}
	themed := container.NewThemeOverride(card, th)
	// Force fixed size for the overlay panel.
	wrap := container.NewStack(themed)
	wrap.Resize(fyne.NewSize(480, 280))

	ui.showDarkPanelOverlay(wrap, pos)
	// Override dismiss already closes darkOverlay; closeDlg just hides overlay.
	closeDlg = func() {
		ui.closeDarkOverlay()
	}
	// Focus entry for immediate typing.
	ui.win.Canvas().Focus(entry)
}

const statusActionHold = 4 * time.Second

// setStatus shows a transient action after the base connection status, e.g.
// "vm  ·  free  ·  client mouse  ·  agent  ·  Sent Windows / Super".
func (ui *sessionUI) setStatus(msg string) {
	if ui == nil {
		return
	}
	ui.mu.Lock()
	ui.statusAction = msg
	if ui.statusActionTimer != nil {
		ui.statusActionTimer.Stop()
		ui.statusActionTimer = nil
	}
	if msg != "" {
		ui.statusActionTimer = time.AfterFunc(statusActionHold, func() {
			if fyne.CurrentApp() != nil {
				fyne.Do(func() {
					ui.clearStatusAction()
				})
				return
			}
			ui.clearStatusAction()
		})
	}
	ui.mu.Unlock()
	ui.paintStatus()
}

func (ui *sessionUI) clearStatusAction() {
	if ui == nil {
		return
	}
	ui.mu.Lock()
	ui.statusAction = ""
	if ui.statusActionTimer != nil {
		ui.statusActionTimer.Stop()
		ui.statusActionTimer = nil
	}
	ui.mu.Unlock()
	ui.paintStatus()
}

// refreshStatus rebuilds the base connection line and keeps any active action.
func (ui *sessionUI) refreshStatus() {
	ui.paintStatus()
}

// baseStatusLine is the persistent connection summary (left side of the bar).
func (ui *sessionUI) baseStatusLine() string {
	grab := "free"
	if ui.grab.Active() {
		grab = "grabbed"
	}
	mode := "mouse?"
	if ui.inputs != nil {
		if ui.inputs.ClientMouse() {
			mode = "client mouse"
		} else {
			mode = "server mouse"
		}
	}
	agent := "no agent"
	if ui.client != nil && ui.client.AgentActive() {
		agent = "agent"
	}
	title := ""
	if ui.client != nil {
		title = ui.client.Title()
	}
	if title == "" {
		title = "SPICE"
	}
	prof := "default"
	if ui != nil {
		prof = ui.profile.String()
	}
	return fmt.Sprintf("%s  ·  %s  ·  %s  ·  %s  ·  %s", title, grab, mode, agent, prof)
}

// paintStatus writes "base [ ·  action]" left-aligned into the status strip.
func (ui *sessionUI) paintStatus() {
	if ui == nil || ui.statusStrip == nil {
		return
	}
	line := ui.baseStatusLine()
	ui.mu.Lock()
	action := ui.statusAction
	ui.mu.Unlock()
	if action != "" {
		line = line + "  ·  " + action
	}
	ui.statusStrip.SetLine(line)
}
