// Package security holds SPICE ticket encryption helpers (RSAES-OAEP),
// secret zeroization utilities, and log redaction helpers.
//
// Secrets must never be logged. Use Redact / RedactingWriter before any
// debug or error text that might include passwords, tickets, or CA PEM.
// Unit tests in redact_test.go fail if a password appears in log output.
//
// Import rules: no UI imports.
package security
