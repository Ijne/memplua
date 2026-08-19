package audio

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

func GetMicrophoneSource() *MicrophoneSource {
	return &MicrophoneSource{
		stopChan: make(chan struct{}),
		data:     make(chan []byte),
	}
}

type MicrophoneSource struct {
	stopChan chan struct{}
	data     chan []byte
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
			case <-time.After(1 * time.Millisecond):
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

func (m *MicrophoneSource) Data() <-chan []byte {
	return m.data
}
