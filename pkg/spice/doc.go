// Package spice is the public library root for the SPICE client.
//
// Consumers obtain a session via Connect / ConnectConfig types
// (to be implemented). Implementation lives under internal/*.
//
// Phase 1 PR 08 exposes DisplayDriver and NullDriver for headless
// frame verification and future UI backends.
//
// Import rules: may import internal/* as needed, but not internal/ui
// or GUI toolkits (library-first; UI stays in internal/ui).
package spice
