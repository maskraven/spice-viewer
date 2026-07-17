// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"

	"github.com/maskraven/spice-viewer/internal/audio"
	"github.com/maskraven/spice-viewer/internal/ux"
	"github.com/maskraven/spice-viewer/pkg/spice"
	"github.com/maskraven/spice-viewer/pkg/vvfile"
)

// fyneAppID is the stable Fyne preferences / storage ID. Must stay fixed so
// settings survive across launches (do not suffix with PID).
const fyneAppID = "com.maskraven.spice-viewer"

// RunGUI runs the multi-window GUI host: one process, one SPICE session per
// window. The first connection comes from cfg; further .vv opens (macOS
// Finder, File → Open) add windows without replacing existing sessions.
//
// Windows/Linux already get multi-process opens via the OS; this also allows
// File → Open for a second window in the same process on every platform.
//
// Does not replace NSApplication.delegate (unsafe with Fyne/GLFW on macOS).
func RunGUI(ctx context.Context, cfg spice.ConnectConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}

	// Fyne logs "Preferences load error: EOF" when preferences.json exists but
	// is empty (or was truncated mid-write by another instance). Repair before
	// NewWithID so startup stays quiet.
	sanitizeFynePreferences(fyneAppID)

	a := app.NewWithID(fyneAppID)
	h := newAppHost(ctx, a, cfg.ShareDir, cfg.Profile)

	if err := h.openSessionFromConfig(cfg); err != nil {
		return err
	}

	SetLiveOpenHandler(func(path string) {
		// Always hop onto the UI thread before touching Fyne.
		if fyne.CurrentApp() != nil {
			fyne.Do(func() {
				h.openPathInteractive(path)
			})
			return
		}
		h.openPathInteractive(path)
	})

	a.Lifecycle().SetOnStarted(func() {
		EnableLiveOpens()
	})
	// Also enable once first session exists (OnStarted can be late/missed).
	EnableLiveOpens()

	first := h.anyWindow()
	if first == nil {
		return ux.New(ux.ClassInternal, ux.MsgInternal, fmt.Errorf("no session window"))
	}
	// App-wide event loop (additional windows only call Show).
	first.ShowAndRun()

	DisableLiveOpens()
	h.closeAll()
	return h.takeError()
}

// sanitizeFynePreferences repairs a missing/empty/corrupt Fyne preferences.json
// for the given app ID. Fyne v2 decodes the file with encoding/json; a zero-byte
// file yields EOF and a noisy "Preferences load error" log on every launch.
//
// Also removes leftover per-PID preference dirs (com.app.p12345) created by an
// earlier multi-instance experiment that changed the Fyne app ID.
func sanitizeFynePreferences(appID string) {
	root := fynePreferencesRoot()
	if root == "" || appID == "" {
		return
	}
	dir := filepath.Join(root, appID)
	path := filepath.Join(dir, "preferences.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Absent is fine — Fyne treats it as empty store.
			return
		}
		return
	}
	if !validFynePreferencesJSON(data) {
		_ = os.WriteFile(path, []byte("{}\n"), 0o600)
	}

	// Clean orphan PID-suffixed stores: com.maskraven.spice-viewer.pNNNN
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	prefix := appID + ".p"
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
}

func validFynePreferencesJSON(data []byte) bool {
	s := strings.TrimSpace(string(data))
	if s == "" || s[0] != '{' {
		return false
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return false
	}
	// json.Unmarshal("null", &map) succeeds with m == nil; only objects are OK.
	return m != nil
}

// fynePreferencesRoot matches fyne.io/fyne/v2/internal/app.RootConfigDir for
// desktop platforms (where preferences.json lives).
func fynePreferencesRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Preferences", "fyne")
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "fyne")
		}
		return filepath.Join(home, "AppData", "Roaming", "fyne")
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "fyne")
		}
		return filepath.Join(home, ".config", "fyne")
	}
}

