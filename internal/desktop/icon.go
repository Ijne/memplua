package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// trayIcon draws the same three geometric parts as the frontend SVG mark:
// track, boom, and indigo bucket wheel. It stays legible at Windows tray sizes.
func trayIcon() []byte {
	return brandIcon(32)
}

func brandIcon(size int) []byte {
	icon := image.NewRGBA(image.Rect(0, 0, size, size))
	ink, accent := color.RGBA{126, 132, 153, 255}, color.RGBA{129, 135, 255, 255}
	for y := range size {
		for x := range size {
			fx, fy := (float64(x)+0.5)*32/float64(size), (float64(y)+0.5)*32/float64(size)
			if (fy >= 23 && fy < 29 && fx >= 6 && fx <= 24) || math.Hypot(fx-6, fy-26) <= 3 || math.Hypot(fx-24, fy-26) <= 3 {
				icon.SetRGBA(x, y, ink)
			}
			if fx >= 9 && fx < 17 && fy >= 18 && fy < 23 {
				icon.SetRGBA(x, y, ink)
			}
			if fx >= 13 && fx <= 25 && math.Abs(fy-(31-fx)) < 1.6 {
				icon.SetRGBA(x, y, ink)
			}
			distance := math.Hypot(fx-24, fy-9)
			if (distance >= 4 && distance <= 6.2) || distance <= 1.6 {
				icon.SetRGBA(x, y, accent)
			}
			if distance <= 7.5 && distance >= 5 && (math.Abs(fx-24) < 1.1 || math.Abs(fy-9) < 1.1 || math.Abs(math.Abs(fx-24)-math.Abs(fy-9)) < 1.1) {
				icon.SetRGBA(x, y, accent)
			}
		}
	}
	var buffer bytes.Buffer
	_ = png.Encode(&buffer, icon)
	return buffer.Bytes()
}
