package gops

import (
	"testing"
	"time"

	"github.com/shirou/gopsutil/v4/net"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncodeNetworkRateCursor(t *testing.T) {
	cursor := NetworkRateCursor{
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		IOStats: map[string]net.IOCountersStat{
			"eth0": {
				Name:      "eth0",
				BytesSent: 1000,
				BytesRecv: 2000,
			},
		},
	}

	encoded, err := encodeNetworkRateCursor(cursor)
	require.NoError(t, err)
	assert.NotEmpty(t, encoded)

	decoded, err := parseNetworkRateCursor(encoded)
	require.NoError(t, err)
	assert.Equal(t, cursor.Timestamp.Unix(), decoded.Timestamp.Unix())
	assert.Equal(t, cursor.IOStats["eth0"].BytesSent, decoded.IOStats["eth0"].BytesSent)
	assert.Equal(t, cursor.IOStats["eth0"].BytesRecv, decoded.IOStats["eth0"].BytesRecv)
}

func TestParseNetworkRateCursor(t *testing.T) {
	tests := []struct {
		name      string
		cursorStr string
		wantErr   bool
	}{
		{
			name:      "invalid base64",
			cursorStr: "not-valid-base64!!!",
			wantErr:   true,
		},
		{
			name:      "invalid json",
			cursorStr: "aGVsbG8gd29ybGQ=", // "hello world" in base64
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
			_, err := parseNetworkRateCursor(tt.cursorStr)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestNetworkRateCursorRoundTrip(t *testing.T) {
	// Test multiple interfaces
	original := NetworkRateCursor{
		Timestamp: time.Now(),
		IOStats: map[string]net.IOCountersStat{
			"eth0": {
				Name:        "eth0",
				BytesSent:   123456,
				BytesRecv:   654321,
				PacketsSent: 100,
				PacketsRecv: 200,
			},
			"wlan0": {
				Name:        "wlan0",
				BytesSent:   789012,
				BytesRecv:   210987,
				PacketsSent: 50,
				PacketsRecv: 75,
			},
		},
	}

	// Encode
	encoded, err := encodeNetworkRateCursor(original)
	require.NoError(t, err)

	decoded, err := parseNetworkRateCursor(encoded)
	require.NoError(t, err)

	assert.Equal(t, original.Timestamp.Unix(), decoded.Timestamp.Unix())
	assert.Len(t, decoded.IOStats, 2)

	for name, stat := range original.IOStats {
		decodedStat, exists := decoded.IOStats[name]
		require.True(t, exists, "Interface %s should exist", name)
		assert.Equal(t, stat.BytesSent, decodedStat.BytesSent)
		assert.Equal(t, stat.BytesRecv, decodedStat.BytesRecv)
		assert.Equal(t, stat.PacketsSent, decodedStat.PacketsSent)
		assert.Equal(t, stat.PacketsRecv, decodedStat.PacketsRecv)
	}
}

func TestNetworkRateCalculations(t *testing.T) {
	tests := []struct {
		name         string
		prevBytes    uint64
		currBytes    uint64
		timeDiffSecs float64
		expectedRate float64
	}{
		{
			name:         "1 MB in 1 second",
			prevBytes:    0,
			currBytes:    1048576,
			timeDiffSecs: 1.0,
			expectedRate: 1048576.0,
		},
		{
			name:         "10 KB in 2 seconds",
			prevBytes:    1000,
			currBytes:    11240,
			timeDiffSecs: 2.0,
			expectedRate: 5120.0,
		},
		{
			name:         "no change",
			prevBytes:    1000,
			currBytes:    1000,
			timeDiffSecs: 1.0,
			expectedRate: 0.0,
		},
		{
			name:         "fractional second",
			prevBytes:    0,
			currBytes:    1024,
			timeDiffSecs: 0.5,
			expectedRate: 2048.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rate := float64(tt.currBytes-tt.prevBytes) / tt.timeDiffSecs
			assert.InDelta(t, tt.expectedRate, rate, 0.01)
		})
	}
}

func BenchmarkEncodeNetworkRateCursor(b *testing.B) {
	cursor := NetworkRateCursor{
		Timestamp: time.Now(),
		IOStats: map[string]net.IOCountersStat{
			"eth0": {
				Name:      "eth0",
				BytesSent: 123456,
				BytesRecv: 654321,
			},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encodeNetworkRateCursor(cursor)
	}
}

func BenchmarkParseNetworkRateCursor(b *testing.B) {
	cursor := NetworkRateCursor{
		Timestamp: time.Now(),
		IOStats: map[string]net.IOCountersStat{
			"eth0": {
				Name:      "eth0",
				BytesSent: 123456,
				BytesRecv: 654321,
			},
		},
	}
	encoded, _ := encodeNetworkRateCursor(cursor)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		parseNetworkRateCursor(encoded)
	}
}

func TestIncludeNetworkInterface(t *testing.T) {
	assert.False(t, includeNetworkInterface("tailscale0", false))
	assert.True(t, includeNetworkInterface("tailscale0", true))
	assert.True(t, includeNetworkInterface("lo", true))
	assert.Equal(t, matchesNetworkInterface("eth0"), includeNetworkInterface("eth0", false))
}
