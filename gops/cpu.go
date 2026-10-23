package gops

import (
	"encoding/base64"
	"encoding/json"
	"slices"
	"sync"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
)

type CPUTracker struct {
	cpuModel    string
	cpuFreq     float64
	cpuCount    int
	modelCached bool

	virtualization string

	tempPath     string
	tempLastRead time.Time
	tempValue    float64

	freqLastRead time.Time
	freqValue    float64

	static  cpuStatic
	dynamic cpuDynamic

	mu sync.RWMutex
}

var cpuTracker = &CPUTracker{}

func (self *GopsUtil) GetCPUInfo() (*models.CPUInfo, error) {
	return self.GetCPUInfoWithCursor("")
}

func (self *GopsUtil) GetCPUInfoWithCursor(cursor string) (*models.CPUInfo, error) {
	cpuInfo := models.CPUInfo{}

	cpuTracker.mu.Lock()
	defer cpuTracker.mu.Unlock()

	self.loadCPUStatic()

	now := time.Now()
	if now.Sub(cpuTracker.freqLastRead) > 2*time.Second {
		cpuTracker.dynamic = readCPUDynamicLive()
		// The first online CPU's clock, as in /proc/cpuinfo, which on x86
		// interrupts every CPU to read.
		if i := slices.IndexFunc(cpuTracker.dynamic.coreMHz, func(f float64) bool { return f > 0 }); i >= 0 {
			cpuTracker.freqValue = cpuTracker.dynamic.coreMHz[i]
		} else {
			cpuTracker.freqValue = getCurrentCPUFreq()
		}
		cpuTracker.freqLastRead = now
	}
	if cpuTracker.freqValue > 0 {
		cpuInfo.Frequency = cpuTracker.freqValue
	} else {
		cpuInfo.Frequency = cpuTracker.cpuFreq
	}

	cpuInfo.CoreFrequencies = slices.Clone(cpuTracker.dynamic.coreMHz)
	cpuInfo.Governor = cpuTracker.dynamic.governor
	cpuInfo.EPP = cpuTracker.dynamic.epp
	cpuInfo.MinFrequency = cpuTracker.static.minMHz
	cpuInfo.MaxFrequency = cpuTracker.static.maxMHz
	cpuInfo.PhysicalCores = cpuTracker.static.physicalCores
	if c := cpuTracker.static.cache; c != nil {
		cpuInfo.Cache = new(*c)
	}
	cpuInfo.BaseFrequency = cpuTracker.static.baseMHz
	cpuInfo.Sockets = cpuTracker.static.sockets
	cpuInfo.Virtualization = cpuTracker.virtualization

	cpuInfo.Count = cpuTracker.cpuCount
	cpuInfo.Model = cpuTracker.cpuModel

	if now.Sub(cpuTracker.tempLastRead) > 5*time.Second {
		cpuTracker.tempValue = getCPUTemperatureCached()
		cpuTracker.tempLastRead = now
	}
	cpuInfo.Temperature = cpuTracker.tempValue

	times, err := self.cpuProvider.Times(false)
	if err == nil && len(times) > 0 {
		t := times[0]
		cpuInfo.Total = []float64{
			t.User, t.Nice, t.System,
			t.Idle, t.Iowait, t.Irq,
			t.Softirq, t.Steal,
		}
	}

	perCore, err := self.cpuProvider.Times(true)
	if err == nil {
		cpuInfo.Cores = make([][]float64, len(perCore))
		for i, c := range perCore {
			cpuInfo.Cores[i] = []float64{
				c.User, c.Nice, c.System,
				c.Idle, c.Iowait, c.Irq,
				c.Softirq, c.Steal,
			}
		}
	}

	currentTime := now.UnixMilli()

	var cursorData models.CPUCursorData
	if cursor != "" {
		if jsonBytes, err := base64.RawURLEncoding.DecodeString(cursor); err == nil {
			if err := json.Unmarshal(jsonBytes, &cursorData); err != nil {
				cursorData = models.CPUCursorData{}
			}
		}
	}

	if len(cursorData.Total) > 0 && len(cpuInfo.Total) > 0 && cursorData.Timestamp > 0 {
		timeDiff := float64(currentTime-cursorData.Timestamp) / 1000.0
		if timeDiff > 0 {
			totalUsage, coreUsages := cpuUsageFromProvider(self.cpuProvider, cursorData.Total, cpuInfo.Total, timeDiff, cpuInfo.Count)
			cpuInfo.Usage = totalUsage

			switch {
			case coreUsages != nil:
				cpuInfo.CoreUsage = coreUsages
			case len(cursorData.Cores) > 0 && len(cpuInfo.Cores) > 0:
				cpuInfo.CoreUsage = make([]float64, len(cpuInfo.Cores))
				for i := 0; i < len(cpuInfo.Cores) && i < len(cursorData.Cores); i++ {
					cpuInfo.CoreUsage[i] = calculateCPUPercentage(cursorData.Cores[i], cpuInfo.Cores[i])
				}
			}
		}
	} else {
		primeCPUPercent()

		cpuPercent, err := self.cpuProvider.Percent(100*time.Millisecond, false)
		if err == nil && len(cpuPercent) > 0 {
			cpuInfo.Usage = cpuPercent[0]
		}

		corePercent, err := self.cpuProvider.Percent(100*time.Millisecond, true)
		if err == nil {
			cpuInfo.CoreUsage = corePercent
		}
	}

	newCursor := models.CPUCursorData{
		Total:     cpuInfo.Total,
		Cores:     cpuInfo.Cores,
		Timestamp: currentTime,
	}
	cursorBytes, _ := json.Marshal(newCursor)
	cpuInfo.Cursor = base64.RawURLEncoding.EncodeToString(cursorBytes)

	return &cpuInfo, nil
}

