// Package winhid is a minimal pure-Go HID layer for Windows.
//
// We only need three things from Win32 HID for FH DualSense:
//   - Enumerate HID devices and read each one's VID/PID + usage page/usage.
//   - Open the "game pad" interface (usage_page=1, usage=5) with overlapped I/O.
//   - Issue non-blocking Read/Write so Steam Input keeps writing rumble freely.
//
// Built only on Windows. No cgo. Uses golang.org/x/sys/windows for syscalls.
//
//go:build windows

package winhid

import (
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ----- Win32 type / constant declarations -----------------------------------

type guid = windows.GUID

const (
	digcfPresent         uint32 = 0x00000002
	digcfDeviceInterface uint32 = 0x00000010
)

type spDeviceInterfaceData struct {
	cbSize             uint32
	interfaceClassGuid guid
	flags              uint32
	reserved           uintptr
}

// SP_DEVICE_INTERFACE_DETAIL_DATA_W with an inline path buffer. The Win32 type
// is variable-length; we allocate a generous fixed size (rare device paths
// exceed a couple of hundred chars).
type spDeviceInterfaceDetail struct {
	cbSize     uint32
	devicePath [512]uint16
}

type hiddAttributes struct {
	size          uint32
	vendorID      uint16
	productID     uint16
	versionNumber uint16
}

type hidpCaps struct {
	usage                     uint16
	usagePage                 uint16
	inputReportByteLength     uint16
	outputReportByteLength    uint16
	featureReportByteLength   uint16
	reserved                  [17]uint16
	numberLinkCollectionNodes uint16
	numberInputButtonCaps     uint16
	numberInputValueCaps      uint16
	numberInputDataIndices    uint16
	numberOutputButtonCaps    uint16
	numberOutputValueCaps     uint16
	numberOutputDataIndices   uint16
	numberFeatureButtonCaps   uint16
	numberFeatureValueCaps    uint16
	numberFeatureDataIndices  uint16
}

// ----- DLL bindings ---------------------------------------------------------

var (
	hid      = windows.NewLazySystemDLL("hid.dll")
	setupapi = windows.NewLazySystemDLL("setupapi.dll")

	procHidDGetHidGuid        = hid.NewProc("HidD_GetHidGuid")
	procHidDGetAttributes     = hid.NewProc("HidD_GetAttributes")
	procHidDGetPreparsedData  = hid.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsedData = hid.NewProc("HidD_FreePreparsedData")
	procHidPGetCaps           = hid.NewProc("HidP_GetCaps")

	procSetupDiGetClassDevs              = setupapi.NewProc("SetupDiGetClassDevsW")
	procSetupDiDestroyDeviceInfoList     = setupapi.NewProc("SetupDiDestroyDeviceInfoList")
	procSetupDiEnumDeviceInterfaces      = setupapi.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = setupapi.NewProc("SetupDiGetDeviceInterfaceDetailW")
)

// ----- Public types ---------------------------------------------------------

// DeviceInfo is a snapshot of one HID interface returned by Enumerate.
type DeviceInfo struct {
	Path      string
	VendorID  uint16
	ProductID uint16
	UsagePage uint16
	Usage     uint16
}

// IsBluetooth heuristically classifies the device from its path. Windows HID
// device paths for BT-connected DualSense controllers contain "BTHENUM" or
// "BLUETOOTH"; USB paths contain "USB#".
func (d DeviceInfo) IsBluetooth() bool {
	up := strings.ToUpper(d.Path)
	return strings.Contains(up, "BTHENUM") || strings.Contains(up, "BLUETOOTH")
}

// Device is an opened HID handle with overlapped I/O state.
type Device struct {
	handle   windows.Handle
	readEvt  windows.Handle
	writeEvt windows.Handle
	readOv   windows.Overlapped
	writeOv  windows.Overlapped
	// State for the non-blocking read: when read returns ERROR_IO_PENDING we
	// keep the overlap "live" so the next call doesn't issue a second one.
	readPending bool
	readBuf     []byte
}

// ----- Enumeration ----------------------------------------------------------

func hidClassGUID() (guid, error) {
	var g guid
	// HidD_GetHidGuid is void; ignore syscall return values entirely.
	_, _, _ = procHidDGetHidGuid.Call(uintptr(unsafe.Pointer(&g)))
	return g, nil
}

// Enumerate returns all HID interfaces matching the requested vendor.
// productIDs filters by PID; pass nil to accept any product.
func Enumerate(vendorID uint16, productIDs []uint16) ([]DeviceInfo, error) {
	all, err := EnumerateAll()
	if err != nil {
		return nil, err
	}
	var out []DeviceInfo
	for _, e := range all {
		if e.Err != nil || e.Info.VendorID != vendorID {
			continue
		}
		if productIDs != nil {
			match := false
			for _, pid := range productIDs {
				if e.Info.ProductID == pid {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		out = append(out, e.Info)
	}
	return out, nil
}

// EnumEntry is one device-interface result. Path is always set; if Err is
// non-nil the device failed to open / report attributes and Info is zero.
type EnumEntry struct {
	Path string
	Info DeviceInfo
	Err  error
}

// EnumerateAll lists every HID interface in the system, including ones we
// couldn't open or query. Designed for diagnostics (--list-hid).
func EnumerateAll() ([]EnumEntry, error) {
	classGUID, err := hidClassGUID()
	if err != nil {
		return nil, err
	}

	h, _, e := procSetupDiGetClassDevs.Call(
		uintptr(unsafe.Pointer(&classGUID)),
		0, 0,
		uintptr(digcfPresent|digcfDeviceInterface),
	)
	if h == 0 || h == ^uintptr(0) {
		return nil, fmt.Errorf("SetupDiGetClassDevs: %v", e)
	}
	devInfoSet := windows.Handle(h)
	defer func() { _, _, _ = procSetupDiDestroyDeviceInfoList.Call(uintptr(devInfoSet)) }()

	var results []EnumEntry

	for index := uint32(0); ; index++ {
		var did spDeviceInterfaceData
		did.cbSize = uint32(unsafe.Sizeof(did))
		r, _, _ := procSetupDiEnumDeviceInterfaces.Call(
			uintptr(devInfoSet),
			0,
			uintptr(unsafe.Pointer(&classGUID)),
			uintptr(index),
			uintptr(unsafe.Pointer(&did)),
		)
		if r == 0 {
			break // ERROR_NO_MORE_ITEMS
		}

		var detail spDeviceInterfaceDetail
		detail.cbSize = 8 // x64 SDK constant

		var required uint32
		r, _, ge := procSetupDiGetDeviceInterfaceDetailW.Call(
			uintptr(devInfoSet),
			uintptr(unsafe.Pointer(&did)),
			uintptr(unsafe.Pointer(&detail)),
			unsafe.Sizeof(detail),
			uintptr(unsafe.Pointer(&required)),
			0,
		)
		if r == 0 {
			results = append(results, EnumEntry{Err: fmt.Errorf("SetupDiGetDeviceInterfaceDetailW: %v", ge)})
			continue
		}

		path := windows.UTF16ToString(detail.devicePath[:])
		info, qerr := queryHIDDetailsDebug(path)
		results = append(results, EnumEntry{Path: path, Info: info, Err: qerr})
	}
	return results, nil
}

// queryHIDDetailsDebug returns either the parsed info or an explanatory error.
func queryHIDDetailsDebug(path string) (DeviceInfo, error) {
	pPath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("UTF16PtrFromString: %w", err)
	}
	h, err := windows.CreateFile(
		pPath, 0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0,
	)
	if err != nil || h == windows.InvalidHandle {
		return DeviceInfo{Path: path}, fmt.Errorf("CreateFile: %w", err)
	}
	defer func() { _ = windows.CloseHandle(h) }()

	var attr hiddAttributes
	attr.size = uint32(unsafe.Sizeof(attr))
	r, _, ge := procHidDGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attr)))
	if r == 0 {
		return DeviceInfo{Path: path}, fmt.Errorf("HidD_GetAttributes failed: %v", ge)
	}

	info := DeviceInfo{
		Path:      path,
		VendorID:  attr.vendorID,
		ProductID: attr.productID,
	}

	var preparsed uintptr
	r, _, _ = procHidDGetPreparsedData.Call(uintptr(h), uintptr(unsafe.Pointer(&preparsed)))
	if r != 0 && preparsed != 0 {
		defer func() { _, _, _ = procHidDFreePreparsedData.Call(preparsed) }()
		var caps hidpCaps
		r, _, _ = procHidPGetCaps.Call(preparsed, uintptr(unsafe.Pointer(&caps)))
		_ = r
		info.UsagePage = caps.usagePage
		info.Usage = caps.usage
	}
	return info, nil
}

