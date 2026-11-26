//go:build !windows

package ports

import "errors"

func winListeners(map[int]string, map[string][]int, func(int) []string) ([]listener, error) {
	return nil, errors.New("no Windows TCP table here")
}
