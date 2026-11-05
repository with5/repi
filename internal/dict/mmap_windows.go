//go:build windows

package dict

import (
	"errors"
	"os"
)

// mmap is unsupported here; Open falls back to reading the file.
func mmap(*os.File, int) ([]byte, error) { return nil, errors.New("mmap unsupported") }
