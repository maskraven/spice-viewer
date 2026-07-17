// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"log"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
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
	clip := cb.Content()
	if !ui.client.AgentActive() {
		// Fallback: type host clipboard as keystrokes.
		if clip == "" {
			dialog.ShowInformation("Paste", "Host clipboard is empty.\nGuest agent not connected — use Type text… or install spice-vdagent in the guest.", ui.win)
			return
		}
		if err := TypeText(ui.inputs, clip); err != nil {
			dialog.ShowError(fmt.Errorf("agent offline; type-text fallback failed: %w\nInstall spice-vdagent in the guest for real paste", err), ui.win)
			return
		}
		ui.setStatus("Pasted via keystrokes (no agent)")
		return
	}
	if err := ui.client.SetHostClipboard(clip); err != nil {
		dialog.ShowError(err, ui.win)
		return
	}
	ui.setStatus("Clipboard offered to guest (agent)")
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
	entry := widget.NewMultiLineEntry()
	entry.SetPlaceHolder("Text is typed into the guest as keystrokes (US QWERTY).\nUse for short passwords/commands when clipboard is unavailable.")
	entry.Wrapping = fyne.TextWrapWord
	form := dialog.NewForm(
		"Type text into guest",
		"Type",
		"Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Text", entry),
		},
		func(ok bool) {
			if !ok {
				return
			}
			text := entry.Text
			if text == "" {
				return
			}
			if err := TypeText(ui.inputs, text); err != nil {
				log.Printf("ui: type text: %v", err)
				dialog.ShowError(err, ui.win)
				return
			}
			ui.setStatus(fmt.Sprintf("Typed %d characters", len([]rune(text))))
		},
		ui.win,
	)
	form.Resize(fyne.NewSize(480, 280))
	form.Show()
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
