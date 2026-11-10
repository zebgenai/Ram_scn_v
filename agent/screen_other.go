//go:build !windows

package main

import "fmt"

// captureScreenJPEG is only implemented on Windows; the managed PCs are Windows.
func captureScreenJPEG(quality int) ([]byte, error) {
	return nil, fmt.Errorf("screen capture is only supported on Windows")
}

// notifyViewing is a no-op off Windows (no system-tray indicator there).
func notifyViewing(on bool) {}
