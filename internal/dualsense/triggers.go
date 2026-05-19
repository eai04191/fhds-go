// Package dualsense implements adaptive-trigger effects and the HID writer.
// triggers.go is a 1:1 port of src/modules/dualsense/triggers.py.
package dualsense

import (
	"math"
	"time"

	"github.com/eai04191/fhds-go/internal/settings"
	"github.com/eai04191/fhds-go/internal/udp"
)

// Raw HID effect-mode bytes.
const (
	ModeOff      byte = 0x05
	ModeRigid    byte = 0x01
	ModePulse    byte = 0x06
	ModeFeedback byte = 0x21 // MultiplePositionFeedback
	ModePulseAB  byte = 0x26 // Pulse_AB
	rawMax            = 255
)

// Frame is one trigger's HID effect: mode byte + up to 10 param bytes.
type Frame struct {
	Mode   byte
	Params [10]byte
	N      int // number of valid bytes in Params
}

func (f Frame) Equal(o Frame) bool {
	if f.Mode != o.Mode || f.N != o.N {
		return false
	}
	for i := 0; i < f.N; i++ {
		if f.Params[i] != o.Params[i] {
			return false
		}
	}
	return true
}

func clampByte(v float64) byte {
	r := int(math.Round(v))
	if r < 0 {
		return 0
	}
	if r > rawMax {
		return rawMax
	}
	return byte(r)
}

// OffFrame returns the neutral "no effect" frame.
func OffFrame() Frame { return Frame{Mode: ModeOff} }

func rigid(force float64) Frame {
	return Frame{Mode: ModeRigid, Params: [10]byte{0, clampByte(force)}, N: 2}
}

func vibration(freq, amp float64) Frame {
	return Frame{Mode: ModePulse, Params: [10]byte{clampByte(freq), clampByte(amp)}, N: 2}
}

func vibrationWall(amp, freq, wallZones int) Frame {
	a := amp
	if a < 1 {
		a = 1
	} else if a > 8 {
		a = 8
	}
	w := wallZones
	if w < 1 {
		w = 1
	} else if w > 9 {
		w = 9
	}
	zones := make([]int, 10)
	for i := 0; i < 10-w; i++ {
		zones[i] = a
	}
	for i := 10 - w; i < 10; i++ {
		zones[i] = 8
	}
	active := 0
	strength := 0
	for i, s := range zones {
		active |= 1 << i
		strength |= (s - 1) << (3 * i)
	}
	var p [10]byte
	p[0] = byte(active & 0xFF)
	p[1] = byte((active >> 8) & 0xFF)
	p[2] = byte(strength & 0xFF)
	p[3] = byte((strength >> 8) & 0xFF)
	p[4] = byte((strength >> 16) & 0xFF)
	p[5] = byte((strength >> 24) & 0xFF)
	p[6] = clampByte(float64(freq))
	return Frame{Mode: ModePulseAB, Params: p, N: 10}
}

func feedback(zones [10]int) Frame {
	active := 0
	force := 0
	for i, s := range zones {
		if s < 0 {
			s = 0
		} else if s > 8 {
			s = 8
		}
		if s > 0 {
			active |= 1 << i
			force |= (s - 1) << (3 * i)
		}
	}
	var p [10]byte
	p[0] = byte(active & 0xFF)
	p[1] = byte((active >> 8) & 0xFF)
	p[2] = byte(force & 0xFF)
	p[3] = byte((force >> 8) & 0xFF)
	p[4] = byte((force >> 16) & 0xFF)
	p[5] = byte((force >> 24) & 0xFF)
	return Frame{Mode: ModeFeedback, Params: p, N: 10}
}

// BuildWall is a static firmware-wall frame: top `zones` slots maxed.
func BuildWall(zones int) Frame {
	n := zones
	if n < 1 {
		n = 1
	} else if n > 9 {
		n = 9
	}
	var z [10]int
	for i := 10 - n; i < 10; i++ {
		z[i] = 8
	}
	return feedback(z)
}

// ----- helpers --------------------------------------------------------------

func ampToStrength(ampByte int) int {
	if ampByte < 0 {
		ampByte = 0
	}
	v := ampByte/32 + 1
	if v < 1 {
		v = 1
	} else if v > 8 {
		v = 8
	}
	return v
}

func maxSlip(values [4]float32) float32 {
	m := float32(0)
	for _, v := range values {
		a := v
		if a < 0 {
			a = -a
		}
		if a > m {
			m = a
		}
	}
	return m
}

func ramp(value float64, deadzone, baseline, maxForce int, curve float64, ceiling int) float64 {
	if value < float64(deadzone) {
		return float64(baseline)
	}
	span := float64(ceiling - deadzone)
	if span < 1 {
		span = 1
	}
	r := (value - float64(deadzone)) / span
	if r > 1.0 {
		r = 1.0
	}
	return float64(baseline) + float64(maxForce-baseline)*math.Pow(r, curve)
}

func wallState(value int, engaged bool, engageAt, releaseAt int) bool {
	if engaged {
		return value >= releaseAt
	}
	return value >= engageAt
}

// ----- TriggerAnimations ----------------------------------------------------

type TriggerAnimations struct {
	prevGear   int8
	prevGearOK bool
	shiftUntil time.Time
	revUntil   time.Time
}

