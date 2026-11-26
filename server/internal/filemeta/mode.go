package filemeta

import "io/fs"

// UnixMode is a FileMode's permission bits as chmod takes them, with setuid, setgid and sticky.
func UnixMode(m fs.FileMode) int {
	bits := int(m.Perm())
	if m&fs.ModeSetuid != 0 {
		bits |= 0o4000
	}
	if m&fs.ModeSetgid != 0 {
		bits |= 0o2000
	}
	if m&fs.ModeSticky != 0 {
		bits |= 0o1000
	}
	return bits
}
