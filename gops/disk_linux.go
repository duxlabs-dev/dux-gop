//go:build linux

package gops

import "strings"

func isVirtualFS(fstype string) bool {
	switch fstype {
	case "tmpfs", "devtmpfs", "sysfs", "proc", "devpts",
		"cgroup", "cgroup2", "securityfs", "pstore",
		"efivarfs", "bpf", "autofs", "hugetlbfs",
		"mqueue", "debugfs", "tracefs", "fusectl",
		"configfs", "ramfs", "nsfs", "binfmt_misc",
		"fuse.gvfsd-fuse", "fuse.portal":
		return true
	}
	return false
}

func isVirtualMount(path string) bool {
	switch {
	case strings.HasPrefix(path, "/proc/"),
		strings.HasPrefix(path, "/sys/"),
		strings.HasPrefix(path, "/dev/"):
		return true
	}
	return false
}

func matchesDiskDevice(name string) bool {
	prefixes := []string{"sd", "nvme", "vd", "dm-", "mmcblk"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

const diskBusySupported = true
