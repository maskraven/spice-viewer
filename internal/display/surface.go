// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package display

import (
	"fmt"
	"image"

	"github.com/maskraven/virt-viewer/internal/protocol"
)

// Surface is a client-side pixel buffer for one SPICE surface.
// Pixels are stored as tightly packed RGBA8888 (stride = width * 4).
type Surface struct {
	ID     uint32
	Width  int
	Height int
	Format uint32
	Flags  uint32
	Pix    []byte
	Stride int
}

// Primary reports whether the surface was created with PRIMARY flag.
func (s *Surface) Primary() bool {
	return s != nil && s.Flags&protocol.SurfaceFlagPrimary != 0
}

// Bounds returns the surface rectangle [0,0)→(w,h).
func (s *Surface) Bounds() image.Rectangle {
	if s == nil {
		return image.Rectangle{}
	}
	return image.Rect(0, 0, s.Width, s.Height)
}

// ValidateSurfaceSize checks Phase-1 surface dimension bounds.
func ValidateSurfaceSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("display: invalid surface size %dx%d", width, height)
	}
	if width > protocol.MaxSurfaceSide || height > protocol.MaxSurfaceSide {
		return fmt.Errorf("display: surface size %dx%d exceeds max side %d",
			width, height, protocol.MaxSurfaceSide)
	}
	bytes := int64(width) * int64(height) * 4
	if bytes > protocol.MaxSurfaceBytes {
		return fmt.Errorf("display: surface %dx%d (%d bytes) exceeds max %d",
			width, height, bytes, protocol.MaxSurfaceBytes)
	}
	return nil
}

// newSurface allocates an opaque-black RGBA surface after bounds checks.
func newSurface(id uint32, width, height int, format, flags uint32) (*Surface, error) {
	if err := ValidateSurfaceSize(width, height); err != nil {
		return nil, err
	}
	switch format {
	case protocol.SurfaceFmt32xRGB, protocol.SurfaceFmt32ARGB:
		// supported
	default:
		return nil, fmt.Errorf("display: unsupported surface format %d", format)
	}
	stride := width * 4
	pix := make([]byte, height*stride)
	for i := 3; i < len(pix); i += 4 {
		pix[i] = 0xff
	}
	return &Surface{
		ID:     id,
		Width:  width,
		Height: height,
		Format: format,
		Flags:  flags,
		Pix:    pix,
		Stride: stride,
	}, nil
}

// contains reports whether r is entirely inside the surface.
func (s *Surface) contains(r image.Rectangle) bool {
	if s == nil {
		return false
	}
	b := s.Bounds()
	return r.In(b)
}

// clipToSurface intersects r with the surface bounds.
func (s *Surface) clipToSurface(r image.Rectangle) image.Rectangle {
	if s == nil {
		return image.Rectangle{}
	}
	return r.Intersect(s.Bounds())
}
