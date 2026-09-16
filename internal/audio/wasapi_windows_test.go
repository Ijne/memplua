//go:build windows && native

package audio

import (
	"testing"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

type emptyCaptureClient struct {
	getBufferCalls int
}

func (c *emptyCaptureClient) GetNextPacketSize(frames *uint32) error {
	*frames = 0
	return nil
}

func (c *emptyCaptureClient) GetBuffer(**byte, *uint32, *uint32, *uint64, *uint64) error {
	c.getBufferCalls++
	return nil
}

func (*emptyCaptureClient) ReleaseBuffer(uint32) error { return nil }

func TestReadCaptureDoesNotRequestAnEmptyBuffer(t *testing.T) {
	capture := &emptyCaptureClient{}
	samples, err := readCapture(capture, &wca.WAVEFORMATEX{})
	if err != nil {
		t.Fatalf("readCapture() error = %v", err)
	}
	if len(samples) != 0 {
		t.Fatalf("readCapture() returned %d samples, want 0", len(samples))
	}
	if capture.getBufferCalls != 0 {
		t.Fatalf("GetBuffer() calls = %d, want 0", capture.getBufferCalls)
	}
}

type statusCaptureClient struct{}

func (*statusCaptureClient) GetNextPacketSize(frames *uint32) error {
	*frames = 1
	return nil
}

func (*statusCaptureClient) GetBuffer(**byte, *uint32, *uint32, *uint64, *uint64) error {
	return ole.NewError(audclntSBufferEmpty)
}

func (*statusCaptureClient) ReleaseBuffer(uint32) error { return nil }

func TestReadCaptureAcceptsBufferEmptySuccessStatus(t *testing.T) {
	samples, err := readCapture(&statusCaptureClient{}, &wca.WAVEFORMATEX{})
	if err != nil {
		t.Fatalf("readCapture() error = %v", err)
	}
	if len(samples) != 0 {
		t.Fatalf("readCapture() returned %d samples, want 0", len(samples))
	}
}
