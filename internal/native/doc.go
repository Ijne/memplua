// Package native registers platform capabilities with the application. Tagged
// Windows builds construct WASAPI, Silero, and Whisper sources; other builds
// register the same source names as unavailable so the UI remains predictable.
package native
