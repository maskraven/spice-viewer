// Package spice is the public library root for the SPICE client.
//
// Consumers obtain a session via Connect / ConnectConfig types
// (to be implemented). Implementation lives under internal/*.
//
// Import rules: may import internal/* as needed, but not internal/ui
// or GUI toolkits (library-first; UI stays in internal/ui).
package spice
