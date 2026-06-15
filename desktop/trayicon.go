package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// octopusTemplatePNG draws the XClaw / Octo mark as a monochrome template image
// for the macOS menu bar: alpha encodes the silhouette (macOS tints it
// white/dark to match the bar). It mirrors the Octo brand icon's motif — a bold
// ring "face" with two pill eyes and a wave of tentacles below — rather than the
// app-icon's color/wordmark, which would be illegible at bar size. Rendered at
// 2x (44px) for retina.
func octopusTemplatePNG() []byte {
	const s = 44
	img := image.NewRGBA(image.Rect(0, 0, s, s))
	black := color.RGBA{0, 0, 0, 255}
	clear := color.RGBA{0, 0, 0, 0}

	set := func(x, y int, c color.RGBA) {
		if x >= 0 && x < s && y >= 0 && y < s {
			img.Set(x, y, c)
		}
	}
	fillEllipse := func(cx, cy, rx, ry float64, c color.RGBA) {
		for y := 0; y < s; y++ {
			for x := 0; x < s; x++ {
				dx := (float64(x) + 0.5 - cx) / rx
				dy := (float64(y) + 0.5 - cy) / ry
				if dx*dx+dy*dy <= 1 {
					set(x, y, c)
				}
			}
		}
	}

	// Head: a bold ring (filled disc with a hollow centre), sitting in the
	// upper portion — the Octo "face".
	const hcx, hcy = 22.0, 16.0
	fillEllipse(hcx, hcy, 13, 12.5, black) // outer
	fillEllipse(hcx, hcy, 7.7, 7.4, clear) // hollow

	// Two pill eyes inside the lower centre of the ring.
	fillEllipse(18.4, 17.5, 1.9, 2.7, black)
	fillEllipse(25.6, 17.5, 1.9, 2.7, black)

	// Tentacle wave: a thick sinusoid below the head (≈3 humps, small gap).
	const (
		xa, xb = 7.0, 37.0
		waveY  = 36.0
		amp    = 4.0
		period = 10.0
		half   = 2.3 // half-thickness of the stroke
	)
	for x := 0; x < s; x++ {
		fx := float64(x) + 0.5
		if fx < xa || fx > xb {
			continue
		}
		cy := waveY + amp*math.Sin((fx-xa)/period*2*math.Pi)
		for y := 0; y < s; y++ {
			if math.Abs(float64(y)+0.5-cy) <= half {
				set(x, y, black)
			}
		}
	}

	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
