//go:build freebsd

package gops

import "strings"

func isVirtualFS(fstype string) bool {
	switch fstype {
	case "devfs", "fdescfs", "procfs", "linprocfs", "linsysfs",
		"tmpfs", "nullfs", "autofs":
		return true
	}
	return false
}

func isVirtualMount(path string) bool {
	switch {
	case strings.HasPrefix(path, "/dev/"),
		strings.HasPrefix(path, "/proc/"):
		return true
	}
	return false
}

// ada(4), da(4), nvd(4)/nda(4), mmcsd(4), vtbd (virtio_blk(4)).
func matchesDiskDevice(name string) bool {
	prefixes := []string{"ada", "da", "nvd", "nda", "mmcsd", "vtbd"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

const diskBusySupported = false

const processIOSupported = false
