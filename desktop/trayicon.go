package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// xMarkTemplatePNG draws a bold, X-logo-style letterform as a monochrome
// template image for the macOS menu bar (macOS tints it white/dark to match the
// bar). Two thick diagonal bars with flat top/bottom caps cross to form the X.
// Rendered at 2x (44px) for retina, 3x-supersampled for clean diagonals.
func xMarkTemplatePNG() []byte {
	const s = 44
	img := image.NewRGBA(image.Rect(0, 0, s, s))

	// Bars span the padded box [L,R]x[T,B] with stroke width w and flat caps on
	// the top/bottom edges — the X-logo look.
	const (
		L, R = 5.0, 39.0
		T, B = 5.0, 39.0
		w    = 12.0
	)
	a := [4][2]float64{{L, T}, {L + w, T}, {R, B}, {R - w, B}} // top-left → bottom-right
	b := [4][2]float64{{R - w, T}, {R, T}, {L + w, B}, {L, B}} // top-right → bottom-left

	inside := func(q [4][2]float64, px, py float64) bool {
		sign := 0
		for i := 0; i < 4; i++ {
			x1, y1 := q[i][0], q[i][1]
			x2, y2 := q[(i+1)%4][0], q[(i+1)%4][1]
			cross := (x2-x1)*(py-y1) - (y2-y1)*(px-x1)
			cs := 0
			switch {
			case cross > 0:
				cs = 1
			case cross < 0:
				cs = -1
			}
			if cs != 0 {
				if sign == 0 {
					sign = cs
				} else if sign != cs {
					return false
				}
			}
		}
		return true
	}

	for y := 0; y < s; y++ {
		for x := 0; x < s; x++ {
			hits := 0
			for sy := 0; sy < 3; sy++ {
				for sx := 0; sx < 3; sx++ {
					px := float64(x) + (float64(sx)+0.5)/3
					py := float64(y) + (float64(sy)+0.5)/3
					if inside(a, px, py) || inside(b, px, py) {
						hits++
					}
				}
			}
			if hits > 0 {
				img.Set(x, y, color.RGBA{0, 0, 0, uint8(255 * hits / 9)})
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
