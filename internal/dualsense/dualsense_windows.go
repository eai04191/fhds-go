//go:build windows

package dualsense

import (
	"encoding/binary"
	"hash/crc32"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/eai04191/fhds-go/internal/winhid"
)

// VID / PID for DualSense and DualSense Edge.
const (
	vendorID             uint16 = 0x054C
	productDualSense     uint16 = 0x0CE6
	productDualSenseEdge uint16 = 0x0DF2
)

var productIDs = []uint16{productDualSense, productDualSenseEdge}

// valid_flag0 bits: 0x01 R-motor, 0x02 L-motor, 0x04 R-trigger, 0x08 L-trigger.
// Some firmware needs the motor bits set for trigger writes to take effect.
const trigFlags byte = 0x01 | 0x02 | 0x04 | 0x08

// Per-transport HID report layout. vf1 = valid_flag1, psav = power_save_control.
type layout struct {
	reportID byte
	flagsAt  int
	vf1At    int
	psavAt   int
	rightAt  int
	leftAt   int
	size     int
	bt       bool
}

var (
	layoutUSB = layout{reportID: 0x02, flagsAt: 1, vf1At: 2, psavAt: 10, rightAt: 11, leftAt: 22, size: 64, bt: false}
	layoutBT  = layout{reportID: 0x31, flagsAt: 2, vf1At: 3, psavAt: 11, rightAt: 12, leftAt: 23, size: 78, bt: true}
)

// Precomputed CRC of the BT report-header byte 0xA2 — crc32 resumes from this
// seed so we can CRC straight off the report buffer.
var btCRCSeed uint32

func init() {
	btCRCSeed = crc32.ChecksumIEEE([]byte{0xA2})
}

// DualSense is a triggers-only writer. Steam keeps rumble bytes untouched.
//
// Resilient: starts without a controller and retries every ReconnectIntervalS
// seconds. Drops writes silently while disconnected.
type DualSense struct {
	mu      sync.Mutex
	dev     *winhid.Device
	devPath string
	lay     layout
	left    Frame
	right   Frame
	dirty   bool
	running bool
	wake    chan struct{}
	doneCh  chan struct{}

	pulseForce         int
	enableStartupPulse bool
	reconnectInterval  time.Duration
	enableReconnect    bool

	everConnected bool
	openHinted    bool
	waitingHinted bool
	lastAttempt   time.Time

	inputIdleTimeout time.Duration
	lastInputAt      time.Time

	// HidHide-persistent: once latched, never disconnect.
	persistent bool
	hidhide    bool

	lastEnumCount int
}

// Options carries the values previously read from Settings.
type Options struct {
	StartupPulseForce  int
	EnableStartupPulse bool
	ReconnectIntervalS float64
	EnableReconnect    bool
	HidHideDetected    bool
}

func New(opts Options) *DualSense {
	return &DualSense{
		lay:                layoutUSB,
		wake:               make(chan struct{}, 1),
		doneCh:             make(chan struct{}),
		pulseForce:         opts.StartupPulseForce,
		enableStartupPulse: opts.EnableStartupPulse,
		reconnectInterval:  time.Duration(opts.ReconnectIntervalS * float64(time.Second)),
		enableReconnect:    opts.EnableReconnect,
		inputIdleTimeout:   3 * time.Second,
		hidhide:            opts.HidHideDetected,
		lastEnumCount:      -1,
		left:               OffFrame(),
		right:              OffFrame(),
	}
}

func (d *DualSense) Connected() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dev != nil
}

// Open starts the I/O goroutine. Never returns an error — the controller may
// be absent and the loop will keep retrying.
func (d *DualSense) Open() {
	if d.hidhide {
		log.Printf("HidHide: detected")
		log.Printf("Reconnect mode: persistent (HidHide present) — initial connect retries every %.0fs",
			d.reconnectInterval.Seconds())
	} else {
		log.Printf("HidHide: not detected")
		if d.enableReconnect {
			log.Printf("Reconnect mode: auto-reconnect every %.0fs after drops",
				d.reconnectInterval.Seconds())
		} else {
			log.Printf("Reconnect mode: disabled — initial connect retries every %.0fs, "+
				"drops will NOT auto-recover", d.reconnectInterval.Seconds())
		}
	}
	d.running = true
	go d.ioLoop()
}

// Close stops the I/O goroutine and releases the device.
func (d *DualSense) Close() {
	d.mu.Lock()
	d.running = false
	d.mu.Unlock()
	d.signalWake()
	<-d.doneCh
	d.disconnect("close")
}

