//go:build linux

package gops

import (
	"sync"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
)

type platformSources struct {
	sockDump     func() ([]sockDiagEntry, error)
	sockDestroys func(emit func(sockDiagEntry)) (stop func(), err error)
	tails        sockTails
	slowChipRead func(dir string) []hwmonReading
	slowChipWait time.Duration

	memModules struct {
		sync.Mutex
		read bool
		info *models.MemoryModulesInfo
		err  error
	}
}

func defaultPlatformSources() platformSources {
	return platformSources{sockDump: dumpSockets, sockDestroys: listenSockDestroys, slowChipRead: readHwmonReadings, slowChipWait: 100 * time.Millisecond}
}

func (self *GopsUtil) closePlatform() { self.tails.close() }
