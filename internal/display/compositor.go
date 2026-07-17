// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package display

import (
	"fmt"
	"image"
	"sync"

	"github.com/maskraven/spice-viewer/internal/codec"
	"github.com/maskraven/spice-viewer/internal/protocol"
)

// Compositor owns surfaces and applies Phase-1 draw ops (FILL, COPY).
// When a Driver is set, Present/Invalidate are called after mutations on the
// primary surface.
//
// Memory bounds: per-surface MaxSurfaceBytes, plus MaxSurfaces and
// MaxTotalSurfaceBytes across all surfaces (see protocol package).
type Compositor struct {
	mu         sync.Mutex
	surfaces   map[uint32]*Surface
	primary    uint32
	hasPrim    bool
	driver     Driver
	marked     bool  // true after DISPLAY_MARK
	totalBytes int64 // sum of allocated surface pixel buffers
}

// NewCompositor builds an empty compositor. driver may be nil (ops still apply
// to surfaces; Present is skipped).
func NewCompositor(driver Driver) *Compositor {
	return &Compositor{
		surfaces: make(map[uint32]*Surface),
		driver:   driver,
	}
}

// SetDriver replaces the frame sink (may be nil).
func (c *Compositor) SetDriver(d Driver) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.driver = d
}

// Surface returns a surface by id, or nil.
func (c *Compositor) Surface(id uint32) *Surface {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.surfaces[id]
}

// PrimaryID returns the primary surface id and whether one exists.
func (c *Compositor) PrimaryID() (uint32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.primary, c.hasPrim
}

// SurfaceCount returns the number of live surfaces.
func (c *Compositor) SurfaceCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.surfaces)
}

// TotalBytes returns total allocated pixel memory across surfaces.
func (c *Compositor) TotalBytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalBytes
}

// CreateSurface allocates a surface. Rejects oversize dimensions, unknown
// formats, too many surfaces, or total memory over MaxTotalSurfaceBytes.
func (c *Compositor) CreateSurface(id uint32, width, height int, format, flags uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.surfaces[id]; exists {
		return fmt.Errorf("display: surface %d already exists", id)
	}
	if len(c.surfaces) >= protocol.MaxSurfaces {
		return fmt.Errorf("display: surface count limit %d reached", protocol.MaxSurfaces)
	}
	s, err := newSurface(id, width, height, format, flags)
	if err != nil {
		return err
	}
	need := int64(len(s.Pix))
	if c.totalBytes+need > protocol.MaxTotalSurfaceBytes {
		return fmt.Errorf("display: total surface memory %d+%d exceeds max %d",
			c.totalBytes, need, protocol.MaxTotalSurfaceBytes)
	}
	c.surfaces[id] = s
	c.totalBytes += need
	if s.Primary() || !c.hasPrim {
		c.primary = id
		c.hasPrim = true
		if c.driver != nil {
			c.driver.SetDesktopSize(width, height)
		}
	}
	return nil
}

// DestroySurface removes a surface.
func (c *Compositor) DestroySurface(id uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	s, ok := c.surfaces[id]
	if !ok {
		return fmt.Errorf("display: surface %d not found", id)
	}
	c.totalBytes -= int64(len(s.Pix))
	if c.totalBytes < 0 {
		c.totalBytes = 0
	}
	delete(c.surfaces, id)
	if c.hasPrim && c.primary == id {
		c.hasPrim = false
		c.primary = 0
		// Pick another primary if any remain.
		for sid, surf := range c.surfaces {
			if surf.Primary() {
				c.primary = sid
				c.hasPrim = true
				break
			}
		}
		if !c.hasPrim {
			for sid := range c.surfaces {
				c.primary = sid
				c.hasPrim = true
				break
			}
		}
	}
	return nil
}

// Reset destroys all surfaces (DISPLAY_RESET).
func (c *Compositor) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.surfaces = make(map[uint32]*Surface)
	c.primary = 0
	c.hasPrim = false
	c.marked = false
	c.totalBytes = 0
}

// Mark records DISPLAY_MARK and presents the primary surface if any.
func (c *Compositor) Mark() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.marked = true
	c.presentLocked(image.Rectangle{}, true)
}

// Fill solid-fills dest on surface id with rgba (4 bytes R,G,B,A).
// dest must be fully contained in the surface or an error is returned.
//
// Clip semantics (nil vs empty):
//   - clips == nil: no clipping; fill full dest
//   - clips non-nil but empty: no drawable region (no-op fill)
//   - clips non-empty: fill dest ∩ each clip rect
func (c *Compositor) Fill(surfaceID uint32, dest image.Rectangle, rgba [4]byte, clips []image.Rectangle) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.surfaces[surfaceID]
	if !ok {
		return fmt.Errorf("display: fill: surface %d not found", surfaceID)
	}
	if dest.Empty() {
		return nil
	}
	if !s.contains(dest) {
		return fmt.Errorf("display: fill: dest %v outside surface %dx%d", dest, s.Width, s.Height)
	}

	regions := clipRegions(dest, clips)
	if len(regions) == 0 {
		return nil
	}
	for _, r := range regions {
		fillRect(s, r, rgba)
	}
	c.presentLocked(dest, false)
	return nil
}

