//go:build !windows || !native

package native

import (
	"crawler/internal/app"
	"crawler/internal/audio"
	"crawler/internal/config"
)

// RegisterSources exposes unavailable microphone and loopback descriptors in
// builds that do not include Windows native audio support.
func RegisterSources(manager *app.SourceManager, _ config.Config) {
	for _, sourceID := range []string{"microphone", "loopback"} {
		manager.Register(sourceID, app.SourceFactory{
			Kind: "audio/" + sourceID, Available: false, Reason: audio.ErrNativeUnavailable.Error(),
		})
	}
}
