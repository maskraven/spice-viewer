// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package webdav_test

import (
	"bytes"
	"testing"

	"github.com/maskraven/spice-viewer/internal/webdav"
)

func TestClientFrameRoundTrip(t *testing.T) {
	in := webdav.ClientFrame{ClientID: 42, Payload: []byte("hello")}
	b := webdav.EncodeClientFrame(in)
	out, err := webdav.ParseClientFrame(b)
	if err != nil {
		t.Fatal(err)
	}
	if out.ClientID != 42 || !bytes.Equal(out.Payload, []byte("hello")) {
		t.Fatalf("%+v", out)
	}
}

func TestStaticShareRoot(t *testing.T) {
	var h webdav.ShareRootHook = webdav.StaticShareRoot{Path: "/share"}
	if h.Root() != "/share" {
		t.Fatal(h.Root())
	}
}
