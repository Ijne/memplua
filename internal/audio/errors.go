package audio

import "errors"

// ErrNativeUnavailable explains that microphone/loopback requires a native build
// and configured speech model assets.
var ErrNativeUnavailable = errors.New("native audio support is unavailable; rebuild with -tags native and configure model assets")
