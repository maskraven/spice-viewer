// Package session owns SPICE session lifecycle and channel management.
//
// Phase 1: no auto-reconnect for short-lived Proxmox tickets.
//
//	PR 06: main-channel dial + link authentication
//	PR 07: MAIN_INIT session_id, CHANNELS_LIST, parallel child links
//	       (display+inputs required; cursor best-effort)
//
// OpenChannels is single-flight and one-shot per Session: concurrent calls are
// rejected, and either success or failure prevents retry (main has already
// consumed ATTACH_CHANNELS / CHANNELS_LIST). Create a new Session to reconnect.
//
// Import rules: no UI imports.
package session
