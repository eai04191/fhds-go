// Package udp parses Forza Horizon Data Out telemetry and listens on UDP.
package udp

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Telemetry holds the fields we actually use from the 324-byte Data Out frame.
// We don't decode every offset — only what triggers.go reads.
type Telemetry struct {
	On bool

	MaxRPM float32
	RPM    float32

	TireSlipRatio    [4]float32 // fl, fr, rl, rr
	TireCombinedSlip [4]float32 // fl, fr, rl, rr

	Speed float32 // km/h

	Accel     byte
	Brake     byte
	Handbrake byte
	Gear      int8 // raw byte, signed for "reverse"
}

func f32(p []byte, o int) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(p[o : o+4]))
}

func i32(p []byte, o int) int32 {
	return int32(binary.LittleEndian.Uint32(p[o : o+4]))
}

// ParsePacket decodes a Forza Data Out frame. Expects >= 323 bytes (the spec
// is 324 but we read up to offset 322 so 323 is the minimum we can accept).
func ParsePacket(p []byte) (Telemetry, error) {
	if len(p) < 323 {
		return Telemetry{}, fmt.Errorf("packet too short: %d", len(p))
	}
	t := Telemetry{
		On:     i32(p, 0) != 0,
		MaxRPM: f32(p, 8),
		RPM:    f32(p, 16),
		TireSlipRatio: [4]float32{
			f32(p, 84), f32(p, 88), f32(p, 92), f32(p, 96),
		},
		TireCombinedSlip: [4]float32{
			f32(p, 180), f32(p, 184), f32(p, 188), f32(p, 192),
		},
		Speed:     f32(p, 256) * 3.6,
		Accel:     p[315],
		Brake:     p[316],
		Handbrake: p[318],
		Gear:      int8(p[319]),
	}
	return t, nil
}
