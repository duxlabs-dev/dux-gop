package gops

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func encodeProcessCursor(entries []processSample) string {
	return encodeCursor(entries, netBaseline{})
}

func decodeProcessCursor(cursor string) map[int32]*processSample {
	m, _ := decodeCursor(cursor)
	return m
}

func TestCalculateProcessCPUPercentageWithCursor(t *testing.T) {
	baseTime := time.Now().UnixMilli()

	tests := []struct {
		name           string
		cursor         *models.ProcessCursorData
		currentCPUTime float64
		currentTime    int64
		expected       float64
	}{
		{
			name: "1 second elapsed, 1 second CPU time",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     0.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 1.0,
			currentTime:    baseTime + 1000,
			expected:       100.0,
		},
		{
			name: "1 second elapsed, 0.5 second CPU time",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     0.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 0.5,
			currentTime:    baseTime + 1000,
			expected:       50.0,
		},
		{
			name: "2 seconds elapsed, 0.5 second CPU time",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     0.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 0.5,
			currentTime:    baseTime + 2000,
			expected:       25.0,
		},
		{
			name: "incremental measurement",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     5.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 6.0,
			currentTime:    baseTime + 1000,
			expected:       100.0,
		},
		{
			name: "fractional CPU usage",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     10.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 10.25,
			currentTime:    baseTime + 1000,
			expected:       25.0,
		},
		{
			// This helper reports the raw single-core rate; GetProcessesWithCursor
			// divides by core count and clamps before the value is published.
			name: "four cores of work in one second is four cores of single-core rate",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     0.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 4.0,
			currentTime:    baseTime + 1000,
			expected:       400.0,
		},
		{
			name: "zero timestamp",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     0.0,
				Timestamp: 0,
			},
			currentCPUTime: 1.0,
			currentTime:    baseTime + 1000,
			expected:       0.0,
		},
		{
			name: "CPU time didn't increase",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     5.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 5.0,
			currentTime:    baseTime + 1000,
			expected:       0.0,
		},
		{
			name: "CPU time decreased (process restarted?)",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     10.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 5.0,
			currentTime:    baseTime + 1000,
			expected:       0.0,
		},
		{
			name: "wall time is zero",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     0.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 1.0,
			currentTime:    baseTime,
			expected:       0.0,
		},
		{
			name: "wall time is negative (clock skew)",
			cursor: &models.ProcessCursorData{
				PID:       1234,
				Ticks:     0.0,
				Timestamp: baseTime,
			},
			currentCPUTime: 1.0,
			currentTime:    baseTime - 1000,
			expected:       0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateProcessCPUPercentageWithCursor(tt.cursor, tt.currentCPUTime, tt.currentTime)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestCalculateProcessCPUPercentageRealWorld(t *testing.T) {
	tests := []struct {
		name         string
		cursor       *models.ProcessCursorData
		currentTicks float64
		elapsedMs    int64
		expected     float64
		delta        float64
	}{
		{
			name: "busy process for 5 seconds",
			cursor: &models.ProcessCursorData{
				PID:       9999,
				Ticks:     100.0,
				Timestamp: 1000000,
			},
			currentTicks: 105.0,
			elapsedMs:    5000,
			expected:     100.0,
			delta:        0.1,
		},
		{
			name: "idle process",
			cursor: &models.ProcessCursorData{
				PID:       9999,
				Ticks:     50.0,
				Timestamp: 1000000,
			},
			currentTicks: 50.05,
			elapsedMs:    10000,
			expected:     0.5,
			delta:        0.1,
		},
		{
			name: "moderate activity",
			cursor: &models.ProcessCursorData{
				PID:       9999,
				Ticks:     200.0,
				Timestamp: 1000000,
			},
			currentTicks: 203.0,
			elapsedMs:    10000,
			expected:     30.0,
			delta:        0.1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentTime := tt.cursor.Timestamp + tt.elapsedMs
			result := calculateProcessCPUPercentageWithCursor(tt.cursor, tt.currentTicks, currentTime)
			assert.InDelta(t, tt.expected, result, tt.delta)
		})
	}
}

func TestGetPssDirty(t *testing.T) {
	_, err := getPssDirty(999999)
	assert.Error(t, err, "Should error for non-existent PID")
}

func TestProcessCursorRoundTrip(t *testing.T) {
	base := int64(1_755_000_000_000)
	entries := []processSample{
		{ProcessCursorData: models.ProcessCursorData{PID: 1, Ticks: 12.34, Timestamp: base}},
		{ProcessCursorData: models.ProcessCursorData{PID: 4242, Ticks: 98765.43, Timestamp: base + 57}},
		{ProcessCursorData: models.ProcessCursorData{PID: 999999, Ticks: 0, Timestamp: base - 12}},
	}

	decoded := decodeProcessCursor(encodeProcessCursor(entries))

	require.Len(t, decoded, 3)
	for _, want := range entries {
		got, ok := decoded[want.PID]
		require.True(t, ok, "pid %d missing", want.PID)
		assert.Equal(t, want.Timestamp, got.Timestamp, "per-process timestamps must survive")
		assert.Equal(t, want.Ticks, got.Ticks)
	}
}

func TestProcessCursorFitsInAQueryParam(t *testing.T) {
	entries := make([]processSample, 0, 500)
	for i := range 500 {
		entries = append(entries, processSample{ProcessCursorData: models.ProcessCursorData{
			PID: int32(i * 7), Ticks: float64(i) * 13.37, Timestamp: 1_755_000_000_000 + int64(i%200),
		}})
	}

	encoded := encodeProcessCursor(entries)

	assert.Less(t, len(encoded), 6000)
	assert.Len(t, decodeProcessCursor(encoded), 500)
}

func gzipB64(t *testing.T, payload string) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, err := gz.Write([]byte(payload))
	require.NoError(t, err)
	require.NoError(t, gz.Close())
	return base64.RawURLEncoding.EncodeToString(buf.Bytes())
}

