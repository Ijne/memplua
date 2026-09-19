package desktop

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

type iconPoint struct {
	x float64
	y float64
}

// trayIcon renders the application mark at the native Windows tray size.
func trayIcon() []byte {
	return brandIcon(32)
}

// brandIcon rasterizes the same four contours as the frontend SVG. Keeping the
// geometry here avoids shipping a second native image decoder solely for icons.
func brandIcon(size int) []byte {
	icon := image.NewRGBA(image.Rect(0, 0, size, size))
	ink := color.RGBA{94, 101, 232, 255}
	shapes := brandShapes()
	const samples = 4

	for y := range size {
		for x := range size {
			covered := 0
			for sampleY := range samples {
				for sampleX := range samples {
					canvasX := 87 + (float64(x)+(float64(sampleX)+0.5)/samples)*338/float64(size)
					canvasY := 99 + (float64(y)+(float64(sampleY)+0.5)/samples)*338/float64(size)
					if insideBrand(canvasX, canvasY, shapes) {
						covered++
					}
				}
			}
			if covered > 0 {
				ink.A = uint8(255 * covered / (samples * samples))
				icon.SetRGBA(x, y, ink)
			}
		}
	}

	var buffer bytes.Buffer
	_ = png.Encode(&buffer, icon)
	return buffer.Bytes()
}

func insideBrand(x, y float64, shapes [][]iconPoint) bool {
	for _, shape := range shapes {
		if insidePolygon(iconPoint{x: x, y: y}, shape) {
			return true
		}
	}
	pointX, pointY := 452.0-60, 171.0+133.5
	return math.Hypot(x-pointX, y-pointY) <= 32.5
}

func insidePolygon(point iconPoint, polygon []iconPoint) bool {
	inside := false
	previous := polygon[len(polygon)-1]
	for _, current := range polygon {
		crosses := (current.y > point.y) != (previous.y > point.y)
		if crosses && point.x < (previous.x-current.x)*(point.y-current.y)/(previous.y-current.y)+current.x {
			inside = !inside
		}
		previous = current
	}
	return inside
}

func brandShapes() [][]iconPoint {
	left := []iconPoint{translatedPoint(148, 202.5)}
	left = appendCubic(left, iconPoint{148, 202.5}, iconPoint{132.5, 191.8}, iconPoint{202.5, 98.5}, iconPoint{227.5, 98.5})
	left = appendCubic(left, iconPoint{227.5, 98.5}, iconPoint{253, 98.5}, iconPoint{270.5, 202.5}, iconPoint{227.5, 202.5})
	left = append(left, translatedPoint(148, 202.5))

	centre := []iconPoint{translatedPoint(274.2, 68)}
	centre = appendCubic(centre, iconPoint{274.2, 68}, iconPoint{285.1, 61.2}, iconPoint{371.5, 176.6}, iconPoint{353.3, 186.6})
	centre = appendCubic(centre, iconPoint{353.3, 186.6}, iconPoint{344.5, 191.5}, iconPoint{323, 202.5}, iconPoint{316, 202.5})
	centre = appendCubic(centre, iconPoint{316, 202.5}, iconPoint{308.5, 202.5}, iconPoint{269, 82}, iconPoint{223.2, 97})
	centre = appendCubic(centre, iconPoint{223.2, 97}, iconPoint{217.2, 99}, iconPoint{252.3, 82}, iconPoint{263.3, 75})
	centre = appendCubic(centre, iconPoint{263.3, 75}, iconPoint{266.9, 72.7}, iconPoint{270.6, 70.3}, iconPoint{274.2, 68})

	right := []iconPoint{translatedPoint(411.5, 202.5)}
	right = appendCubic(right, iconPoint{411.5, 202.5}, iconPoint{403, 202.5}, iconPoint{323, 109.5}, iconPoint{323, 96})
	right = appendCubic(right, iconPoint{323, 96}, iconPoint{323, 63.5}, iconPoint{411.5, 42.5}, iconPoint{411.5, 96.5})
	right = append(right, translatedPoint(411.5, 202.5))
	return [][]iconPoint{left, centre, right}
}

func appendCubic(points []iconPoint, start, controlA, controlB, end iconPoint) []iconPoint {
	const segments = 32
	for segment := 1; segment <= segments; segment++ {
		t := float64(segment) / segments
		inverse := 1 - t
		x := inverse*inverse*inverse*start.x + 3*inverse*inverse*t*controlA.x + 3*inverse*t*t*controlB.x + t*t*t*end.x
		y := inverse*inverse*inverse*start.y + 3*inverse*inverse*t*controlA.y + 3*inverse*t*t*controlB.y + t*t*t*end.y
		points = append(points, translatedPoint(x, y))
	}
	return points
}

func translatedPoint(x, y float64) iconPoint {
	return iconPoint{x: x - 60, y: y + 133.5}
}
