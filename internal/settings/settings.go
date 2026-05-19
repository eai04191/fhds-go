// Package settings holds all tunables. Mirrors src/modules/settings.py.
package settings

// Settings is the full set of FH DualSense tunables.
// Forces are 0-255, frequencies in Hz.
type Settings struct {
	// UDP
	UDPHost    string
	UDPPort    int
	UDPTimeout float64

	// Shared pedal config
	PedalValueMax int
	WallZones     int

	// L2 — Brake
	EnableBrakeResistance bool
	BrakeDeadzone         int
	BrakeBaselineForce    int
	BrakeMaxForce         int
	BrakeCurve            float64
	BrakeWallEngageAt     int
	BrakeWallReleaseAt    int

	EnableHandbrakeBonus bool
	HandbrakeBonus       int

	EnableABS                bool
	ABSBrakeThreshold        int
	ABSMinSpeedKMH           float64
	ABSSlipRatioThreshold    float64
	ABSCombinedSlipThreshold float64
	ABSFreq                  int
	ABSAmp                   int

	// R2 — Throttle
	EnableThrottleResistance bool
	AccelDeadzone            int
	ThrottleBaselineForce    int
	ThrottleMaxForce         int
	ThrottleCurve            float64
	ThrottleWallEngageAt     int
	ThrottleWallReleaseAt    int

	EnableRevLimiter bool
	RevLimitRatio    float64
	RevLimitFreq     int
	RevLimitAmp      int
	RevLimitHoldMS   float64

	EnableGearShift      bool
	EnableGearShiftBrake bool
	GearShiftFreq        int
	GearShiftAmp         int
	GearShiftDurationMS  float64

	// System
	EnableStartupPulse bool
	StartupPulseForce  int

	EnableReconnect    bool
	ReconnectIntervalS float64

	ExitOnGameClose         bool
	GameProcessNameContains []string
	GamePollIntervalS       float64
	TelemetryLostExitS      float64
}

// Default returns the same defaults as the Python Settings dataclass.
func Default() Settings {
	return Settings{
		UDPHost:    "127.0.0.1",
		UDPPort:    5300,
		UDPTimeout: 0.5,

		PedalValueMax: 255,
		WallZones:     2,

		EnableBrakeResistance: true,
		BrakeDeadzone:         50,
		BrakeBaselineForce:    10,
		BrakeMaxForce:         60,
		BrakeCurve:            5.0,
		BrakeWallEngageAt:     250,
		BrakeWallReleaseAt:    200,

		EnableHandbrakeBonus: true,
		HandbrakeBonus:       25,

		EnableABS:                true,
		ABSBrakeThreshold:        80,
		ABSMinSpeedKMH:           15.0,
		ABSSlipRatioThreshold:    1.0,
		ABSCombinedSlipThreshold: 1.0,
		ABSFreq:                  10,
		ABSAmp:                   20,

		EnableThrottleResistance: true,
		AccelDeadzone:            50,
		ThrottleBaselineForce:    0,
		ThrottleMaxForce:         8,
		ThrottleCurve:            5.0,
		ThrottleWallEngageAt:     250,
		ThrottleWallReleaseAt:    200,

		EnableRevLimiter: true,
		RevLimitRatio:    0.93,
		RevLimitFreq:     20,
		RevLimitAmp:      1,
		RevLimitHoldMS:   120.0,

		EnableGearShift:      true,
		EnableGearShiftBrake: true,
		GearShiftFreq:        20,
		GearShiftAmp:         255,
		GearShiftDurationMS:  100.0,

		EnableStartupPulse: true,
		StartupPulseForce:  150,

		EnableReconnect:    false,
		ReconnectIntervalS: 5.0,

		ExitOnGameClose:         true,
		GameProcessNameContains: []string{"forza"},
		GamePollIntervalS:       1.0,
		TelemetryLostExitS:      60.0,
	}
}
