// Package exitdetect watches for the Forza Horizon game process to disappear.
//
//go:build windows

package exitdetect

import (
	"log"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// Watcher polls the process list and reports when the named game has been
// seen-and-then-vanished.
type Watcher struct {
	needles      []string
	pollInterval time.Duration
	lastCheck    time.Time
	matched      string
}

func New(nameContains []string, pollInterval time.Duration) *Watcher {
	w := &Watcher{pollInterval: pollInterval}
	for _, n := range nameContains {
		w.needles = append(w.needles, strings.ToLower(n))
	}
	return w
}

// ShouldExit returns true once the watched process has been observed at least
// once and then disappeared.
func (w *Watcher) ShouldExit() bool {
	now := time.Now()
	if now.Sub(w.lastCheck) < w.pollInterval {
		return false
	}
	w.lastCheck = now
	found := w.find()
	if found != "" && w.matched == "" {
		w.matched = found
		log.Printf("Detected game process %q — will exit when it closes.", found)
		return false
	}
	if w.matched != "" && found == "" {
		log.Printf("Game process %q closed.", w.matched)
		return true
	}
	return false
}

// find returns the matched process name, or "" if none.
func (w *Watcher) find() string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer func() { _ = windows.CloseHandle(snap) }()

	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe_Sizeof_ProcessEntry32())
	if err := windows.Process32First(snap, &pe); err != nil {
		return ""
	}
	for {
		name := strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))
		for _, n := range w.needles {
			if strings.Contains(name, n) {
				return windows.UTF16ToString(pe.ExeFile[:])
			}
		}
		if err := windows.Process32Next(snap, &pe); err != nil {
			break
		}
	}
	return ""
}
