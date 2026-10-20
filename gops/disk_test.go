package gops

import (
	"testing"

	"github.com/duxlabs-dev/dux-gop/gops/mocks"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		name     string
		bytes    uint64
		expected string
	}{
		{"zero bytes", 0, "0B"},
		{"single byte", 1, "1B"},
		{"bytes only", 512, "512B"},
		{"exact KB", 1024, "1.0K"},
		{"kilobytes", 1536, "1.5K"},
		{"exact MB", 1048576, "1.0M"},
		{"megabytes", 5242880, "5.0M"},
		{"fractional MB", 1572864, "1.5M"},
		{"exact GB", 1073741824, "1.0G"},
		{"gigabytes", 5368709120, "5.0G"},
		{"exact TB", 1099511627776, "1.0T"},
		{"terabytes", 2199023255552, "2.0T"},
		{"exact PB", 1125899906842624, "1.0P"},
		{"petabytes", 2251799813685248, "2.0P"},
		{"exact EB", 1152921504606846976, "1.0E"},
		{"large value", 18446744073709551615, "16.0E"}, // Max uint64
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFormatBytesRounding(t *testing.T) {
	tests := []struct {
		bytes    uint64
		contains string
	}{
		{1024 * 1024 * 100, "100.0M"},
		{1024 * 1024 * 1024 * 5, "5.0G"},
		{1024 * 1024 * 500, "500.0M"},
		{1024 * 1024 * 1024 * 10, "10.0G"},
	}

	for _, tt := range tests {
		t.Run(tt.contains, func(t *testing.T) {
			result := formatBytes(tt.bytes)
			assert.Equal(t, tt.contains, result)
		})
	}
}

func TestGetDiskMountsDedupesSharedDevice(t *testing.T) {
	mockDisk := mocks.NewMockDiskInfoProvider(t)

	mockDisk.EXPECT().Partitions(true).Return([]disk.PartitionStat{
		{Device: "/dev/nvme0n1p3", Mountpoint: "/", Fstype: "btrfs", Opts: []string{"rw", "subvol=/root"}},
		{Device: "/dev/nvme0n1p3", Mountpoint: "/home", Fstype: "btrfs", Opts: []string{"rw", "subvol=/home"}},
		{Device: "/dev/nvme0n1p1", Mountpoint: "/boot", Fstype: "vfat", Opts: []string{"rw"}},
		{Device: "proc", Mountpoint: "/proc", Fstype: "proc", Opts: []string{"rw"}},
	}, nil)

	mockDisk.EXPECT().Usage("/").Return(&disk.UsageStat{
		Total: 500 * 1024 * 1024 * 1024,
		Used:  200 * 1024 * 1024 * 1024,
		Free:  300 * 1024 * 1024 * 1024,
	}, nil)
	mockDisk.EXPECT().Usage("/boot").Return(&disk.UsageStat{
		Total: 1 * 1024 * 1024 * 1024,
		Used:  256 * 1024 * 1024,
		Free:  768 * 1024 * 1024,
	}, nil)

	g := &GopsUtil{diskProvider: mockDisk}
	mounts, err := g.GetDiskMounts()
	require.NoError(t, err)

	require.Len(t, mounts, 2)
	assert.Equal(t, "/dev/nvme0n1p3", mounts[0].Device)
	assert.Equal(t, "/", mounts[0].Mount)
	assert.Equal(t, "/dev/nvme0n1p1", mounts[1].Device)
	assert.Equal(t, "/boot", mounts[1].Mount)
}

func TestGetDiskMountsSkipsFailedUsage(t *testing.T) {
	mockDisk := mocks.NewMockDiskInfoProvider(t)

	mockDisk.EXPECT().Partitions(true).Return([]disk.PartitionStat{
		{Device: "/dev/sda1", Mountpoint: "/mnt/broken", Fstype: "ext4"},
		{Device: "/dev/sda2", Mountpoint: "/data", Fstype: "ext4"},
	}, nil)

	mockDisk.EXPECT().Usage("/mnt/broken").Return(nil, assert.AnError)
	mockDisk.EXPECT().Usage("/data").Return(&disk.UsageStat{
		Total: 100 * 1024 * 1024 * 1024,
		Used:  50 * 1024 * 1024 * 1024,
		Free:  50 * 1024 * 1024 * 1024,
	}, nil)

	g := &GopsUtil{diskProvider: mockDisk}
	mounts, err := g.GetDiskMounts()
	require.NoError(t, err)

	require.Len(t, mounts, 1)
	assert.Equal(t, "/dev/sda2", mounts[0].Device)
}

func TestGetDiskMountsKeepsFuseMountsSharingFilesystemName(t *testing.T) {
	mockDisk := mocks.NewMockDiskInfoProvider(t)

	mockDisk.EXPECT().Partitions(true).Return([]disk.PartitionStat{
		{Device: "fuse-overlayfs", Mountpoint: "/run/user/1000/psd/chromium", Fstype: "fuse.fuse-overlayfs", Opts: []string{"rw"}},
		{Device: "fuse-overlayfs", Mountpoint: "/run/user/1000/psd/zen-default", Fstype: "fuse.fuse-overlayfs", Opts: []string{"rw"}},
		{Device: "fuse-overlayfs", Mountpoint: "/run/user/1000/psd/zen-work", Fstype: "fuse.fuse-overlayfs", Opts: []string{"rw"}},
	}, nil)

	usage := &disk.UsageStat{
		Total: 2 * 1024 * 1024 * 1024,
		Used:  1 * 1024 * 1024 * 1024,
		Free:  1 * 1024 * 1024 * 1024,
	}
	mockDisk.EXPECT().Usage("/run/user/1000/psd/chromium").Return(usage, nil)
	mockDisk.EXPECT().Usage("/run/user/1000/psd/zen-default").Return(usage, nil)
	mockDisk.EXPECT().Usage("/run/user/1000/psd/zen-work").Return(usage, nil)

	g := &GopsUtil{diskProvider: mockDisk}
	mounts, err := g.GetDiskMounts()
	require.NoError(t, err)

	require.Len(t, mounts, 3)
	assert.Equal(t, "/run/user/1000/psd/chromium", mounts[0].Mount)
	assert.Equal(t, "/run/user/1000/psd/zen-default", mounts[1].Mount)
	assert.Equal(t, "/run/user/1000/psd/zen-work", mounts[2].Mount)
}

func TestGetDiskMountsDedupesRepeatedNonBlockMountpoint(t *testing.T) {
	mockDisk := mocks.NewMockDiskInfoProvider(t)

	mockDisk.EXPECT().Partitions(true).Return([]disk.PartitionStat{
		{Device: "tank/data", Mountpoint: "/mnt/data", Fstype: "zfs", Opts: []string{"rw"}},
		{Device: "tank/data", Mountpoint: "/mnt/data", Fstype: "zfs", Opts: []string{"rw"}},
	}, nil)

	mockDisk.EXPECT().Usage("/mnt/data").Return(&disk.UsageStat{
		Total: 10 * 1024 * 1024 * 1024,
		Used:  4 * 1024 * 1024 * 1024,
		Free:  6 * 1024 * 1024 * 1024,
	}, nil).Once()

	g := &GopsUtil{diskProvider: mockDisk}
	mounts, err := g.GetDiskMounts()
	require.NoError(t, err)

	require.Len(t, mounts, 1)
	assert.Equal(t, "tank/data", mounts[0].Device)
}
