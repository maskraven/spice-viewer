// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package security

import "testing"

func TestWipe(t *testing.T) {
	b := []byte("secret-ticket")
	Wipe(b)
	for i, c := range b {
		if c != 0 {
			t.Fatalf("byte %d = %d, want 0", i, c)
		}
	}
	// nil / empty must not panic
	Wipe(nil)
	Wipe([]byte{})
}
