//go:build windows

package dualsense

import (
	"os"
	"os/exec"
	"path/filepath"
)

// HidHideDetected returns true when HidHide appears to be installed on the
// system. We do NOT call HidHideCLI; this only switches DualSense into
// persistent mode on first connect.
func HidHideDetected() bool {
	if env := os.Getenv("HIDHIDE_CLI"); env != "" {
		if st, err := os.Stat(env); err == nil && !st.IsDir() {
			return true
		}
	}
	if _, err := exec.LookPath("HidHideCLI.exe"); err == nil {
		return true
	}
	pf := os.Getenv("ProgramFiles")
	if pf == "" {
		pf = `C:\Program Files`
	}
	p := filepath.Join(pf, "Nefarius Software Solutions", "HidHide", "x64", "HidHideCLI.exe")
	if st, err := os.Stat(p); err == nil && !st.IsDir() {
		return true
	}
	return false
}
