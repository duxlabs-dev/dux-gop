package gops

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/disk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeDiskRateCursor(t *testing.T) {
	cursor := DiskRateCursor{
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		IOStats: map[string]disk.IOCountersStat{
			"sda": {
				Name:       "sda",
				ReadBytes:  1024000,
				WriteBytes: 2048000,
				ReadCount:  100,
				WriteCount: 200,
			},
		},
	}

	encoded, err := encodeDiskRateCursor(cursor)
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)

	decoded, err := parseDiskRateCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, cursor.Timestamp.Unix(), decoded.Timestamp.Unix())
	assert.Equal(t, cursor.IOStats["sda"].ReadBytes, decoded.IOStats["sda"].ReadBytes)
	assert.Equal(t, cursor.IOStats["sda"].WriteBytes, decoded.IOStats["sda"].WriteBytes)
}

func TestParseDiskRateCursor(t *testing.T) {
	tests := []struct {
		name      string
		cursorStr string
		wantErr   bool
	}{
		{
			name:      "invalid base64",
			cursorStr: "!!!invalid!!!",
			wantErr:   true,
		},
		{
			name:      "valid base64 but invalid json",
			cursorStr: "aGVsbG8=", // "hello" in base64
			wantErr:   true,
		},
		{
			name:      "empty string",
			cursorStr: "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseDiskRateCursor(tt.cursorStr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDiskRateCursorRoundTrip(t *testing.T) {
	original := DiskRateCursor{
		Timestamp: time.Now(),
		IOStats: map[string]disk.IOCountersStat{
			"sda": {
				Name:           "sda",
				ReadBytes:      1048576000,
				WriteBytes:     2097152000,
				ReadCount:      1000,
				WriteCount:     2000,
				ReadTime:       5000,
				WriteTime:      10000,
				IopsInProgress: 5,
			},
			"nvme0n1": {
				Name:           "nvme0n1",
				ReadBytes:      5242880000,
				WriteBytes:     10485760000,
				ReadCount:      5000,
				WriteCount:     10000,
				ReadTime:       2000,
				WriteTime:      4000,
				IopsInProgress: 2,
			},
		},
	}

	encoded, err := encodeDiskRateCursor(original)
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)

	decoded, err := parseDiskRateCursor(encoded)
	require.NoError(t, err)

	assert.Equal(t, original.Timestamp.Unix(), decoded.Timestamp.Unix())
	assert.Len(t, decoded.IOStats, 2)

	for name, stat := range original.IOStats {
		decodedStat, exists := decoded.IOStats[name]
		require.True(t, exists, "Disk %s should exist", name)
		assert.Equal(t, stat.ReadBytes, decodedStat.ReadBytes)
		assert.Equal(t, stat.WriteBytes, decodedStat.WriteBytes)
		assert.Equal(t, stat.ReadCount, decodedStat.ReadCount)
		assert.Equal(t, stat.WriteCount, decodedStat.WriteCount)
	}
}

func TestDiskRateCalculations(t *testing.T) {
	tests := []struct {
		name         string
		prevBytes    uint64
		currBytes    uint64
		timeDiffSecs float64
		expectedRate float64
	}{
		{
			name:         "100 MB written in 1 second",
			prevBytes:    0,
			currBytes:    104857600,
			timeDiffSecs: 1.0,
			expectedRate: 104857600.0,
		},
		{
			name:         "50 MB read in 2 seconds",
			prevBytes:    10485760,
			currBytes:    62914560,
			timeDiffSecs: 2.0,
			expectedRate: 26214400.0,
		},
		{
			name:         "no IO activity",
			prevBytes:    1000000,
			currBytes:    1000000,
			timeDiffSecs: 1.0,
			expectedRate: 0.0,
		},
		{
			name:         "high throughput SSD",
			prevBytes:    0,
			currBytes:    524288000,
			timeDiffSecs: 0.5,
			expectedRate: 1048576000.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := float64(tt.currBytes-tt.prevBytes) / tt.timeDiffSecs
			assert.InDelta(t, tt.expectedRate, rate, 0.01)
		})
	}
}

func TestDiskRateCursorWithMultipleDisks(t *testing.T) {
	cursor := DiskRateCursor{
		Timestamp: time.Now(),
		IOStats: map[string]disk.IOCountersStat{
			"sda":     {Name: "sda", ReadBytes: 1000, WriteBytes: 2000},
			"sdb":     {Name: "sdb", ReadBytes: 3000, WriteBytes: 4000},
			"nvme0n1": {Name: "nvme0n1", ReadBytes: 5000, WriteBytes: 6000},
		},
	}

	encoded, err := encodeDiskRateCursor(cursor)
	require.NoError(t, err)

	decoded, err := parseDiskRateCursor(encoded)
	require.NoError(t, err)

	assert.Len(t, decoded.IOStats, 3)
	assert.Contains(t, decoded.IOStats, "sda")
	assert.Contains(t, decoded.IOStats, "sdb")
	assert.Contains(t, decoded.IOStats, "nvme0n1")
}

func BenchmarkEncodeDiskRateCursor(b *testing.B) {
	cursor := DiskRateCursor{
		Timestamp: time.Now(),
		IOStats: map[string]disk.IOCountersStat{
			"sda": {
				Name:       "sda",
				ReadBytes:  1048576000,
				WriteBytes: 2097152000,
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encodeDiskRateCursor(cursor)
	}
}

func BenchmarkParseDiskRateCursor(b *testing.B) {
	cursor := DiskRateCursor{
		Timestamp: time.Now(),
		IOStats: map[string]disk.IOCountersStat{
			"sda": {
				Name:       "sda",
				ReadBytes:  1048576000,
				WriteBytes: 2097152000,
			},
		},
	}
	encoded, _ := encodeDiskRateCursor(cursor)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseDiskRateCursor(encoded)
	}
}

func TestDiskBusyAndResponse(t *testing.T) {
	prev := disk.IOCountersStat{IoTime: 1000, ReadCount: 100, WriteCount: 100, ReadTime: 50, WriteTime: 50}

	cur := prev
	cur.IoTime += 250
	cur.ReadCount += 6
	cur.WriteCount += 4
	cur.ReadTime += 3
	cur.WriteTime += 2
	busy, resp := diskBusyAndResponse(prev, cur, 1)
	require.NotNil(t, busy)
	require.NotNil(t, resp)
	assert.InDelta(t, 25, *busy, 1e-9)
	assert.InDelta(t, 0.5, *resp, 1e-9)

	busy, resp = diskBusyAndResponse(prev, prev, 1)
	require.NotNil(t, busy)
	assert.Zero(t, *busy)
	assert.Nil(t, resp, "no completed I/O leaves the response time unknown, not 0")

	cur = prev
	cur.IoTime += 5000
	busy, _ = diskBusyAndResponse(prev, cur, 1)
	require.NotNil(t, busy)
	assert.Equal(t, 100.0, *busy)

	cur = prev
	cur.IoTime = 10
	cur.ReadCount = 1
	busy, resp = diskBusyAndResponse(prev, cur, 1)
	assert.Nil(t, busy)
	assert.Nil(t, resp)
}

func TestOldFormatDiskCursorStillDecodes(t *testing.T) {
	raw := `{"timestamp":"2024-01-01T12:00:00Z","iostats":{"sda":{"readCount":1,"writeCount":2,"readBytes":3,"writeBytes":4,"name":"sda"}}}`
	cursor, err := parseDiskRateCursor(base64.StdEncoding.EncodeToString([]byte(raw)))
	require.NoError(t, err)
	assert.Equal(t, uint64(3), cursor.IOStats["sda"].ReadBytes)
}
