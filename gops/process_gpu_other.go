//go:build !linux

package gops

import "time"

func scanProcessGPU(_ []int32, _ []gpuEntry, now time.Time) processGPUSample {
	return newProcessGPUSample(now)
}
