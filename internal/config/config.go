package config

const (
	VAD_SAMPLES int = 512

	SOUND_RECORDING_TICK int64 = 1 // Milleseconds

	SOUND_RECORDING_DURATION int64 = 3
	SOUND_BUFFER_SIZE        int64 = SOUND_RECORDING_DURATION * 48000 * 2 * 4 * 3 / 2
	MIN_SOUND_BUFFER_SIZE    int   = 48000 * 2 * 4 / 2
)
