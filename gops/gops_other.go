//go:build !linux

package gops

import "time"

type platformSources struct{}

func defaultPlatformSources() platformSources { return platformSources{} }

func (self *GopsUtil) defaultGPUSampler() func([]gpuEntry, gpuCursor, time.Time) ([]gpuSample, gpuCursor) {
	return sampleGPUs
}

type drmClientSet map[string]struct{}

type procGPUReader struct{}

type procNetReader struct{ netDump }

func (self *GopsUtil) newProcGPUReader(_ []string) *procGPUReader { return nil }

func (self *GopsUtil) newProcNetReader() *procNetReader { return nil }

func (self *GopsUtil) closePlatform() {}

func (self processExtras) readFDs(_ string, _ int32) (procGPU, procNet) {
	return procGPU{}, procNet{}
}

func cursorClients(_ drmClientSet) []gpuCursorClient { return nil }

func procGPUUsage(_ []gpuCursorClient, _ drmClientSet, _ time.Duration) (all, video float64) {
	return 0, 0
}