// ----- Open / Read / Write --------------------------------------------------

// Open opens the given device path for asynchronous (overlapped) HID I/O.
func Open(path string) (*Device, error) {
	pPath, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(
		pPath,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateFile %q: %w", path, err)
	}
	if h == windows.InvalidHandle {
		return nil, fmt.Errorf("CreateFile %q: invalid handle", path)
	}

	rEvt, err := windows.CreateEvent(nil, 1 /*manualReset*/, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("CreateEvent (read): %w", err)
	}
	wEvt, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(rEvt)
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("CreateEvent (write): %w", err)
	}

	return &Device{
		handle:   h,
		readEvt:  rEvt,
		writeEvt: wEvt,
		readOv:   windows.Overlapped{HEvent: rEvt},
		writeOv:  windows.Overlapped{HEvent: wEvt},
	}, nil
}

// Close releases the device and its overlapped event handles.
func (d *Device) Close() error {
	if d == nil || d.handle == 0 {
		return nil
	}
	// Cancel any pending I/O so we don't leak the buffer until Windows times out.
	_ = windows.CancelIoEx(d.handle, nil)
	if d.handle != 0 {
		_ = windows.CloseHandle(d.handle)
		d.handle = 0
	}
	if d.readEvt != 0 {
		_ = windows.CloseHandle(d.readEvt)
		d.readEvt = 0
	}
	if d.writeEvt != 0 {
		_ = windows.CloseHandle(d.writeEvt)
		d.writeEvt = 0
	}
	return nil
}