func TestProcessCursorRejectsBadInput(t *testing.T) {
	for name, cursor := range map[string]string{
		"empty":            "",
		"not base64":       "!!!not base64!!!",
		"not gzip":         base64.RawURLEncoding.EncodeToString([]byte(`[{"pid":1}]`)),
		"truncated gzip":   base64.RawURLEncoding.EncodeToString([]byte{0x1f, 0x8b, 0x08, 0x00}),
		"short cpu array":  gzipB64(t, `{"t":1,"pid":[1,2,3],"cpu":[1],"dt":[0,0,0]}`),
		"short dt array":   gzipB64(t, `{"t":1,"pid":[1,2,3],"cpu":[1,2,3],"dt":[]}`),
		"no arrays at all": gzipB64(t, `{"t":1}`),
	} {
		t.Run(name, func(t *testing.T) {
			assert.Empty(t, decodeProcessCursor(cursor))
		})
	}
}

func TestProcessCursorEncodesConcurrently(t *testing.T) {
	entries := make([]processSample, 0, 300)
	for i := range 300 {
		entries = append(entries, processSample{ProcessCursorData: models.ProcessCursorData{
			PID: int32(i), Ticks: float64(i), Timestamp: 1_755_000_000_000 + int64(i),
		}})
	}

	encoded := make(chan string, 16)
	for range 16 {
		go func() { encoded <- encodeProcessCursor(entries) }()
	}

	for range 16 {
		assert.Len(t, decodeProcessCursor(<-encoded), 300,
			"encoders share a pooled gzip writer and must not interleave output")
	}
}

func TestProcessCursorCapsDecompression(t *testing.T) {
	n := maxCursorDecodedBytes / 10
	bomb := make([]processSample, 0, n)
	for i := range n {
		bomb = append(bomb, processSample{ProcessCursorData: models.ProcessCursorData{PID: int32(i), Ticks: 1, Timestamp: 1}})
	}

	assert.Empty(t, decodeProcessCursor(encodeProcessCursor(bomb)),
		"a cursor that inflates past the cap must be discarded, not decoded")
}

func BenchmarkCalculateProcessCPUPercentage(b *testing.B) {
	cursor := &models.ProcessCursorData{
		PID:       1234,
		Ticks:     100.0,
		Timestamp: 1000000,
	}
	currentCPUTime := 105.5
	currentTime := int64(1005000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateProcessCPUPercentageWithCursor(cursor, currentCPUTime, currentTime)
	}
}