// appHost owns the Fyne application and all live SPICE session windows.
type appHost struct {
	ctx      context.Context
	app      fyne.App
	shareDir string
	profile  spice.PerformanceProfile

	mu       sync.Mutex
	sessions map[*session]*struct{}
	runErr   error
}

type session struct {
	ui     *sessionUI
	client *spice.Client
	sink   *audio.Sink

	closeOnce sync.Once
}

func newAppHost(ctx context.Context, a fyne.App, shareDir string, profile spice.PerformanceProfile) *appHost {
	return &appHost{
		ctx:      ctx,
		app:      a,
		shareDir: shareDir,
		profile:  profile,
		sessions: make(map[*session]*struct{}),
	}
}

func (h *appHost) takeError() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.runErr
}

func (h *appHost) setError(err error) {
	if err == nil {
		return
	}
	h.mu.Lock()
	if h.runErr == nil {
		h.runErr = err
	}
	h.mu.Unlock()
}

func (h *appHost) anyWindow() fyne.Window {
	h.mu.Lock()
	defer h.mu.Unlock()
	for s := range h.sessions {
		if s.ui != nil && s.ui.win != nil {
			return s.ui.win
		}
	}
	return nil
}

func (h *appHost) sessionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.sessions)
}

// openPathInteractive opens a connection file; errors are shown in a dialog.
func (h *appHost) openPathInteractive(path string) {
	path = NormalizePathArg(path)
	if path == "" {
		return
	}
	if err := h.openSessionFromPath(path); err != nil {
		log.Printf("ui: open session %q: %v", path, err)
		w := h.anyWindow()
		if w != nil {
			dialog.ShowError(err, w)
		}
	}
}

// OpenPicker shows the native file chooser and opens a new session window.
func (h *appHost) OpenPicker() {
	path, err := PickConnectionFile()
	if err != nil {
		w := h.anyWindow()
		if w != nil {
			dialog.ShowError(err, w)
		}
		return
	}
	if path == "" {
		return
	}
	h.openPathInteractive(path)
}

func (h *appHost) openSessionFromPath(path string) error {
	f, err := vvfile.ParseFile(path, vvfile.ParseOptions{DeleteIfRequested: true})
	if err != nil {
		return err
	}
	if f.DeleteErr != nil {
		log.Printf("spice-viewer: warning: could not delete connection file: %v", f.DeleteErr)
	}
	cfg, err := spice.ConnectConfigFromVV(f)
	if err != nil {
		wipeBytes(f.Password)
		wipeBytes(f.CA)
		return err
	}
	cfg.ShareDir = h.shareDir
	if cfg.Profile == spice.ProfileDefault {
		cfg.Profile = h.profile
	}
	if cfg.Title == "" {
		cfg.Title = "spice-viewer"
	}
	err = h.openSessionFromConfig(cfg)
	wipeBytes(cfg.Password)
	wipeBytes(cfg.CACertPEM)
	wipeBytes(f.Password)
	wipeBytes(f.CA)
	return err
}

