// Package connector provides Dialer implementations for SPICE transports:
// TCP, TLS, and HTTP CONNECT (Proxmox spiceproxy).
//
// Proxmox host tokens (pvespiceproxy:...) are opaque CONNECT authorities and
// must never be passed to net.SplitHostPort. TLS in subject-pin mode verifies
// the certificate chain against the .vv CA and pins the leaf DN via structured
// RDN comparison (not pkix.Name.String equality).
//
// Import rules: no UI imports.
package connector