// Set queues a new (left, right) frame for the next write.
func (d *DualSense) Set(left, right Frame) {
	d.mu.Lock()
	d.left = left
	d.right = right
	d.dirty = true
	d.mu.Unlock()
	d.signalWake()
}

func (d *DualSense) signalWake() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// ----- connect / disconnect -------------------------------------------------

func (d *DualSense) tryConnect() bool {
	devices, err := winhid.Enumerate(vendorID, productIDs)
	if err != nil {
		log.Printf("HID enumerate failed: %v", err)
		return false
	}
	if len(devices) != d.lastEnumCount {
		d.lastEnumCount = len(devices)
		if len(devices) == 0 {
			log.Printf("HID enumerate: 0 DualSense interfaces visible " +
				"(controller off, cable loose, or hidden by HidHide/Steam Input).")
		} else {
			var parts []string
			for _, dv := range devices {
				parts = append(parts, formatDeviceSummary(dv))
			}
			log.Printf("HID enumerate: %d DualSense interface(s): %s",
				len(devices), strings.Join(parts, ", "))
		}
	}

	info, ok := findGamepad(devices)
	if !ok {
		if len(devices) > 0 && !d.waitingHinted {
			log.Printf("DualSense interfaces present but none is the Game Pad " +
				"(usage_page=1, usage=5). Sensor/audio interfaces don't accept " +
				"trigger writes. Reconnect the controller.")
		}
		if !d.waitingHinted {
			log.Printf("Waiting for DualSense — retrying every %.0fs", d.reconnectInterval.Seconds())
			d.waitingHinted = true
		}
		return false
	}

	dev, err := winhid.Open(info.Path)
	if err != nil {
		if !d.openHinted {
			log.Printf("DualSense open failed (%v) — another app may be holding it open "+
				"(Steam Input, DS4Windows, reWASD).", err)
			d.openHinted = true
		}
		return false
	}

	d.mu.Lock()
	d.dev = dev
	d.devPath = info.Path
	if info.IsBluetooth() {
		d.lay = layoutBT
	} else {
		d.lay = layoutUSB
	}
	d.openHinted = false
	d.waitingHinted = false
	d.everConnected = true
	d.lastInputAt = time.Now()
	transport := "USB"
	if d.lay.bt {
		transport = "BT"
	}
	if d.hidhide && !d.persistent {
		d.persistent = true
		log.Printf("DualSense connected (%s) — persistent mode latched (HidHide present)", transport)
	} else {
		log.Printf("DualSense connected (%s)", transport)
	}
	d.mu.Unlock()

	if d.enableStartupPulse {
		pulse := Frame{Mode: ModeRigid, Params: [10]byte{0, byte(d.pulseForce)}, N: 2}
		d.safeWrite(d.build(pulse, pulse))
		time.Sleep(200 * time.Millisecond)
		d.safeWrite(d.build(OffFrame(), OffFrame()))
	}
	d.safeWrite(d.buildPowerSaver())
	return true
}

func (d *DualSense) disconnect(reason string) {
	d.mu.Lock()
	// HidHide-persistent: keep the handle, ignore transient errors forever.
	if d.persistent && d.running {
		d.mu.Unlock()
		return
	}
	wasConnected := d.dev != nil
	dev := d.dev
	d.dev = nil
	d.devPath = ""
	d.mu.Unlock()

	if wasConnected {
		// Best-effort: stop the active effect, then close.
		_, _ = dev.Write(d.build(OffFrame(), OffFrame()))
		_ = dev.Close()
	}
	if wasConnected {
		suffix := ""
		if reason != "" {
			suffix = " (" + reason + ")"
		}
		if d.enableReconnect {
			log.Printf("DualSense disconnected%s — retrying every %.0fs",
				suffix, d.reconnectInterval.Seconds())
		} else {
			log.Printf("DualSense disconnected%s — auto-reconnect is disabled.", suffix)
		}
	}
}

// safeWrite ignores errors. Used during startup pulse and the off-pulse on
// teardown, where the device may be about to disappear.
func (d *DualSense) safeWrite(buf []byte) {
	d.mu.Lock()
	dev := d.dev
	d.mu.Unlock()
	if dev == nil {
		return
	}
	_, _ = dev.Write(buf)
}

// ----- I/O loop -------------------------------------------------------------