// Write sends an output report. Blocks until the controller acks the write so
// we know whether the device is still alive — but with overlapped I/O the
// HID stack does the actual transfer asynchronously, so this does not stall
// other writers (Steam Input).
//
// Returns the number of bytes written, or an error.
func (d *Device) Write(buf []byte) (int, error) {
	_ = windows.ResetEvent(d.writeEvt)
	d.writeOv = windows.Overlapped{HEvent: d.writeEvt}

	var written uint32
	err := windows.WriteFile(d.handle, buf, &written, &d.writeOv)
	if err != nil && !errors.Is(err, windows.ERROR_IO_PENDING) {
		return 0, err
	}
	// Wait for completion. Use a short timeout so a wedged stack doesn't pin us.
	wait, werr := windows.WaitForSingleObject(d.writeEvt, 100 /*ms*/)
	if werr != nil {
		return 0, werr
	}
	if wait == uint32(windows.WAIT_TIMEOUT) {
		_ = windows.CancelIoEx(d.handle, &d.writeOv)
		return 0, errors.New("HID write timed out")
	}
	if err := windows.GetOverlappedResult(d.handle, &d.writeOv, &written, false); err != nil {
		return 0, err
	}
	return int(written), nil
}

// ReadNonblocking returns the latest input report if one is available, or
// (nil, nil) immediately if none is pending. size is the report length.
//
// Implementation: we keep one overlapped read armed at a time. If the previous
// arm has completed, we drain it and re-arm. If it's still pending, we return
// (nil, nil) without waiting.
func (d *Device) ReadNonblocking(size int) ([]byte, error) {
	if !d.readPending {
		if cap(d.readBuf) < size {
			d.readBuf = make([]byte, size)
		} else {
			d.readBuf = d.readBuf[:size]
		}
		_ = windows.ResetEvent(d.readEvt)
		d.readOv = windows.Overlapped{HEvent: d.readEvt}
		var got uint32
		err := windows.ReadFile(d.handle, d.readBuf, &got, &d.readOv)
		if err == nil {
			// Completed synchronously.
			out := append([]byte(nil), d.readBuf[:got]...)
			return out, nil
		}
		if !errors.Is(err, windows.ERROR_IO_PENDING) {
			return nil, err
		}
		d.readPending = true
	}
	// Poll the event without blocking.
	wait, werr := windows.WaitForSingleObject(d.readEvt, 0)
	if werr != nil {
		return nil, werr
	}
	if wait == uint32(windows.WAIT_TIMEOUT) {
		return nil, nil
	}
	var got uint32
	if err := windows.GetOverlappedResult(d.handle, &d.readOv, &got, false); err != nil {
		d.readPending = false
		return nil, err
	}
	d.readPending = false
	out := append([]byte(nil), d.readBuf[:got]...)
	return out, nil
}
