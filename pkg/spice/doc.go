// Package spice is the public library root for the SPICE client.
//
// Consumers open a session with Connect / ConnectConfig, optionally built via
// ConnectConfigFromVV from a parsed pkg/vvfile.File. Implementation lives under
// internal/*; this package is the only library surface for session lifecycle.
//
// Password ownership:
//
//   - Connect deep-copies ConnectConfig.Password into session memory (sole owner).
//   - Connect does not wipe the caller's slice.
//   - Client.Close wipes the session-owned copy via session.Close.
//   - CACertPEM is only used to build a TLS cert pool; not retained/wiped by Client.
//
// Events: after a successful Connect the first event is EventConnected; non-fatal
// EventError may follow (e.g. cursor/playback degrade). EventDisconnected is
// terminal. A fatal peer error recorded before or during Close is preserved on
// EventDisconnected.Err (Close does not erase it with a clean nil).
//
// Phase 1 does not auto-reconnect ticket/password sessions. On disconnect,
// open a fresh connection file and call Connect again.
//
// Phase 2 (desktop comfort):
//
//   - vdagent clipboard: SetHostClipboard, RequestGuestClipboard, EventClipboard
//   - guest resize: SetGuestDisplaySize (monitors config)
//   - codecs: raw, LZ, Quic, JPEG, MJPEG streams (display channel)
//   - playback: Drivers.Playback (RAW S16LE); Nil → NullPlayback
//
// The context passed to Connect only bounds dial/open; session lifetime after
// Connect returns is ended with Client.Close.
//
// DisplayDriver and NullDriver support headless frame verification and UI
// backends. CursorDriver and PlaybackDriver are best-effort. Hotkeys/Fullscreen
// stay on ConnectConfig (Client keeps Title only).
//
// Playback: set Drivers.Playback to a host audio sink implementing
// PlaybackDriver (WritePCM for S16LE). Nil uses NullPlayback (discard). See
// playback.go for the UI hook pattern. No cgo host device is linked by default.
//
// Import rules: may import internal/* as needed, but not internal/ui or GUI
// toolkits (library-first; UI stays in internal/ui).
package spice
