// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

//go:build !darwin

package ui

// Windows/Linux file associations start a new process per open; no in-process
// live-open rearm is required.
func rearmLiveOpenPlatformImpl() {}
