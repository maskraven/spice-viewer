// Package spice is the public library root for the SPICE client.
//
// Consumers open a session with Connect / ConnectConfig, optionally built via
// ConnectConfigFromVV from a parsed pkg/vvfile.File. Implementation lives under
// internal/*; this package is the only library surface for session lifecycle.
//
// Password ownership:
//
//   - Connect deep-copies ConnectConfig.Password into session memory.
//   - Connect does not wipe the caller's slice.
//   - Client.Close wipes the private copy.
//
// Phase 1 does not auto-reconnect ticket/password sessions. On disconnect,
// open a fresh connection file and call Connect again.
//
// DisplayDriver and NullDriver support headless frame verification and UI
// backends. CursorDriver is best-effort.
//
// Import rules: may import internal/* as needed, but not internal/ui or GUI
// toolkits (library-first; UI stays in internal/ui).
package spice
