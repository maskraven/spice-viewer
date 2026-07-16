// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package display_test

import (
	"image"
	"testing"

	"github.com/maskraven/virt-viewer/internal/codec"
	"github.com/maskraven/virt-viewer/internal/display"
	"github.com/maskraven/virt-viewer/internal/protocol"
)

func TestSurfaceBoundsReject(t *testing.T) {
	c := display.NewCompositor(nil)

	// Zero size
	if err := c.CreateSurface(0, 0, 100, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary); err == nil {
		t.Fatal("expected reject zero width")
	}
	// Over max side
	if err := c.CreateSurface(0, protocol.MaxSurfaceSide+1, 1, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary); err == nil {
		t.Fatal("expected reject oversize side")
	}
	// Over max bytes: 8192*8192*4 = 256 MiB > 64 MiB
	if err := c.CreateSurface(0, 5000, 5000, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary); err == nil {
		t.Fatal("expected reject oversize bytes")
	}
	// Valid small surface
	if err := c.CreateSurface(0, 64, 48, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Fill outside bounds
	if err := c.Fill(0, image.Rect(0, 0, 100, 10), [4]byte{1, 2, 3, 255}, nil); err == nil {
		t.Fatal("expected fill bounds reject")
	}
	// Fill OK
	if err := c.Fill(0, image.Rect(0, 0, 10, 10), [4]byte{255, 0, 0, 255}, nil); err != nil {
		t.Fatalf("fill: %v", err)
	}
}

func TestNullDriverHashAfterDrawOps(t *testing.T) {
	drv := display.NewNullDriver()
	c := display.NewCompositor(drv)

	if err := c.CreateSurface(0, 4, 4, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary); err != nil {
		t.Fatal(err)
	}
	// Fill whole surface red
	if err := c.Fill(0, image.Rect(0, 0, 4, 4), [4]byte{0xff, 0x00, 0x00, 0xff}, nil); err != nil {
		t.Fatal(err)
	}
	c.Mark()
	hash1 := drv.Hash()
	if hash1 == "" {
		t.Fatal("empty hash")
	}
	if drv.PresentCount < 1 {
		t.Fatalf("present count %d", drv.PresentCount)
	}

	// Same ops on a fresh compositor → same hash
	drv2 := display.NewNullDriver()
	c2 := display.NewCompositor(drv2)
	_ = c2.CreateSurface(0, 4, 4, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary)
	_ = c2.Fill(0, image.Rect(0, 0, 4, 4), [4]byte{0xff, 0x00, 0x00, 0xff}, nil)
	c2.Mark()
	if drv2.Hash() != hash1 {
		t.Fatalf("hash mismatch: %s vs %s", hash1, drv2.Hash())
	}

	// Different fill → different hash
	drv3 := display.NewNullDriver()
	c3 := display.NewCompositor(drv3)
	_ = c3.CreateSurface(0, 4, 4, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary)
	_ = c3.Fill(0, image.Rect(0, 0, 4, 4), [4]byte{0x00, 0xff, 0x00, 0xff}, nil)
	c3.Mark()
	if drv3.Hash() == hash1 {
		t.Fatal("expected different hash for green fill")
	}
}

func TestCopyOp(t *testing.T) {
	drv := display.NewNullDriver()
	c := display.NewCompositor(drv)
	if err := c.CreateSurface(0, 8, 8, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary); err != nil {
		t.Fatal(err)
	}

	// 2x2 source: solid blue
	src := &codec.RGBA{
		Width: 2, Height: 2, Stride: 8,
		Pix: []byte{
			0, 0, 0xff, 0xff, 0, 0, 0xff, 0xff,
			0, 0, 0xff, 0xff, 0, 0, 0xff, 0xff,
		},
	}
	dest := image.Rect(3, 3, 5, 5)
	if err := c.Copy(0, dest, src, image.Pt(0, 0), nil); err != nil {
		t.Fatal(err)
	}
	c.Mark()
	pix, _, _, stride := drv.Snapshot()
	off := 3*stride + 3*4
	if pix[off+2] != 0xff || pix[off] != 0 {
		t.Fatalf("pixel at 3,3 = %v want blue", pix[off:off+4])
	}

	// Source too small for dest
	if err := c.Copy(0, image.Rect(0, 0, 4, 4), src, image.Pt(0, 0), nil); err == nil {
		t.Fatal("expected source bounds error")
	}
}

func TestValidateSurfaceSize(t *testing.T) {
	if err := display.ValidateSurfaceSize(1, 1); err != nil {
		t.Fatal(err)
	}
	if err := display.ValidateSurfaceSize(0, 1); err == nil {
		t.Fatal("want error")
	}
	if err := display.ValidateSurfaceSize(protocol.MaxSurfaceSide+1, 1); err == nil {
		t.Fatal("want error")
	}
}

func TestDestroyAndReset(t *testing.T) {
	c := display.NewCompositor(nil)
	_ = c.CreateSurface(1, 16, 16, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary)
	if err := c.DestroySurface(1); err != nil {
		t.Fatal(err)
	}
	if s := c.Surface(1); s != nil {
		t.Fatal("surface should be gone")
	}
	if c.TotalBytes() != 0 {
		t.Fatalf("total bytes after destroy: %d", c.TotalBytes())
	}
	_ = c.CreateSurface(2, 8, 8, protocol.SurfaceFmt32ARGB, 0)
	c.Reset()
	if s := c.Surface(2); s != nil {
		t.Fatal("reset should clear")
	}
	if c.TotalBytes() != 0 || c.SurfaceCount() != 0 {
		t.Fatal("reset should zero counters")
	}
}

func TestEmptyClipNoDraw(t *testing.T) {
	c := display.NewCompositor(nil)
	_ = c.CreateSurface(0, 4, 4, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary)
	// Green fill unclipped
	_ = c.Fill(0, image.Rect(0, 0, 4, 4), [4]byte{0, 0xff, 0, 0xff}, nil)
	// Red fill with empty clip list → no-op
	empty := []image.Rectangle{}
	if err := c.Fill(0, image.Rect(0, 0, 4, 4), [4]byte{0xff, 0, 0, 0xff}, empty); err != nil {
		t.Fatal(err)
	}
	s := c.Surface(0)
	if s.Pix[0] != 0 || s.Pix[1] != 0xff {
		t.Fatalf("empty clip overwrote pixels: %v", s.Pix[:4])
	}
}

func TestSurfaceCountCap(t *testing.T) {
	c := display.NewCompositor(nil)
	for i := 0; i < protocol.MaxSurfaces; i++ {
		if err := c.CreateSurface(uint32(i), 8, 8, protocol.SurfaceFmt32xRGB, 0); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	if err := c.CreateSurface(uint32(protocol.MaxSurfaces), 8, 8, protocol.SurfaceFmt32xRGB, 0); err == nil {
		t.Fatal("expected surface count reject")
	}
}

func TestCopyHashStable(t *testing.T) {
	src := &codec.RGBA{
		Width: 2, Height: 2, Stride: 8,
		Pix: []byte{
			0x10, 0x20, 0x30, 0xff, 0x10, 0x20, 0x30, 0xff,
			0x10, 0x20, 0x30, 0xff, 0x10, 0x20, 0x30, 0xff,
		},
	}
	run := func() string {
		drv := display.NewNullDriver()
		c := display.NewCompositor(drv)
		_ = c.CreateSurface(0, 2, 2, protocol.SurfaceFmt32xRGB, protocol.SurfaceFlagPrimary)
		_ = c.Copy(0, image.Rect(0, 0, 2, 2), src, image.Pt(0, 0), nil)
		c.Mark()
		return drv.Hash()
	}
	h1, h2 := run(), run()
	if h1 == "" || h1 != h2 {
		t.Fatalf("COPY hash unstable: %q %q", h1, h2)
	}
}
