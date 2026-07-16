// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package security

// Wipe zeros b in place. Best-effort only: the Go GC may retain copies,
// and the compiler is not required to preserve the writes. Prefer []byte
// secrets over string; call Wipe on Close of session-owned password copies.
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
