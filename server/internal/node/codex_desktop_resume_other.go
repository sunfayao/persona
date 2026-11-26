//go:build !darwin && !linux

package node

// We cannot prove a native Desktop writer is released on this platform.
func codexDesktopWriterReleased(codexSource) bool { return false }