func (d *DualSense) ioLoop() {
	defer close(d.doneCh)
	for {
		d.mu.Lock()
		running := d.running
		connected := d.dev != nil
		persistent := d.persistent
		d.mu.Unlock()
		if !running {
			return
		}

		now := time.Now()

		// Disconnected: throttled reconnect attempts.
		if !connected {
			if d.enableReconnect || !d.everConnected {
				if now.Sub(d.lastAttempt) >= d.reconnectInterval {
					d.lastAttempt = now
					d.tryConnect()
				}
			}
			d.waitWake(500 * time.Millisecond)
			continue
		}

		// Drain one input report for the liveness watchdog.
		d.mu.Lock()
		dev := d.dev
		size := d.lay.size
		d.mu.Unlock()
		if dev != nil {
			data, err := dev.ReadNonblocking(size)
			if err != nil {
				if !persistent {
					d.disconnect("read failed: " + err.Error())
					continue
				}
			} else if data != nil {
				d.lastInputAt = now
			} else if !persistent && now.Sub(d.lastInputAt) >= d.inputIdleTimeout {
				d.disconnect("no input for 3s")
				continue
			}
		}

		// Write the latest queued frame if any.
		d.mu.Lock()
		dirty := d.dirty
		left := d.left
		right := d.right
		d.dirty = false
		dev = d.dev
		d.mu.Unlock()
		if dirty && dev != nil {
			n, err := dev.Write(d.build(left, right))
			if err != nil {
				if !persistent {
					d.disconnect("write failed: " + err.Error())
					continue
				}
			} else if !persistent && n <= 0 {
				d.disconnect("write returned 0")
				continue
			}
		}
		d.waitWake(500 * time.Millisecond)
	}
}

func (d *DualSense) waitWake(timeout time.Duration) {
	select {
	case <-d.wake:
	case <-time.After(timeout):
	}
}

// ----- report builders ------------------------------------------------------

func (d *DualSense) newReport() []byte {
	buf := make([]byte, d.lay.size)
	buf[0] = d.lay.reportID
	if d.lay.bt {
		buf[1] = 0x02
	}
	return buf
}

func (d *DualSense) finalizeBTCRC(buf []byte) {
	if !d.lay.bt {
		return
	}
	c := crc32.Update(btCRCSeed, crc32.IEEETable, buf[:74])
	binary.LittleEndian.PutUint32(buf[74:78], c)
}

func (d *DualSense) build(left, right Frame) []byte {
	buf := d.newReport()
	buf[d.lay.flagsAt] = trigFlags
	writeFrame := func(at int, f Frame) {
		buf[at] = f.Mode
		// At most 10 param bytes — same slot count as Python.
		n := f.N
		if n > 10 {
			n = 10
		}
		copy(buf[at+1:at+1+n], f.Params[:n])
	}
	writeFrame(d.lay.rightAt, right)
	writeFrame(d.lay.leftAt, left)
	d.finalizeBTCRC(buf)
	return buf
}

func (d *DualSense) buildPowerSaver() []byte {
	buf := d.newReport()
	buf[d.lay.vf1At] |= 0x02  // bit 1 — POWER_SAVE_CONTROL enable
	buf[d.lay.psavAt] |= 0x10 // bit 4 — hardware power save
	d.finalizeBTCRC(buf)
	return buf
}

// ----- helpers --------------------------------------------------------------

func findGamepad(devices []winhid.DeviceInfo) (winhid.DeviceInfo, bool) {
	for _, d := range devices {
		// Game Pad interface = usage_page 1, usage 5.
		if d.UsagePage == 1 && d.Usage == 5 {
			return d, true
		}
	}
	if len(devices) > 0 {
		return devices[0], true
	}
	return winhid.DeviceInfo{}, false
}

func formatDeviceSummary(d winhid.DeviceInfo) string {
	transport := "USB"
	if d.IsBluetooth() {
		transport = "BT"
	}
	return "[pid=0x" + hex4(d.ProductID) +
		" up=" + dec(int(d.UsagePage)) +
		" u=" + dec(int(d.Usage)) +
		" " + transport + "]"
}

func hex4(v uint16) string {
	const hexdigits = "0123456789abcdef"
	return string([]byte{
		hexdigits[(v>>12)&0xF],
		hexdigits[(v>>8)&0xF],
		hexdigits[(v>>4)&0xF],
		hexdigits[v&0xF],
	})
}

func dec(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [12]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
