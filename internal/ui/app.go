// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"log"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/maskraven/virt-viewer/internal/audio"
	"github.com/maskraven/virt-viewer/internal/ux"
	"github.com/maskraven/virt-viewer/pkg/spice"
)

// RunGUI installs a Fyne display driver on cfg, connects the SPICE session,
// and blocks until the window is closed or the session disconnects.
//
// Window title comes from cfg.Title (fallback "spice-viewer").
// Hotkeys use cfg.Hotkeys with virt-viewer defaults when empty.
// cfg.Fullscreen starts the window fullscreen.
//
// On return, the client is closed. The caller retains ownership of cfg.Password
// (Connect copies it; Client.Close wipes the session copy only).
func RunGUI(ctx context.Context, cfg spice.ConnectConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}

	bind, err := BindingsFromConfig(cfg.Hotkeys)
	if err != nil {
		return ux.New(ux.ClassConfig, ux.MsgConfigEndpoint, err)
	}

	surface := NewSurface()
	// Driver must be set before Connect so the display channel presents here.
	cfg.Drivers.Display = surface
	// Host audio is best-effort: never fail the session if the device is missing.
	var hostSink *audio.Sink
	if cfg.Drivers.Playback == nil {
		if sink := audio.OpenDefault(); sink != nil {
			cfg.Drivers.Playback = sink
			if s, ok := sink.(*audio.Sink); ok {
				hostSink = s
			}
		}
	}

	a := app.NewWithID("com.maskraven.spice-viewer")
	title := cfg.Title
	if title == "" {
		title = "spice-viewer"
	}
	ui := newSessionUI(a, surface, bind, title, cfg.Fullscreen, cfg.Profile)

	client, err := spice.Connect(ctx, cfg)
	if err != nil {
		if hostSink != nil {
			hostSink.Close()
		}
		return err
	}
	ui.AttachClient(client)
	defer func() {
		if hostSink != nil {
			hostSink.Close()
		}
	}()

	var (
		closeOnce sync.Once
		runErr    error
		errMu     sync.Mutex
	)
	setErr := func(e error) {
		if e == nil {
			return
		}
		errMu.Lock()
		if runErr == nil {
			runErr = e
		}
		errMu.Unlock()
	}
	shutdown := func() {
		closeOnce.Do(func() {
			_ = client.Close()
			// Close window if still open (disconnect path).
			if fyne.CurrentApp() != nil {
				fyne.Do(func() {
					ui.win.Close()
				})
			}
		})
	}

	// Watch session events: close UI on disconnect.
	go func() {
		for {
			select {
			case <-ctx.Done():
				setErr(ctx.Err())
				shutdown()
				return
			case ev, ok := <-client.Events():
				if !ok {
					shutdown()
					return
				}
				switch ev.Type {
				case spice.EventError:
					if ev.Err != nil {
						log.Printf("spice-viewer: %v", ev.Err)
					}
				case spice.EventClipboard:
					text := ev.ClipboardText
					fyne.Do(func() {
						ui.onGuestClipboard(text)
					})
				case spice.EventAgent:
					fyne.Do(func() {
						if ev.AgentActive {
							ui.setStatus("Guest agent connected (clipboard ready)")
						} else {
							ui.setStatus("Guest agent disconnected")
						}
						ui.refreshStatus()
					})
				case spice.EventDisconnected:
					if ev.Err != nil {
						setErr(ev.Err)
						log.Printf("spice-viewer: disconnected: %v", ev.Err)
					}
					shutdown()
					return
				}
			}
		}
	}()

	ui.win.SetCloseIntercept(func() {
		shutdown()
		ui.win.Close()
	})

	ui.updateScaleHint()
	ui.win.ShowAndRun()

	// Ensure teardown if the user closed the window without intercept.
	shutdown()

	errMu.Lock()
	defer errMu.Unlock()
	return runErr
}

// FormatUXError returns a user-facing error string for CLI/GUI reporting.
func FormatUXError(err error) string {
	return ux.UserMessage(err)
}

// Ensure spice.DisplayDriver assignment type-checks in this package.
var _ spice.DisplayDriver = (*Surface)(nil)
