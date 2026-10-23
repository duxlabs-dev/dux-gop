//go:build darwin

package gops

import "strings"

func isVirtualFS(fstype string) bool {
	switch fstype {
	case "devfs", "autofs", "synthfs":
		return true
	}
	return false
}

func isVirtualMount(path string) bool {
	switch {
	case strings.HasPrefix(path, "/dev/"):
		return true
	}
	return false
}

func matchesDiskDevice(name string) bool {
	return strings.HasPrefix(name, "disk")
}

const diskBusySupported = false

const processIOSupported = true
