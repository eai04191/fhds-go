// Package loop is the per-frame control loop. Mirrors src/modules/loop.py.
package loop

import (
	"context"
	"log"
	"time"

	"github.com/eai04191/fhds-go/internal/dualsense"
	"github.com/eai04191/fhds-go/internal/exitdetect"
	"github.com/eai04191/fhds-go/internal/settings"
	"github.com/eai04191/fhds-go/internal/udp"
)

// Run drives the trigger loop until ctx is cancelled or the watched game exits.
// Settings are read from the store every frame so hot-reload takes effect on
// the next packet without restarting the backend.
func Run(ctx context.Context, ds *dualsense.DualSense, l *udp.Listener, store *settings.Store) {
	off := dualsense.OffFrame()
	ctrl := dualsense.NewController(store.Get())

	var prevL, prevR dualsense.Frame
	prevSet := false

	lastPkt := time.Now()
	lastLog := time.Time{}
	pktCount := 0

	// Exit-watcher config is captured once at startup — game_process_name_contains
	// and game_poll_interval_s are not hot-reloaded.
	var watcher *exitdetect.Watcher
	if init := store.Get(); init.ExitOnGameClose {
		watcher = exitdetect.New(init.GameProcessNameContains,
			time.Duration(init.GamePollIntervalS*float64(time.Second)))
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		s := store.Get()
		now := time.Now()
		if watcher != nil && watcher.ShouldExit() {
			log.Printf("Game process closed — exiting.")
			return
		}

		pkt, addr, err := l.RecvLatest()
		if err != nil {
			log.Printf("UDP read error: %v", err)
			continue
		}

		if pkt == nil {
			idle := now.Sub(lastPkt)
			if idle > 5*time.Second && !l.Lost() {
				log.Printf("No UDP packets yet — check Forza Horizon Data Out IP/port and Windows Firewall")
				l.SetLost(true)
			}
			if idle > time.Second && (!prevSet || !prevL.Equal(off) || !prevR.Equal(off)) {
				ds.Set(off, off)
				prevL, prevR = off, off
				prevSet = true
			}
			if pktCount > 0 && idle > time.Duration(s.TelemetryLostExitS*float64(time.Second)) {
				log.Printf("Telemetry lost for %.0fs — exiting.", idle.Seconds())
				return
			}
			continue
		}

		pktCount++
		lastPkt = now
		l.SetLost(false)
		if pktCount == 1 {
			log.Printf("First packet from %s (%d bytes)", addr, len(pkt))
		}

		t, err := udp.ParsePacket(pkt)
		if err != nil {
			log.Printf("Bad packet from %s (%d bytes): %v", addr, len(pkt), err)
			continue
		}

		left, right := ctrl.Update(&t, s)
		if !prevSet || !left.Equal(prevL) || !right.Equal(prevR) {
			ds.Set(left, right)
			prevL, prevR = left, right
			prevSet = true
		}

		if now.Sub(lastLog) >= time.Second {
			lastLog = now
			tag := "MENU"
			if t.On {
				tag = "RACE"
			}
			log.Printf("[%s] %6.1f km/h | gear %d | gas %3d | brake %3d", tag, t.Speed, t.Gear, t.Accel, t.Brake)
		}
	}
}
