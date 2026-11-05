//go:build !windows

package dict

import (
	"os"
	"syscall"
)

func mmap(f *os.File, size int) ([]byte, error) {
	return syscall.Mmap(int(f.Fd()), 0, size, syscall.PROT_READ, syscall.MAP_SHARED)
}
