package audio

import (
	"crawler/internal/config"
	"crawler/internal/data"
	"crawler/internal/models"
	"crawler/models_storage"
	"fmt"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

func GetMicrophoneSource() *MicrophoneSource {
	vad, _ := models.NewSilero(models_storage.SILERO) // TODO: Handle error properly
	extractor := models.NewWhisperExtractor()

	mic := &MicrophoneSource{
		vad:       vad,
		extractor: extractor,
		stopChan:  make(chan struct{}),
		data:      make(chan []byte),
	}

	return mic
}

type MicrophoneSource struct {
	vad       models.VAD
	extractor models.Extractor
	stopChan  chan struct{}
	data      chan []byte
}

func (m *MicrophoneSource) Start() error {
	ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)

	var mmde *wca.IMMDeviceEnumerator
	wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &mmde)

	var mmd *wca.IMMDevice
	mmde.GetDefaultAudioEndpoint(wca.ECapture, wca.EConsole, &mmd)

	var ac *wca.IAudioClient
	mmd.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &ac)

	var wfx *wca.WAVEFORMATEX
	ac.GetMixFormat(&wfx)

	var defaultPeriod wca.REFERENCE_TIME
	var minimumPeriod wca.REFERENCE_TIME
	ac.GetDevicePeriod(&defaultPeriod, &minimumPeriod)

	if err := ac.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, 0, defaultPeriod, 0, wfx, nil); err != nil {
		return fmt.Errorf("Start error: %v", err)
	}
	if err := ac.Start(); err != nil {
		return fmt.Errorf("Start error: %v", err)
	}

	var acc *wca.IAudioCaptureClient
	if err := ac.GetService(wca.IID_IAudioCaptureClient, &acc); err != nil {
		return fmt.Errorf("Start error: %v", err)
	}

	var data *byte
	var availableFrameSize uint32
	var flags uint32
	var devicePosition, qcpPosition uint64

	go func() {
		defer ole.CoUninitialize()
		defer mmde.Release()
		defer mmd.Release()
		defer acc.Release()

		for {
			select {
			case <-m.stopChan:
				return
			case <-time.After(time.Duration(config.SOUND_RECORDING_TICK) * time.Millisecond):
				acc.GetBuffer(&data, &availableFrameSize, &flags, &devicePosition, &qcpPosition)

				if availableFrameSize == 0 {
					continue
				}

				if flags&wca.AUDCLNT_BUFFERFLAGS_SILENT != 0 {
					acc.ReleaseBuffer(availableFrameSize)
					continue
				}

				start := unsafe.Pointer(data)
				lim := int(availableFrameSize) * int(wfx.NBlockAlign)
				buf := make([]byte, lim)

				for n := 0; n < lim; n++ {
					b := (*byte)(unsafe.Pointer(uintptr(start) + uintptr(n)))
					buf[n] = *b
				}

				m.data <- buf
				acc.ReleaseBuffer(availableFrameSize)
			}
		}
	}()

	return nil
}

func (m *MicrophoneSource) Stop() error {
	close(m.stopChan)
	return nil
}

func (m *MicrophoneSource) ProcessData() <-chan data.Chunk {
	chunks := make(chan data.Chunk)

	go func() {
		defer close(chunks)

		const vadWindowSize = 512
		const speechThreshold = float32(0.5)
		const silenceLimit = 15

		speechBuf := make([]float32, 0, 16000*30)
		vadWindow := make([]float32, 0, vadWindowSize)
		silenceCount := 0
		inSpeech := false

		for raw := range m.data {
			resampled := Resample(raw)

			for _, sample := range resampled {
				vadWindow = append(vadWindow, sample)

				if len(vadWindow) < vadWindowSize {
					continue
				}

				window := make([]float32, len(vadWindow))
				copy(window, vadWindow)
				vadWindow = vadWindow[:0]

				prob, err := m.vad.IsSpeech(window)
				if err != nil {
					continue
				}

				if prob >= speechThreshold {
					inSpeech = true
					silenceCount = 0
					speechBuf = append(speechBuf, window...)
				} else if inSpeech {
					silenceCount++
					speechBuf = append(speechBuf, window...)

					if silenceCount >= silenceLimit {
						snapshot := make([]float32, len(speechBuf))
						copy(snapshot, speechBuf)
						speechBuf = speechBuf[:0]
						silenceCount = 0
						inSpeech = false
						m.vad.Reset()

						chunk, err := m.extractor.Extract(snapshot)
						if err != nil {
							continue
						}
						chunks <- chunk
					}
				}
			}
		}
	}()

	return chunks
}
