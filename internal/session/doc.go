// Package session owns SPICE session lifecycle and channel management.
//
// Phase 1: no auto-reconnect for short-lived Proxmox tickets.
// PR 06: main-channel dial + link authentication only (no child channels).
//
// Import rules: no UI imports.
package session
