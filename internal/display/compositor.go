// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

package display

import (
	"fmt"
	"image"
	"sync"

	"github.com/maskraven/virt-viewer/internal/codec"
)

// Compositor owns surfaces and applies Phase-1 draw ops (FILL, COPY).
// When a Driver is set, Present/Invalidate are called after mutations on the
// primary surface.
type Compositor struct {
	mu       sync.Mutex
	surfaces map[uint32]*Surface
	primary  uint32
	hasPrim  bool
	driver   Driver
	marked   bool // true after DISPLAY_MARK
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

// CreateSurface allocates a surface. Rejects oversize dimensions and unknown formats.
func (c *Compositor) CreateSurface(id uint32, width, height int, format, flags uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, exists := c.surfaces[id]; exists {
		return fmt.Errorf("display: surface %d already exists", id)
	}
	s, err := newSurface(id, width, height, format, flags)
	if err != nil {
		return err
	}
	c.surfaces[id] = s
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
	if _, ok := c.surfaces[id]; !ok {
		return fmt.Errorf("display: surface %d not found", id)
	}
	delete(c.surfaces, id)
	if c.hasPrim && c.primary == id {
		c.hasPrim = false
		c.primary = 0
		// Pick another primary if any remain.
		for sid, s := range c.surfaces {
			if s.Primary() {
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
// clip rects (if non-empty) further restrict the filled region.
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
	for _, r := range regions {
		fillRect(s, r, rgba)
	}
	c.presentLocked(dest, false)
	return nil
}

// Copy blits src image pixels into surface dest.
// srcOrigin is the point in src corresponding to dest.Min (typically src_area.Min).
// dest must be fully inside the surface.
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

// presentLocked notifies the driver. full forces Present of entire primary buffer.
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

// clipRegions returns dest intersected with each clip, or [dest] if no clips.
func clipRegions(dest image.Rectangle, clips []image.Rectangle) []image.Rectangle {
	if len(clips) == 0 {
		return []image.Rectangle{dest}
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
