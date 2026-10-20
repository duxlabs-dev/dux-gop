package gops

import (
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/gops/mocks"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestGetCPUInfo_WithMocks(t *testing.T) {
	mockCPU := mocks.NewMockCPUInfoProvider(t)
	mockMem := mocks.NewMockMemoryInfoProvider(t)
	mockDisk := mocks.NewMockDiskInfoProvider(t)
	mockNet := mocks.NewMockNetworkInfoProvider(t)
	mockProc := mocks.NewMockProcessInfoProvider(t)
	mockHost := mocks.NewMockHostInfoProvider(t)
	mockLoad := mocks.NewMockLoadInfoProvider(t)
	mockFS := mocks.NewMockFileSystem(t)

	gops := NewGopsUtilWithProviders(
		mockCPU,
		mockMem,
		mockDisk,
		mockNet,
		mockProc,
		mockHost,
		mockLoad,
		mockFS,
	)

	cpuTracker.modelCached = false
	cpuTracker.freqLastRead = time.Time{}
	cpuTracker.tempLastRead = time.Time{}

	mockCPU.EXPECT().
		Counts(true).
		Return(8, nil).
		Once()

	mockCPU.EXPECT().
		Info().
		Return([]cpu.InfoStat{
			{
				ModelName: "AMD Ryzen 7 5800X",
				Mhz:       3800.0,
			},
		}, nil).
		Once()

	mockCPU.EXPECT().
		Times(false).
		Return([]cpu.TimesStat{
			{
				User:    1000.0,
				Nice:    0.0,
				System:  500.0,
				Idle:    8500.0,
				Iowait:  0.0,
				Irq:     0.0,
				Softirq: 0.0,
				Steal:   0.0,
			},
		}, nil).
		Once()

	mockCPU.EXPECT().
		Times(true).
		Return([]cpu.TimesStat{
			{User: 100.0, Nice: 0.0, System: 50.0, Idle: 850.0, Iowait: 0.0, Irq: 0.0, Softirq: 0.0, Steal: 0.0},
			{User: 110.0, Nice: 0.0, System: 60.0, Idle: 830.0, Iowait: 0.0, Irq: 0.0, Softirq: 0.0, Steal: 0.0},
			{User: 120.0, Nice: 0.0, System: 70.0, Idle: 810.0, Iowait: 0.0, Irq: 0.0, Softirq: 0.0, Steal: 0.0},
			{User: 130.0, Nice: 0.0, System: 80.0, Idle: 790.0, Iowait: 0.0, Irq: 0.0, Softirq: 0.0, Steal: 0.0},
		}, nil).
		Once()

	mockCPU.EXPECT().
		Percent(100*time.Millisecond, false).
		Return([]float64{25.5}, nil).
		Once()

	mockCPU.EXPECT().
		Percent(100*time.Millisecond, true).
		Return([]float64{20.0, 25.0, 30.0, 35.0}, nil).
		Once()

	result, err := gops.GetCPUInfo()

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 8, result.Count)
	assert.Equal(t, "AMD Ryzen 7 5800X", result.Model)
	assert.Greater(t, result.Frequency, 0.0)
	assert.Equal(t, 25.5, result.Usage)
	assert.Len(t, result.Total, 8)
	assert.Len(t, result.Cores, 4)
	assert.Len(t, result.CoreUsage, 4)
	assert.NotEmpty(t, result.Cursor)

	mockCPU.AssertExpectations(t)
}

