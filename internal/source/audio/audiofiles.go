package audio

import (
	"crawler/internal/AI"
	"crawler/internal/config"
	"crawler/internal/models"
	"crawler/models_storage"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hajimehoshi/go-mp3"
)

// FileSource читает аудиофайлы из директории и эмулирует потоковую обработку,
// аналогичную LoopbackSource, но с максимальной скоростью (без ожидания реального времени).
type FileSource struct {
	vad       AI.VAD
	extractor AI.Extractor
	stopChan  chan struct{}
	audioDir  string
}

// NewFileSource создаёт новый источник из файлов в указанной директории.
func NewFileSource(audioDir string) *FileSource {
	vad, err := AI.NewSilero(models_storage.SILERO)
	if err != nil {
		fmt.Printf("Error initializing VAD: %v\n", err)
		return nil
	}
	extractor := AI.NewWhisperExtractor()
	return &FileSource{
		vad:       vad,
		extractor: extractor,
		stopChan:  make(chan struct{}),
		audioDir:  audioDir,
	}
}

// Start не требуется для файлового источника.
func (f *FileSource) Start() error {
	return nil
}

// Stop сигнализирует о прекращении обработки.
func (f *FileSource) Stop() error {
	close(f.stopChan)
	return nil
}

// ProcessData запускает обработку файлов и возвращает канал с готовыми чанками.
func (f *FileSource) ProcessData() <-chan models.Chunk {
	chunks := make(chan models.Chunk)
	tasks := make(chan []float32, 1) // ограничиваем очередь одним элементом

	go Worker(tasks, chunks, f.extractor)

	go func() {
		defer close(tasks) // после завершения закроем канал задач, worker сам завершится

		files, err := filepath.Glob(filepath.Join(f.audioDir, "*.mp3"))
		if err != nil {
			fmt.Printf("Error globbing MP3 files: %v\n", err)
			return
		}

		for _, file := range files {
			select {
			case <-f.stopChan:
				return
			default:
				fmt.Printf("Processing %s\n", file)
				samples, err := decodeMP3ToFloat32Mono16k(file)
				if err != nil {
					fmt.Printf("Failed to decode %s: %v\n", file, err)
					continue
				}
				processSamples(samples, f.vad, tasks, f.stopChan)
			}
		}
	}()

	return chunks
}

// decodeMP3ToFloat32Mono16k декодирует MP3‑файл и возвращает моно‑сэмплы float32 с частотой 16 кГц.
func decodeMP3ToFloat32Mono16k(path string) ([]float32, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	decoder, err := mp3.NewDecoder(f)
	if err != nil {
		return nil, err
	}
	sampleRate := decoder.SampleRate()

	// Читаем весь PCM‑поток (16‑бит, стерео)
	pcmBytes, err := io.ReadAll(decoder)
	if err != nil {
		return nil, err
	}

	const (
		bytesPerSample = 2
		channels       = 2
	)
	bytesPerFrame := bytesPerSample * channels
	totalFrames := len(pcmBytes) / bytesPerFrame

	// Преобразуем в моно float32 на исходной частоте
	mono := make([]float32, totalFrames)
	for i := 0; i < totalFrames; i++ {
		off := i * bytesPerFrame
		left := int16(binary.LittleEndian.Uint16(pcmBytes[off : off+2]))
		right := int16(binary.LittleEndian.Uint16(pcmBytes[off+2 : off+4]))
		mono[i] = (float32(left) + float32(right)) / (2 * 32768.0)
	}

	// Если частота уже 16 кГц, возвращаем как есть
	if sampleRate == 16000 {
		return mono, nil
	}

	// Линейная интерполяция для понижения частоты до 16 кГц
	return resampleLinear(mono, sampleRate, 16000), nil
}

// resampleLinear выполняет линейную интерполяцию при изменении частоты дискретизации.
func resampleLinear(input []float32, srcRate, dstRate int) []float32 {
	if srcRate == dstRate || len(input) == 0 {
		return input
	}
	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(float64(len(input)) / ratio)
	output := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		srcIndex := float64(i) * ratio
		left := int(srcIndex)
		right := left + 1
		if right >= len(input) {
			right = len(input) - 1
		}
		frac := srcIndex - float64(left)
		output[i] = input[left]*(1-float32(frac)) + input[right]*float32(frac)
	}
	return output
}

// processSamples прогоняет сэмплы через VAD и отправляет речевые фрагменты в канал tasks.
func processSamples(samples []float32, vad AI.VAD, tasks chan<- []float32, stopChan <-chan struct{}) {
	const (
		windowSize    = config.VAD_SAMPLES // 512 сэмплов (32 мс)
		targetSamples = 480000             // 30 секунд речи при 16 кГц
		vadThreshold  = 0.00001            // порог вероятности речи (как в Loopback)
	)

	buffer := make([]float32, 0, targetSamples)

	for start := 0; start+windowSize <= len(samples); start += windowSize {
		window := samples[start : start+windowSize]
		prob, err := vad.IsSpeech(window)
		if err != nil {
			fmt.Printf("VAD error: %v\n", err)
			continue
		}
		if prob > vadThreshold {
			buffer = append(buffer, window...)
			if len(buffer) >= targetSamples {
				// Отправляем копию буфера, блокируясь, если очередь полна
				task := make([]float32, len(buffer))
				copy(task, buffer)
				select {
				case tasks <- task:
					buffer = buffer[:0]
				case <-stopChan:
					return
				}
			}
		}
	}

	// Отправляем остаток, если он есть
	if len(buffer) > 0 {
		task := make([]float32, len(buffer))
		copy(task, buffer)
		select {
		case tasks <- task:
		case <-stopChan:
			return
		}
	}
}
