//go:build !linux

package files

func fileReadIsSysfs(ReadHandle) (bool, error) { return false, nil }
