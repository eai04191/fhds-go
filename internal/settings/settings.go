// Package settings holds all tunables. Mirrors src/modules/settings.py.
package settings

// Settings is the full set of FH DualSense tunables.
// Forces are 0-255, frequencies in Hz.
// TOML tags use snake_case to match the Python field names so configs are
// recognizable to anyone coming from the upstream project.
type Settings struct {
	// UDP
	UDPHost    string  `toml:"udp_host"`
	UDPPort    int     `toml:"udp_port"`
	UDPTimeout float64 `toml:"udp_timeout"`

	// Shared pedal config
	PedalValueMax int `toml:"pedal_value_max"`
	WallZones     int `toml:"wall_zones"`

	// L2 — Brake
	EnableBrakeResistance bool    `toml:"enable_brake_resistance"`
	BrakeDeadzone         int     `toml:"brake_deadzone"`
	BrakeBaselineForce    int     `toml:"brake_baseline_force"`
	BrakeMaxForce         int     `toml:"brake_max_force"`
	BrakeCurve            float64 `toml:"brake_curve"`
	BrakeWallEngageAt     int     `toml:"brake_wall_engage_at"`
	BrakeWallReleaseAt    int     `toml:"brake_wall_release_at"`

	EnableHandbrakeBonus bool `toml:"enable_handbrake_bonus"`
	HandbrakeBonus       int  `toml:"handbrake_bonus"`

	EnableABS                bool    `toml:"enable_abs"`
	ABSBrakeThreshold        int     `toml:"abs_brake_threshold"`
	ABSMinSpeedKMH           float64 `toml:"abs_min_speed_kmh"`
	ABSSlipRatioThreshold    float64 `toml:"abs_slip_ratio_threshold"`
	ABSCombinedSlipThreshold float64 `toml:"abs_combined_slip_threshold"`
	ABSFreq                  int     `toml:"abs_freq"`
	ABSAmp                   int     `toml:"abs_amp"`

	// R2 — Throttle
	EnableThrottleResistance bool    `toml:"enable_throttle_resistance"`
	AccelDeadzone            int     `toml:"accel_deadzone"`
	ThrottleBaselineForce    int     `toml:"throttle_baseline_force"`
	ThrottleMaxForce         int     `toml:"throttle_max_force"`
	ThrottleCurve            float64 `toml:"throttle_curve"`
	ThrottleWallEngageAt     int     `toml:"throttle_wall_engage_at"`
	ThrottleWallReleaseAt    int     `toml:"throttle_wall_release_at"`

	EnableRevLimiter bool    `toml:"enable_rev_limiter"`
	RevLimitRatio    float64 `toml:"rev_limit_ratio"`
	RevLimitFreq     int     `toml:"rev_limit_freq"`
	RevLimitAmp      int     `toml:"rev_limit_amp"`
	RevLimitHoldMS   float64 `toml:"rev_limit_hold_ms"`

	EnableGearShift      bool    `toml:"enable_gear_shift"`
	EnableGearShiftBrake bool    `toml:"enable_gear_shift_brake"`
	GearShiftFreq        int     `toml:"gear_shift_freq"`
	GearShiftAmp         int     `toml:"gear_shift_amp"`
	GearShiftDurationMS  float64 `toml:"gear_shift_duration_ms"`

	// System
	EnableStartupPulse bool `toml:"enable_startup_pulse"`
	StartupPulseForce  int  `toml:"startup_pulse_force"`

	EnableReconnect    bool    `toml:"enable_reconnect"`
	ReconnectIntervalS float64 `toml:"reconnect_interval_s"`

	ExitOnGameClose         bool     `toml:"exit_on_game_close"`
	GameProcessNameContains []string `toml:"game_process_name_contains"`
	GamePollIntervalS       float64  `toml:"game_poll_interval_s"`
	TelemetryLostExitS      float64  `toml:"telemetry_lost_exit_s"`
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