func TestGetCPUInfoWithCursor_WithMocks(t *testing.T) {
	mockCPU := mocks.NewMockCPUInfoProvider(t)
	mockMem := mocks.NewMockMemoryInfoProvider(t)
	mockDisk := mocks.NewMockDiskInfoProvider(t)
	mockNet := mocks.NewMockNetworkInfoProvider(t)
	mockProc := mocks.NewMockProcessInfoProvider(t)
	mockHost := mocks.NewMockHostInfoProvider(t)
	mockLoad := mocks.NewMockLoadInfoProvider(t)
	mockFS := mocks.NewMockFileSystem(t)

	gops := NewGopsUtilWithProviders(
		mockCPU,
		mockMem,
		mockDisk,
		mockNet,
		mockProc,
		mockHost,
		mockLoad,
		mockFS,
	)

	mockCPU.EXPECT().
		Times(false).
		Return([]cpu.TimesStat{
			{
				User:    2000.0,
				Nice:    0.0,
				System:  1000.0,
				Idle:    17000.0,
				Iowait:  0.0,
				Irq:     0.0,
				Softirq: 0.0,
				Steal:   0.0,
			},
		}, nil).
		Once()

	mockCPU.EXPECT().
		Times(true).
		Return([]cpu.TimesStat{
			{User: 200.0, Nice: 0.0, System: 100.0, Idle: 1700.0, Iowait: 0.0, Irq: 0.0, Softirq: 0.0, Steal: 0.0},
			{User: 210.0, Nice: 0.0, System: 110.0, Idle: 1680.0, Iowait: 0.0, Irq: 0.0, Softirq: 0.0, Steal: 0.0},
		}, nil).
		Once()

	cursor := "eyJUb3RhbCI6WzEwMDAsMCw1MDAsODUwMCwwLDAsMCwwXSwiQ29yZXMiOltbMTAwLDAsNTAsODUwLDAsMCwwLDBdLFsxMTAsMCw2MCw4MzAsMCwwLDAsMF1dLCJUaW1lc3RhbXAiOjE2MzA1MjYyNzAwMDB9"

	result, err := gops.GetCPUInfoWithCursor(cursor)

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Greater(t, result.Usage, 0.0)
	assert.Len(t, result.CoreUsage, 2)
	assert.NotEmpty(t, result.Cursor)

	mockCPU.AssertExpectations(t)
}

func TestGetCPUInfo_ErrorHandling(t *testing.T) {
	tests := []struct {
		name           string
		setupMocks     func(*mocks.MockCPUInfoProvider)
		expectError    bool
		validateResult func(*testing.T, interface{})
	}{
		{
			name: "handles Info() error gracefully",
			setupMocks: func(m *mocks.MockCPUInfoProvider) {
				cpuTracker.modelCached = false
				m.EXPECT().Counts(true).Return(0, assert.AnError).Once()
				m.EXPECT().Info().Return(nil, assert.AnError).Once()
				m.EXPECT().Times(false).Return([]cpu.TimesStat{{User: 100, Idle: 900}}, nil).Once()
				m.EXPECT().Times(true).Return([]cpu.TimesStat{{User: 100, Idle: 900}}, nil).Once()
				m.EXPECT().Percent(mock.Anything, false).Return([]float64{10.0}, nil).Once()
				m.EXPECT().Percent(mock.Anything, true).Return([]float64{10.0}, nil).Once()
			},
			expectError: false,
		},
		{
			name: "handles Times() error gracefully",
			setupMocks: func(m *mocks.MockCPUInfoProvider) {
				m.EXPECT().Times(false).Return(nil, assert.AnError).Once()
				m.EXPECT().Times(true).Return(nil, assert.AnError).Once()
				m.EXPECT().Percent(mock.Anything, false).Return([]float64{10.0}, nil).Once()
				m.EXPECT().Percent(mock.Anything, true).Return([]float64{10.0}, nil).Once()
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCPU := mocks.NewMockCPUInfoProvider(t)
			mockMem := mocks.NewMockMemoryInfoProvider(t)
			mockDisk := mocks.NewMockDiskInfoProvider(t)
			mockNet := mocks.NewMockNetworkInfoProvider(t)
			mockProc := mocks.NewMockProcessInfoProvider(t)
			mockHost := mocks.NewMockHostInfoProvider(t)
			mockLoad := mocks.NewMockLoadInfoProvider(t)
			mockFS := mocks.NewMockFileSystem(t)

			gops := NewGopsUtilWithProviders(
				mockCPU,
				mockMem,
				mockDisk,
				mockNet,
				mockProc,
				mockHost,
				mockLoad,
				mockFS,
			)

			tt.setupMocks(mockCPU)

			result, err := gops.GetCPUInfo()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}

			mockCPU.AssertExpectations(t)
		})
	}
}