// Copy blits src image pixels into surface dest.
// srcOrigin is the point in src corresponding to dest.Min (typically src_area.Min).
// dest must be fully inside the surface. Clip semantics match Fill (nil vs empty).
func (c *Compositor) Copy(surfaceID uint32, dest image.Rectangle, src *codec.RGBA, srcOrigin image.Point, clips []image.Rectangle) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if src == nil {
		return fmt.Errorf("display: copy: nil source image")
	}
	s, ok := c.surfaces[surfaceID]
	if !ok {
		return fmt.Errorf("display: copy: surface %d not found", surfaceID)
	}
	if dest.Empty() {
		return nil
	}
	if !s.contains(dest) {
		return fmt.Errorf("display: copy: dest %v outside surface %dx%d", dest, s.Width, s.Height)
	}

	regions := clipRegions(dest, clips)
	if len(regions) == 0 {
		return nil
	}
	for _, r := range regions {
		// Offset into source relative to dest.Min mapping.
		dx := r.Min.X - dest.Min.X
		dy := r.Min.Y - dest.Min.Y
		srcPt := image.Pt(srcOrigin.X+dx, srcOrigin.Y+dy)
		if err := blit(s, r, src, srcPt); err != nil {
			return err
		}
	}
	c.presentLocked(dest, false)
	return nil
}

// CopyBits blits within a single surface from srcOrigin to dest (scroll).
// Handles overlapping regions safely (temp buffer when needed).
func (c *Compositor) CopyBits(surfaceID uint32, dest image.Rectangle, srcOrigin image.Point, clips []image.Rectangle) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	s, ok := c.surfaces[surfaceID]
	if !ok {
		return fmt.Errorf("display: copybits: surface %d not found", surfaceID)
	}
	if dest.Empty() {
		return nil
	}
	if !s.contains(dest) {
		return fmt.Errorf("display: copybits: dest %v outside surface %dx%d", dest, s.Width, s.Height)
	}
	srcRect := image.Rect(srcOrigin.X, srcOrigin.Y, srcOrigin.X+dest.Dx(), srcOrigin.Y+dest.Dy())
	if !s.contains(srcRect) {
		return fmt.Errorf("display: copybits: src %v outside surface", srcRect)
	}

	regions := clipRegions(dest, clips)
	if len(regions) == 0 {
		return nil
	}

	// Snapshot source into a temp image to handle overlap.
	tmp := &codec.RGBA{
		Width:  s.Width,
		Height: s.Height,
		Stride: s.Stride,
		Pix:    append([]byte(nil), s.Pix...),
	}
	for _, r := range regions {
		dx := r.Min.X - dest.Min.X
		dy := r.Min.Y - dest.Min.Y
		srcPt := image.Pt(srcOrigin.X+dx, srcOrigin.Y+dy)
		if err := blit(s, r, tmp, srcPt); err != nil {
			return err
		}
	}
	c.presentLocked(dest, false)
	return nil
}

// presentLocked notifies the driver. full forces Present of entire primary buffer.
// Always presents (does not wait for DISPLAY_MARK) so progressive updates show.
func (c *Compositor) presentLocked(dirty image.Rectangle, full bool) {
	if c.driver == nil || !c.hasPrim {
		return
	}
	s := c.surfaces[c.primary]
	if s == nil {
		return
	}
	if full || dirty.Empty() {
		dirty = s.Bounds()
	}
	c.driver.Present(s.Pix, s.Stride, dirty)
	c.driver.Invalidate(dirty)
}

// clipRegions returns dest regions after applying clips.
//
//	clips == nil  → no clipping (return [dest])
//	len(clips)==0 → empty clip list (return nil — draw nothing)
//	else          → dest ∩ each clip
func clipRegions(dest image.Rectangle, clips []image.Rectangle) []image.Rectangle {
	if clips == nil {
		return []image.Rectangle{dest}
	}
	if len(clips) == 0 {
		return nil
	}
	var out []image.Rectangle
	for _, c := range clips {
		r := dest.Intersect(c)
		if !r.Empty() {
			out = append(out, r)
		}
	}
	return out
}

func fillRect(s *Surface, r image.Rectangle, rgba [4]byte) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := y * s.Stride
		for x := r.Min.X; x < r.Max.X; x++ {
			off := row + x*4
			s.Pix[off+0] = rgba[0]
			s.Pix[off+1] = rgba[1]
			s.Pix[off+2] = rgba[2]
			s.Pix[off+3] = rgba[3]
		}
	}
}

func blit(dst *Surface, dest image.Rectangle, src *codec.RGBA, srcOrigin image.Point) error {
	// Source must cover the blit region.
	needW := dest.Dx()
	needH := dest.Dy()
	if srcOrigin.X < 0 || srcOrigin.Y < 0 ||
		srcOrigin.X+needW > src.Width || srcOrigin.Y+needH > src.Height {
		return fmt.Errorf("display: copy: source rect origin %v size %dx%d outside %dx%d image",
			srcOrigin, needW, needH, src.Width, src.Height)
	}
	for y := 0; y < needH; y++ {
		for x := 0; x < needW; x++ {
			si := (srcOrigin.Y+y)*src.Stride + (srcOrigin.X+x)*4
			di := (dest.Min.Y+y)*dst.Stride + (dest.Min.X+x)*4
			copy(dst.Pix[di:di+4], src.Pix[si:si+4])
		}
	}
	return nil
}