func (a *TriggerAnimations) ArmShift(t *udp.Telemetry, s *settings.Settings, now time.Time) {
	gear := t.Gear
	if a.prevGearOK && gear != a.prevGear {
		a.shiftUntil = now.Add(time.Duration(s.GearShiftDurationMS * float64(time.Millisecond)))
	}
	a.prevGear = gear
	a.prevGearOK = true
}

func (a *TriggerAnimations) ShiftBurst(s *settings.Settings, now time.Time, pedal int, wallEngageAt int) (Frame, bool) {
	if !now.Before(a.shiftUntil) {
		return Frame{}, false
	}
	if pedal >= (wallEngageAt+rawMax)/2 {
		return vibrationWall(ampToStrength(s.GearShiftAmp), s.GearShiftFreq, s.WallZones), true
	}
	return vibration(float64(s.GearShiftFreq), float64(s.GearShiftAmp)), true
}

func (a *TriggerAnimations) RevBuzz(t *udp.Telemetry, s *settings.Settings, now time.Time) (Frame, bool) {
	if !s.EnableRevLimiter {
		return Frame{}, false
	}
	if int(t.Accel) >= s.AccelDeadzone {
		var ratio float64
		if t.MaxRPM > 0 {
			ratio = float64(t.RPM) / float64(t.MaxRPM)
		}
		if ratio > s.RevLimitRatio {
			a.revUntil = now.Add(time.Duration(s.RevLimitHoldMS * float64(time.Millisecond)))
		}
	}
	if now.Before(a.revUntil) {
		return vibration(float64(s.RevLimitFreq), float64(s.RevLimitAmp)), true
	}
	return Frame{}, false
}

func (a *TriggerAnimations) ABSPulse(t *udp.Telemetry, s *settings.Settings) (Frame, bool) {
	if !s.EnableABS {
		return Frame{}, false
	}
	if int(t.Brake) < s.ABSBrakeThreshold || float64(t.Speed) < s.ABSMinSpeedKMH {
		return Frame{}, false
	}
	slipR := maxSlip(t.TireSlipRatio)
	slipC := maxSlip(t.TireCombinedSlip)
	if float64(slipR) < s.ABSSlipRatioThreshold && float64(slipC) < s.ABSCombinedSlipThreshold {
		return Frame{}, false
	}
	return vibration(float64(s.ABSFreq), float64(s.ABSAmp)), true
}

func (a *TriggerAnimations) BrakeResistance(t *udp.Telemetry, s *settings.Settings) Frame {
	handbrake := s.EnableHandbrakeBonus && t.Handbrake != 0
	if !s.EnableBrakeResistance {
		if handbrake {
			return rigid(float64(s.HandbrakeBonus))
		}
		return OffFrame()
	}
	force := ramp(float64(t.Brake), s.BrakeDeadzone, s.BrakeBaselineForce,
		s.BrakeMaxForce, s.BrakeCurve, s.BrakeWallEngageAt)
	if handbrake {
		force += float64(s.HandbrakeBonus)
	}
	return rigid(force)
}

func (a *TriggerAnimations) ThrottleRamp(t *udp.Telemetry, s *settings.Settings) Frame {
	if !s.EnableThrottleResistance {
		return OffFrame()
	}
	return rigid(ramp(float64(t.Accel), s.AccelDeadzone, s.ThrottleBaselineForce,
		s.ThrottleMaxForce, s.ThrottleCurve, s.ThrottleWallEngageAt))
}

// ----- Controller -----------------------------------------------------------

// Controller resolves the per-tick priority chain for both triggers.
type Controller struct {
	anim      TriggerAnimations
	wall      Frame
	wallZones int
	l2InWall  bool
	r2InWall  bool
}

func NewController(s *settings.Settings) *Controller {
	return &Controller{wall: BuildWall(s.WallZones), wallZones: s.WallZones}
}

func (c *Controller) Update(t *udp.Telemetry, s *settings.Settings) (left, right Frame) {
	if !t.On {
		return OffFrame(), OffFrame()
	}
	if s.WallZones != c.wallZones {
		c.wall = BuildWall(s.WallZones)
		c.wallZones = s.WallZones
	}
	now := time.Now()
	if s.EnableGearShift || s.EnableGearShiftBrake {
		c.anim.ArmShift(t, s, now)
	}
	return c.l2(t, s, now), c.r2(t, s, now)
}

func (c *Controller) l2(t *udp.Telemetry, s *settings.Settings, now time.Time) Frame {
	brake := int(t.Brake)
	if s.EnableGearShiftBrake {
		if f, ok := c.anim.ShiftBurst(s, now, brake, s.BrakeWallEngageAt); ok {
			return f
		}
	}
	if f, ok := c.anim.ABSPulse(t, s); ok {
		return f
	}
	c.l2InWall = wallState(brake, c.l2InWall, s.BrakeWallEngageAt, s.BrakeWallReleaseAt)
	if c.l2InWall {
		return c.wall
	}
	return c.anim.BrakeResistance(t, s)
}

func (c *Controller) r2(t *udp.Telemetry, s *settings.Settings, now time.Time) Frame {
	accel := int(t.Accel)
	if s.EnableGearShift {
		if f, ok := c.anim.ShiftBurst(s, now, accel, s.ThrottleWallEngageAt); ok {
			return f
		}
	}
	if f, ok := c.anim.RevBuzz(t, s, now); ok {
		return f
	}
	c.r2InWall = wallState(accel, c.r2InWall, s.ThrottleWallEngageAt, s.ThrottleWallReleaseAt)
	if c.r2InWall {
		return c.wall
	}
	return c.anim.ThrottleRamp(t, s)
}
