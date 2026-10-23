//go:build linux

package gops

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMatchesNetworkInterface(t *testing.T) {
	tests := []struct {
		name     string
		iface    string
		expected bool
	}{
		{"wireless wlan", "wlan0", true},
		{"wireless wlo", "wlo1", true},
		{"wireless wlp", "wlp2s0", true},
		{"ethernet eth", "eth0", true},
		{"ethernet eno", "eno1", true},
		{"ethernet enp", "enp3s0", true},
		{"ethernet ens", "ens33", true},
		{"lxc bridge", "lxcbr0", true},
		{"usb ethernet", "enx001122334455", true},
		{"usb wireless", "wlx001122334455", true},
		{"wireless generic", "wl0", true},
		{"usb network", "usb0", true},
		{"rndis tethering", "rndis0", true},
		{"cellular modem", "wwan0", true},
		{"android modem", "rmnet0", true},
		{"loopback", "lo", false},
		{"docker", "docker0", false},
		{"virbr", "virbr0", false},
		{"veth", "veth123abc", false},
		{"tun", "tun0", false},
		{"tap", "tap0", false},
		{"empty", "", false},
		{"random", "foobar", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesNetworkInterface(tt.iface)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesNetworkInterfaceRealWorld(t *testing.T) {
	validInterfaces := []string{
		"eth0", "eth1",
		"eno1", "eno2",
		"enp0s3", "enp0s25",
		"ens33", "ens192",
		"wlan0", "wlan1",
		"wlo1",
		"wlp3s0", "wlp4s0",
		"lxcbr0", "lxcbr1",
	}

	for _, iface := range validInterfaces {
		t.Run(iface, func(t *testing.T) {
			assert.True(t, matchesNetworkInterface(iface), "%s should match", iface)
		})
	}

	invalidInterfaces := []string{
		"lo",
		"docker0", "docker1",
		"virbr0", "virbr1",
		"veth0", "vethab123cd",
		"br-1234567890ab",
		"tun0", "tap0",
	}

	for _, iface := range invalidInterfaces {
		t.Run(iface, func(t *testing.T) {
			assert.False(t, matchesNetworkInterface(iface), "%s should not match", iface)
		})
	}
}

func BenchmarkMatchesNetworkInterface(b *testing.B) {
	testCases := []string{"eth0", "wlan0", "docker0", "lo", "enp3s0"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, tc := range testCases {
			matchesNetworkInterface(tc)
		}
	}
}

func TestReadLinkSysfs(t *testing.T) {
	root := t.TempDir()
	writeSys(t, root, "enp6s0/mtu", "1500")
	writeSys(t, root, "enp6s0/speed", "1000")
	writeSys(t, root, "enp6s0/duplex", "full")
	writeSys(t, root, "enp6s0/type", "1")
	writeSys(t, root, "drivers/igb/x", "")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "enp6s0", "device"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(root, "drivers", "igb"), filepath.Join(root, "enp6s0", "device", "driver")))
	writeSys(t, root, "tailscale0/mtu", "1280")
	writeSys(t, root, "tailscale0/speed", "10000")
	writeSys(t, root, "tailscale0/duplex", "full")
	writeSys(t, root, "tailscale0/type", "65534")
	writeSys(t, root, "wlan0/type", "1")
	writeSys(t, root, "wlan0/wireless/x", "")
	writeSys(t, root, "docker0/type", "1")
	writeSys(t, root, "lo/type", "772")

	read := func(name string) models.NetworkRateInfo {
		info := models.NetworkRateInfo{Interface: name}
		readLinkSysfs(root, &info)
		return info
	}
	assert.Equal(t, models.NetworkRateInfo{Interface: "enp6s0", MTU: 1500, Speed: 1000, Duplex: "full", LinkType: "ethernet", Driver: "igb"}, read("enp6s0"))
	assert.Equal(t, models.NetworkRateInfo{Interface: "tailscale0", MTU: 1280, LinkType: "tunnel"}, read("tailscale0"))
	assert.Equal(t, "wireless", read("wlan0").LinkType)
	assert.Equal(t, "other", read("docker0").LinkType)
	assert.Equal(t, "loopback", read("lo").LinkType)
	assert.Equal(t, models.NetworkRateInfo{Interface: "gone"}, read("gone"))
}
