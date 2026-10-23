//go:build linux

package gops

import (
	"bytes"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// readTrimmed reads a small file, such as a sysfs attribute, in three
// syscalls; os.ReadFile also stats it and registers it with the poller.
func readTrimmed(path string) string {
	var buf [4096]byte
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return ""
	}
	n, err := unix.Read(fd, buf[:])
	_ = unix.Close(fd)
	if err != nil {
		return ""
	}
	if n == len(buf) {
		b, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	return string(bytes.TrimSpace(buf[:n]))
}

func readInt(path string) (int64, bool) {
	n, err := strconv.ParseInt(readTrimmed(path), 10, 64)
	return n, err == nil
}

func readUint(path string) *uint64 {
	v, err := strconv.ParseUint(readTrimmed(path), 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func readKB(path string) *uint64 {
	v := readUint(path)
	if v != nil {
		*v /= 1024
	}
	return v
}

func readFloat(path string) *float64 {
	v, err := strconv.ParseFloat(readTrimmed(path), 64)
	if err != nil {
		return nil
	}
	return &v
}

// readScaled divides by div and treats values <= 0 as unset.
func readScaled(path string, div float64) *float64 {
	v := readFloat(path)
	if v == nil || *v <= 0 {
		return nil
	}
	return new(*v / div)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