func (h *appHost) openSessionFromConfig(cfg spice.ConnectConfig) error {
	if h.ctx == nil {
		h.ctx = context.Background()
	}

	bind, err := BindingsFromConfig(cfg.Hotkeys)
	if err != nil {
		return ux.New(ux.ClassConfig, ux.MsgConfigEndpoint, err)
	}

	surface := NewSurface()
	cfg.Drivers.Display = surface
	cursorDrv := newCursorBridge()
	if cfg.Drivers.Cursor == nil {
		cfg.Drivers.Cursor = cursorDrv
	}

	var hostSink *audio.Sink
	if cfg.Drivers.Playback == nil {
		if sink := audio.OpenDefault(); sink != nil {
			cfg.Drivers.Playback = sink
			if s, ok := sink.(*audio.Sink); ok {
				hostSink = s
			}
		}
	}

	title := cfg.Title
	if title == "" {
		title = "spice-viewer"
	}
	// Do not SetMaster: multi-window requires the app to outlive any single session.
	ui := newSessionUI(h.app, surface, bind, title, cfg.Fullscreen, cfg.Profile, cursorDrv, h)

	client, err := spice.Connect(h.ctx, cfg)
	if err != nil {
		if hostSink != nil {
			hostSink.Close()
		}
		return err
	}
	ui.AttachClient(client)

	s := &session{ui: ui, client: client, sink: hostSink}
	h.mu.Lock()
	h.sessions[s] = &struct{}{}
	n := len(h.sessions)
	h.mu.Unlock()

	ui.win.SetCloseIntercept(func() {
		// Intercept replaces default close: tear down, then Close on this UI thread.
		h.teardownSession(s)
		ui.win.SetCloseIntercept(nil)
		ui.win.Close()
		if h.sessionCount() == 0 && h.app != nil {
			h.app.Quit()
		}
	})

	// Per-session disconnect watcher.
	go h.watchSession(s)

	log.Printf("ui: session window ready (%s) sessions=%d", title, n)
	ui.win.Show()
	ui.win.RequestFocus()
	return nil
}

func (h *appHost) watchSession(s *session) {
	if s == nil || s.client == nil {
		return
	}
	for {
		select {
		case <-h.ctx.Done():
			h.closeSession(s)
			return
		case ev, ok := <-s.client.Events():
			if !ok {
				h.closeSession(s)
				return
			}
			switch ev.Type {
			case spice.EventError:
				if ev.Err != nil {
					log.Printf("spice-viewer: %v", ev.Err)
				}
			case spice.EventClipboard:
				text := ev.ClipboardText
				if fyne.CurrentApp() != nil {
					fyne.Do(func() {
						if s.ui != nil {
							s.ui.onGuestClipboard(text)
						}
					})
				}
			case spice.EventAgent:
				if fyne.CurrentApp() != nil {
					fyne.Do(func() {
						if s.ui == nil {
							return
						}
						if ev.AgentActive {
							s.ui.setStatus("Guest agent connected (clipboard ready)")
						} else {
							s.ui.setStatus("Guest agent disconnected")
						}
						s.ui.refreshStatus()
					})
				}
			case spice.EventDisconnected:
				if ev.Err != nil {
					h.setError(ev.Err)
					log.Printf("spice-viewer: disconnected: %v", ev.Err)
				}
				h.closeSession(s)
				return
			}
		}
	}
}

// teardownSession closes the SPICE client and removes the session from the host
// (idempotent). Does not touch the window.
func (h *appHost) teardownSession(s *session) {
	if s == nil {
		return
	}
	s.closeOnce.Do(func() {
		if s.client != nil {
			_ = s.client.Close()
		}
		if s.sink != nil {
			s.sink.Close()
		}
		h.mu.Lock()
		delete(h.sessions, s)
		h.mu.Unlock()
	})
}

// closeSession tears down the session and closes its window (from any goroutine).
func (h *appHost) closeSession(s *session) {
	if s == nil {
		return
	}
	h.teardownSession(s)
	finish := func() {
		if s.ui != nil && s.ui.win != nil {
			s.ui.win.SetCloseIntercept(nil)
			s.ui.win.Close()
		}
		if h.sessionCount() == 0 && h.app != nil {
			h.app.Quit()
		}
	}
	if fyne.CurrentApp() != nil {
		fyne.Do(finish)
	} else {
		finish()
	}
}

func (h *appHost) closeAll() {
	h.mu.Lock()
	list := make([]*session, 0, len(h.sessions))
	for s := range h.sessions {
		list = append(list, s)
	}
	h.mu.Unlock()
	for _, s := range list {
		h.closeSession(s)
	}
}

func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// FormatUXError returns a user-facing error string for CLI/GUI reporting.
func FormatUXError(err error) string {
	return ux.UserMessage(err)
}

// Ensure spice.DisplayDriver assignment type-checks in this package.
var _ spice.DisplayDriver = (*Surface)(nil)
