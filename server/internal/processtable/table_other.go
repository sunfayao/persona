//go:build !windows

package processtable

import "errors"

func winProcs() (map[int]Process, error) { return nil, errors.New("no Windows process table here") }
