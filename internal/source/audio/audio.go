package audio

import (
	"encoding/binary"
	"math"
)

func Resample(buf []byte) []float32 {
	samples := make([]float32, len(buf)/8)

	for i := range samples {
		left := math.Float32frombits(
			binary.LittleEndian.Uint32(buf[i*8:]),
		)

		right := math.Float32frombits(
			binary.LittleEndian.Uint32(buf[i*8+4:]),
		)

		samples[i] = (left + right) / 2
	}

	resampled := make([]float32, len(samples)/3)

	for i := range resampled {
		resampled[i] = samples[i*3]
	}

	return resampled
}
