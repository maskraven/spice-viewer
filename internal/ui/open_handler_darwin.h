// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

#ifndef SPICE_VIEWER_OPEN_HANDLER_DARWIN_H
#define SPICE_VIEWER_OPEN_HANDLER_DARWIN_H

#ifdef __cplusplus
extern "C" {
#endif

// Install kAEOpenDocuments handler (idempotent). Call from Go init.
void spiceViewerInstallOpenHandler(void);

// Pump the main run loop for up to `seconds` so queued open events deliver.
void spiceViewerPumpOpenEvents(double seconds);

// Returns malloc'd UTF-8 path of the first pending document, or NULL.
// Caller must free().
char *spiceViewerTakePendingPath(void);

#ifdef __cplusplus
}
#endif

#endif
