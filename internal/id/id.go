package id

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"sync/atomic"
	"time"
)

var fallbackSequence atomic.Uint64

// New returns an RFC 4122 version 4 UUID. A process-local hashed fallback keeps
// ID creation non-panicking if the operating-system random source is unavailable.
func New() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		var seed [24]byte
		binary.LittleEndian.PutUint64(seed[0:8], uint64(time.Now().UnixNano()))
		binary.LittleEndian.PutUint64(seed[8:16], uint64(os.Getpid()))
		binary.LittleEndian.PutUint64(seed[16:24], fallbackSequence.Add(1))
		digest := sha256.Sum256(seed[:])
		copy(value[:], digest[:16])
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80

	var encoded [36]byte
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded[:])
}
