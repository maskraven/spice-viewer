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
// EventError may follow (e.g. cursor degrade). EventDisconnected is terminal.
// A fatal peer error recorded before or during Close is preserved on
// EventDisconnected.Err (Close does not erase it with a clean nil).
//
// Phase 1 does not auto-reconnect ticket/password sessions. On disconnect,
// open a fresh connection file and call Connect again.
//
// The context passed to Connect only bounds dial/open; session lifetime after
// Connect returns is ended with Client.Close.
//
// DisplayDriver and NullDriver support headless frame verification and UI
// backends. CursorDriver is best-effort. Hotkeys/Fullscreen stay on
// ConnectConfig (Client keeps Title only).
//
// Import rules: may import internal/* as needed, but not internal/ui or GUI
// toolkits (library-first; UI stays in internal/ui).
package spice
