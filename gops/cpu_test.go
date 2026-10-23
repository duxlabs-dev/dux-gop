package gops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCalculateCPUPercentage(t *testing.T) {
	tests := []struct {
		name     string
		prev     []float64
		curr     []float64
		expected float64
	}{
		{
			name:     "idle system",
			prev:     []float64{100, 0, 50, 9850, 0, 0, 0, 0},
			curr:     []float64{100, 0, 50, 9850, 0, 0, 0, 0},
			expected: 0,
		},
		{
			name:     "15% usage",
			prev:     []float64{1000, 0, 500, 8500, 0, 0, 0, 0},
			curr:     []float64{2000, 0, 1000, 17000, 0, 0, 0, 0},
			expected: 15.0,
		},
		{
			name:     "100% usage",
			prev:     []float64{1000, 0, 500, 500, 0, 0, 0, 0},
			curr:     []float64{2000, 0, 1000, 500, 0, 0, 0, 0},
			expected: 100.0,
		},
		{
			name:     "9.375% usage with iowait",
			prev:     []float64{500, 0, 250, 7000, 250, 0, 0, 0},
			curr:     []float64{1000, 0, 500, 14000, 500, 0, 0, 0},
			expected: 9.375,
		},
		{
			name:     "12.5% usage irq and softirq included",
			prev:     []float64{500, 0, 250, 7000, 0, 100, 150, 0},
			curr:     []float64{1000, 0, 500, 14000, 0, 200, 300, 0},
			expected: 12.5,
		},
		{
			name:     "12.5% usage with steal time",
			prev:     []float64{500, 0, 250, 7000, 0, 0, 0, 250},
			curr:     []float64{1000, 0, 500, 14000, 0, 0, 0, 500},
			expected: 12.5,
		},
		{
			name:     "invalid - too few prev values",
			prev:     []float64{100, 0, 50},
			curr:     []float64{200, 0, 100, 9700, 0, 0, 0, 0},
			expected: 0,
		},
		{
			name:     "invalid - too few curr values",
			prev:     []float64{100, 0, 50, 9850, 0, 0, 0, 0},
			curr:     []float64{200, 0, 100},
			expected: 0,
		},
		{
			name:     "busy decreased - return 0",
			prev:     []float64{2000, 0, 1000, 7000, 0, 0, 0, 0},
			curr:     []float64{1000, 0, 500, 8500, 0, 0, 0, 0},
			expected: 0,
		},
		{
			name:     "total decreased - return 0",
			prev:     []float64{1000, 0, 500, 8500, 0, 0, 0, 0},
			curr:     []float64{500, 0, 250, 4250, 0, 0, 0, 0},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateCPUPercentage(tt.prev, tt.curr)
			assert.InDelta(t, tt.expected, result, 0.1)
		})
	}
}

func TestCalculateCPUPercentageRealWorld(t *testing.T) {
	tests := []struct {
		name     string
		prev     []float64
		curr     []float64
		expected float64
		delta    float64
	}{
		{
			name:     "light load",
			prev:     []float64{12345, 67, 8901, 234567, 89, 12, 34, 56},
			curr:     []float64{12445, 67, 9001, 244567, 89, 12, 34, 56},
			expected: 2.0,
			delta:    0.5,
		},
		{
			name:     "moderate load - realistic values",
			prev:     []float64{50000, 100, 25000, 400000, 500, 100, 200, 100},
			curr:     []float64{55000, 100, 30000, 405000, 500, 100, 200, 100},
			expected: 66.67,
			delta:    1.0,
		},
		{
			name:     "heavy load - realistic values",
			prev:     []float64{100000, 200, 50000, 150000, 1000, 500, 1000, 300},
			curr:     []float64{110000, 200, 60000, 155000, 1000, 500, 1000, 300},
			expected: 80.0,
			delta:    1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := calculateCPUPercentage(tt.prev, tt.curr)
			assert.InDelta(t, tt.expected, result, tt.delta)
		})
	}
}

func BenchmarkCalculateCPUPercentage(b *testing.B) {
	prev := []float64{1000, 0, 500, 8500, 0, 0, 0, 0}
	curr := []float64{2000, 0, 1000, 17000, 0, 0, 0, 0}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		calculateCPUPercentage(prev, curr)
	}
}

func TestCPUVirtualization(t *testing.T) {
	assert.Equal(t, "AMD-V", cpuVirtualization([]string{"fpu", "svm", "sse"}))
	assert.Equal(t, "VT-x", cpuVirtualization([]string{"vmx"}))
	assert.Equal(t, "", cpuVirtualization(nil))
}
