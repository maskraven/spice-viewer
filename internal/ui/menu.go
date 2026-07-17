// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"fmt"
	"log"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// installMenus builds the main menu bar and a compact toolbar for daily use.
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
	)

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
			dialog.ShowInformation("Keyboard shortcuts", FormatSendKeyHelp(ui.bind), ui.win)
		}),
		fyne.NewMenuItem("About remote-viewer", func() {
			dialog.ShowInformation("About",
				"remote-viewer — SPICE client (Proxmox-friendly)\n"+
					"Library: github.com/maskraven/virt-viewer\n"+
					"Display, inputs, cursor, audio, Send Keys, hotkeys.\n"+
					"Clipboard: toolbar Copy/Paste via spice-vdagent; Type text fallback.",
				ui.win)
		}),
	)

	ui.win.SetMainMenu(fyne.NewMainMenu(fileMenu, viewMenu, sendMenu, helpMenu))

	// Toolbar: daily actions always visible (clipboard lives here, not an Edit menu).
	ui.toolbar = container.NewHBox(
		widget.NewButtonWithIcon("Ctrl+Alt+Del", theme.ConfirmIcon(), func() {
			ui.sendKeys(SendKeyPreset{Label: "Ctrl+Alt+Del", Keys: CADScancodes()})
		}),
		widget.NewButtonWithIcon("Ungrab", theme.CancelIcon(), func() {
			ui.releaseGrab()
			ui.refreshStatus()
		}),
		widget.NewButtonWithIcon("Fullscreen", theme.ViewFullScreenIcon(), func() {
			ui.toggleFullscreen()
			ui.refreshStatus()
		}),
		widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
			ui.copyFromGuest()
		}),
		widget.NewButtonWithIcon("Paste", theme.ContentPasteIcon(), func() {
			ui.pasteToGuest()
		}),
		widget.NewButtonWithIcon("Type…", theme.DocumentCreateIcon(), func() {
			ui.showTypeTextDialog()
		}),
	)
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

func (ui *sessionUI) setStatus(msg string) {
	if ui.status == nil {
		return
	}
	ui.status.SetText(msg)
}

func (ui *sessionUI) refreshStatus() {
	if ui.status == nil {
		return
	}
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
	// Single dense line; ellipsis when the window is narrow.
	ui.status.SetText(fmt.Sprintf("%s  ·  %s  ·  %s  ·  %s", title, grab, mode, agent))
}
