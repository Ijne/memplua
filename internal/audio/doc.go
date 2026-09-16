// Package audio defines realtime audio contracts and the Windows WASAPI source.
// Native capture is isolated behind build tags so the durable core can build and
// test without Whisper, ONNX Runtime, or Windows audio libraries.
package audio
