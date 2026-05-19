// fhds — Forza Horizon DualSense adaptive triggers, headless Windows build.
//
//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/eai04191/fhds-go/internal/dualsense"
	"github.com/eai04191/fhds-go/internal/loop"
	"github.com/eai04191/fhds-go/internal/settings"
	"github.com/eai04191/fhds-go/internal/udp"
	"github.com/eai04191/fhds-go/internal/winhid"
)

func main() {
	host := flag.String("host", "127.0.0.1", "UDP bind address")
	port := flag.Int("port", 0, "UDP port (default 5300)")
	debug := flag.Bool("debug", false, "Verbose per-packet logs")
	listHID := flag.Bool("list-hid", false, "Dump every HID interface visible to this process and exit")
	flag.Parse()

	_ = debug // log level is single-tier for now; reserved for future use.

	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if *listHID {
		dumpHID()
		return
	}

	s := settings.Default()
	if *host != "" {
		s.UDPHost = *host
	}
	if *port != 0 {
		s.UDPPort = *port
	}

	ds := dualsense.New(dualsense.Options{
		StartupPulseForce:  s.StartupPulseForce,
		EnableStartupPulse: s.EnableStartupPulse,
		ReconnectIntervalS: s.ReconnectIntervalS,
		EnableReconnect:    s.EnableReconnect,
		HidHideDetected:    dualsense.HidHideDetected(),
	})
	ds.Open()
	defer ds.Close()

	l, err := udp.Open(s.UDPHost, s.UDPPort, time.Duration(s.UDPTimeout*float64(time.Second)))
	if err != nil {
		log.Fatalf("UDP open %s:%d failed: %v", s.UDPHost, s.UDPPort, err)
	}
	defer func() { _ = l.Close() }()

	log.Printf("Listening on %s:%d | Ctrl+C to quit", s.UDPHost, s.UDPPort)
	log.Printf("  In game: HUD & Gameplay -> Data Out: ON, IP 127.0.0.1, Port %d", s.UDPPort)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	loop.Run(ctx, ds, l, &s)
}

// dumpHID lists every HID interface this process can see — diagnostic for
// HidHide / reWASD whitelist troubleshooting.
func dumpHID() {
	if exe, err := os.Executable(); err == nil {
		fmt.Printf("My exe path (compare to HidHide whitelist entry):\n  %s\n\n", exe)
	}
	fmt.Printf("HidHide detected: %v\n", dualsense.HidHideDetected())
	all, err := winhid.EnumerateAll()
	if err != nil {
		fmt.Printf("EnumerateAll failed: %v\n", err)
		return
	}
	fmt.Printf("Found %d HID interface(s):\n\n", len(all))
	sonyCount := 0
	for i, e := range all {
		if e.Err != nil {
			fmt.Printf("[%3d] (open-failed) %v\n      path: %s\n\n", i, e.Err, e.Path)
			continue
		}
		marker := ""
		if e.Info.VendorID == 0x054C {
			marker = "  *** SONY ***"
			sonyCount++
		}
		fmt.Printf("[%3d] vid=0x%04x pid=0x%04x usagePage=%d usage=%d%s\n      path: %s\n\n",
			i, e.Info.VendorID, e.Info.ProductID,
			e.Info.UsagePage, e.Info.Usage, marker, e.Info.Path)
	}
	fmt.Printf("Sony devices visible: %d\n", sonyCount)
	if sonyCount == 0 {
		fmt.Println("\n=> Sony VID (0x054C) not visible from this exe. HidHide is hiding it,")
		fmt.Println("   or the controller is not connected at the Windows level.")
	}
}
