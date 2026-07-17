// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package spice_test

import (
	"image"
	"testing"

	"github.com/maskraven/spice-viewer/internal/display"
	"github.com/maskraven/spice-viewer/internal/protocol"
	"github.com/maskraven/spice-viewer/pkg/spice"
)

func TestNullDriverHashStable(t *testing.T) {
	var drv spice.DisplayDriver = spice.NewNullDriver()
	comp := display.NewCompositor(spice.AsDriver(drv))

	if err := comp.CreateSurface(0, 2, 2, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary); err != nil {
		t.Fatal(err)
	}
	if err := comp.Fill(0, image.Rect(0, 0, 2, 2), [4]byte{1, 2, 3, 255}, nil); err != nil {
		t.Fatal(err)
	}
	comp.Mark()

	nd := drv.(*spice.NullDriver)
	h1 := nd.Hash()
	h2 := nd.Hash()
	if h1 == "" || h1 != h2 {
		t.Fatalf("hash unstable: %q %q", h1, h2)
	}
	if nd.PresentCount < 1 || nd.InvalidateCount < 1 {
		t.Fatalf("counts present=%d inval=%d", nd.PresentCount, nd.InvalidateCount)
	}
}
