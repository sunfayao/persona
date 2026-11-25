//go:build !darwin && !linux

package node

func (*Node) agyPresenceHeld(string) bool { return false }
