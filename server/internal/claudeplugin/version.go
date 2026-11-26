package claudeplugin

import (
	"strconv"
	"strings"
)

// VersionAtLeast says whether version v ("2.1.318", "2.1.0-beta") is min or later; false when it is not one.
func VersionAtLeast(v string, min [3]int) bool {
	parts := strings.SplitN(strings.SplitN(v, "-", 2)[0], ".", 3)
	if len(parts) != 3 {
		return false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return false
		}
		if n != min[i] {
			return n > min[i]
		}
	}
	return true
}
