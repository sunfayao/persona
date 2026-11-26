package textfmt

import "unicode/utf8"

// TrimPartialRune drops a UTF-8 sequence cut in half at the end of a truncated read.
func TrimPartialRune(b []byte) []byte {
	for i := 0; i < 4 && len(b) > 0; i++ {
		if utf8.Valid(b) {
			return b
		}
		b = b[:len(b)-1]
	}
	return b
}