// The caller holds cpuTracker.mu.
func (self *GopsUtil) loadCPUStatic() {
	if cpuTracker.modelCached {
		return
	}
	cpuTracker.cpuCount, _ = self.cpuProvider.Counts(true)
	info, err := self.cpuProvider.Info()
	if err == nil && len(info) > 0 {
		cpuTracker.cpuModel = info[0].ModelName
		cpuTracker.cpuFreq = info[0].Mhz
		cpuTracker.virtualization = cpuVirtualization(info[0].Flags)
	}
	cpuTracker.static = readCPUStaticLive()
	cpuTracker.modelCached = true
}

func cpuVirtualization(flags []string) string {
	switch {
	case slices.Contains(flags, "svm"):
		return "AMD-V"
	case slices.Contains(flags, "vmx"):
		return "VT-x"
	}
	return ""
}

func calculateCPUPercentage(prev, curr []float64) float64 {
	if len(prev) < 8 || len(curr) < 8 {
		return 0
	}

	prevUser, prevNice, prevSystem := prev[0], prev[1], prev[2]
	prevIdle, prevIowait := prev[3], prev[4]
	prevIrq, prevSoftirq, prevSteal := prev[5], prev[6], prev[7]

	currUser, currNice, currSystem := curr[0], curr[1], curr[2]
	currIdle, currIowait := curr[3], curr[4]
	currIrq, currSoftirq, currSteal := curr[5], curr[6], curr[7]

	prevTotal := prevUser + prevNice + prevSystem + prevIdle + prevIowait + prevIrq + prevSoftirq + prevSteal
	currTotal := currUser + currNice + currSystem + currIdle + currIowait + currIrq + currSoftirq + currSteal

	prevBusy := prevTotal - prevIdle - prevIowait
	currBusy := currTotal - currIdle - currIowait

	if currBusy <= prevBusy {
		return 0
	}
	if currTotal <= prevTotal {
		return 100
	}

	usage := (currBusy - prevBusy) / (currTotal - prevTotal) * 100.0

	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}

	return usage
}
