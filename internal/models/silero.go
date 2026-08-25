package models

import (
	"fmt"

	ort "github.com/yalue/onnxruntime_go"
)

type SileroVAD struct {
	session *ort.DynamicAdvancedSession

	state []float32
	sr    []int64
}

func NewSilero(modelPath string) (*SileroVAD, error) {
	ort.SetSharedLibraryPath(
		"C:/Users/Ivan/Documents/Golang/Crawler/onnxruntime.dll",
	) // TODO: Make this path configurable or relative to the application directory

	if !ort.IsInitialized() {
		if err := ort.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("initialize ONNX Runtime: %w", err)
		}
	}

	session, err := ort.NewDynamicAdvancedSession(
		modelPath,
		[]string{
			"input",
			"state",
			"sr",
		},
		[]string{
			"output",
			"stateN",
		},
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create Silero session: %w", err)
	}

	return &SileroVAD{
		session: session,

		state: make([]float32, 2*1*128),

		sr: []int64{16000},
	}, nil
}

func (v *SileroVAD) IsSpeech(samples []float32) (float32, error) {
	input, err := ort.NewTensor(
		ort.NewShape(1, int64(len(samples))),
		samples,
	)
	if err != nil {
		return 0, fmt.Errorf("create input tensor: %w", err)
	}
	defer input.Destroy()

	state, err := ort.NewTensor(
		ort.NewShape(2, 1, 128),
		v.state,
	)
	if err != nil {
		return 0, fmt.Errorf("create state tensor: %w", err)
	}
	defer state.Destroy()

	sr, err := ort.NewTensor(
		ort.NewShape(1),
		v.sr,
	)
	if err != nil {
		return 0, fmt.Errorf("create sample rate tensor: %w", err)
	}
	defer sr.Destroy()

	outputs := make([]ort.Value, 2)

	if err := v.session.Run(
		[]ort.Value{
			input,
			state,
			sr,
		},
		outputs,
	); err != nil {
		return 0, fmt.Errorf("run Silero inference: %w", err)
	}

	defer func() {
		for _, output := range outputs {
			if output != nil {
				output.Destroy()
			}
		}
	}()

	out := outputs[0].(*ort.Tensor[float32])

	stateN := outputs[1].(*ort.Tensor[float32])

	copy(v.state, stateN.GetData())
	return out.GetData()[0], nil
}

func (v *SileroVAD) Reset() {
	v.state = make([]float32, 2*1*128)
}

func (v *SileroVAD) Destroy() {
	if v.session != nil {
		v.session.Destroy()
		v.session = nil
	}
}

func (v *SileroVAD) Type() string {
	return VADTypeSilero
}
