// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"strings"

	"github.com/maskraven/virt-viewer/internal/connector"
	"github.com/maskraven/virt-viewer/internal/ux"
	"github.com/maskraven/virt-viewer/pkg/vvfile"
)

// HotkeyConfig holds virt-viewer hotkey chords from a connection file.
// Values are free-form chord strings (e.g. "Ctrl+Alt+Ins"); the UI layer
// interprets them. Phase 1 does not parse chords inside this package.
type HotkeyConfig struct {
	// SecureAttention injects Ctrl+Alt+Del (CAD) when the chord is pressed.
	SecureAttention string
	// ReleaseCursor ungrabs the mouse/keyboard.
	ReleaseCursor string
	// ToggleFullscreen toggles fullscreen presentation.
	ToggleFullscreen string
}

// Drivers is the optional set of frame/cursor sinks supplied by the UI.
// Nil fields use headless defaults (NullDriver / no cursor shape).
type Drivers struct {
	Display DisplayDriver
	Cursor  CursorDriver
}

// ConnectConfig is the library entry configuration for a SPICE session.
//
// Password ownership:
//   - Connect deep-copies Password (and CACertPEM) into session memory.
//   - Connect does NOT wipe the caller's Password slice (caller may reuse);
//     use security.Wipe or zero the slice after Connect if desired.
//   - Client.Close wipes the session-owned password copy.
//
// No auto-reconnect is performed for ticket/password sessions (Phase 1).
// AllowReconnect is reserved for a future lab-only path and is ignored today.
type ConnectConfig struct {
	Host        string
	Port        int // cleartext lab port (requires AllowCleartext when TLSPort==0)
	TLSPort     int
	Password    []byte // secret; copied on Connect, not wiped by Connect
	ProxyURL    string // e.g. "http://proxy:3128"; empty = direct
	CACertPEM   []byte // PEM; required when TLSPort is set
	HostSubject string // OpenSSL-style DN pin (Proxmox); empty = DNS ServerName mode
	Title       string
	Hotkeys     HotkeyConfig
	Fullscreen  bool
	Drivers     Drivers

	// AllowReconnect is reserved; Phase 1 never auto-reconnects ticket sessions.
	AllowReconnect bool
	// AllowCleartext permits a non-TLS dial when TLSPort is 0 (lab only).
	AllowCleartext bool

	// dialer overrides the default connector dialer (tests; unexported).
	dialer connector.Dialer
}

// ConnectConfigFromVV maps a parsed virt-viewer connection file into ConnectConfig.
//
// Field map:
//
//	Host←Host, TLSPort←TLSPort, Port←Port, Password←Password (copy),
//	ProxyURL←Proxy.String(), CACertPEM←CA (copy), HostSubject←HostSubject,
//	Title←Title, Hotkeys from secure-attention / release-cursor / toggle-fullscreen,
//	Fullscreen←Fullscreen.
//
// Drivers are left zero (caller sets ConnectConfig.Drivers). DeleteThisFile is a
// parse-side concern and is not mapped. Password and CA are deep-copied so the
// caller may wipe f.Password independently.
func ConnectConfigFromVV(f *vvfile.File) (ConnectConfig, error) {
	if f == nil {
		return ConnectConfig{}, ux.New(ux.ClassConfig, ux.MsgConfigEndpoint,
			fmt.Errorf("spice: nil vvfile.File"))
	}
	if !strings.EqualFold(f.Type, "spice") && f.Type != "" {
		return ConnectConfig{}, ux.New(ux.ClassConfig, ux.MsgConfigNotSpice,
			fmt.Errorf("spice: type %q is not spice", f.Type))
	}

	cfg := ConnectConfig{
		Host:        f.Host,
		Port:        f.Port,
		TLSPort:     f.TLSPort,
		HostSubject: f.HostSubject,
		Title:       f.Title,
		Fullscreen:  f.Fullscreen,
		Hotkeys: HotkeyConfig{
			SecureAttention:  f.SecureAttention,
			ReleaseCursor:    f.ReleaseCursor,
			ToggleFullscreen: f.ToggleFullscreen,
		},
	}
	if len(f.Password) > 0 {
		cfg.Password = make([]byte, len(f.Password))
		copy(cfg.Password, f.Password)
	}
	if len(f.CA) > 0 {
		cfg.CACertPEM = make([]byte, len(f.CA))
		copy(cfg.CACertPEM, f.CA)
	}
	if f.Proxy != nil {
		cfg.ProxyURL = f.Proxy.String()
	}
	return cfg, nil
}

// endpoint builds a connector.Endpoint from cfg.
func (cfg ConnectConfig) endpoint() (connector.Endpoint, error) {
	if cfg.Host == "" {
		return connector.Endpoint{}, ux.New(ux.ClassConfig, ux.MsgConfigEndpoint,
			fmt.Errorf("spice: empty host"))
	}

	ep := connector.Endpoint{
		Host:           cfg.Host,
		AllowCleartext: cfg.AllowCleartext,
	}

	if cfg.ProxyURL != "" {
		u, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			return connector.Endpoint{}, ux.New(ux.ClassConfig, ux.MsgConfigEndpoint,
				fmt.Errorf("spice: invalid proxy URL: %w", err))
		}
		if u.Scheme == "" || u.Host == "" {
			return connector.Endpoint{}, ux.New(ux.ClassConfig, ux.MsgConfigEndpoint,
				fmt.Errorf("spice: invalid proxy URL %q (need scheme and host)", cfg.ProxyURL))
		}
		ep.Proxy = u
	}

	switch {
	case cfg.TLSPort != 0:
		ep.Port = cfg.TLSPort
		tlsParams := &connector.TLSParams{
			HostSubject: cfg.HostSubject,
		}
		if len(cfg.CACertPEM) > 0 {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(cfg.CACertPEM) {
				return connector.Endpoint{}, ux.New(ux.ClassConfig, ux.MsgConfigEndpoint,
					fmt.Errorf("spice: failed to parse CA PEM"))
			}
			tlsParams.RootCAs = pool
		}
		// DNS mode: no host-subject → use Host as ServerName when it looks like a name.
		if cfg.HostSubject == "" {
			tlsParams.ServerName = cfg.Host
		}
		ep.TLS = tlsParams
	case cfg.Port != 0:
		ep.Port = cfg.Port
		ep.AllowCleartext = true // port-only path is cleartext by definition
		if !cfg.AllowCleartext {
			// Still require explicit opt-in so Proxmox TLS paths cannot fall through.
			return connector.Endpoint{}, ux.New(ux.ClassConfig, ux.MsgConfigEndpoint,
				fmt.Errorf("spice: cleartext port requires AllowCleartext"))
		}
	default:
		return connector.Endpoint{}, ux.New(ux.ClassConfig, ux.MsgConfigEndpoint,
			fmt.Errorf("spice: TLSPort or Port is required"))
	}

	return ep, nil
}
