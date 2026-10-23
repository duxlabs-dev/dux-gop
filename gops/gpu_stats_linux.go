//go:build linux

package gops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
)

const (
	pciDevicesRoot   = "/sys/bus/pci/devices"
	thermalRoot      = "/sys/class/thermal"
	nvidiaSmiTimeout = 2 * time.Second
)

var nvidiaSmiFields = []string{
	"pci.bus_id",
	"temperature.gpu",
	"utilization.gpu",
	"memory.used",
	"memory.total",
	"power.draw",
	"clocks.gr",
	"clocks.mem",
	"clocks.max.gr",
	"clocks.max.mem",
	"power.limit",
	"fan.speed",
	"pcie.link.gen.current",
	"pcie.link.gen.max",
	"pcie.link.width.current",
	"pcie.link.width.max",
}

var nvidiaSmiVideoFields = []string{"utilization.encoder", "utilization.decoder"}

// nvidiaSmiNoVideo is set once nvidia-smi rejects nvidiaSmiVideoFields, which
// older drivers don't know.
var nvidiaSmiNoVideo atomic.Bool

var nvidiaSmiInvalidField = []byte("is not a valid field")

var pcieGenSpeeds = map[int]string{
	1: "2.5 GT/s PCIe",
	2: "5.0 GT/s PCIe",
	3: "8.0 GT/s PCIe",
	4: "16.0 GT/s PCIe",
	5: "32.0 GT/s PCIe",
	6: "64.0 GT/s PCIe",
}

type gpuSampler struct {
	pciDevices string
	thermal    string
	nvidiaSmi  func(fields, busIds []string) ([]byte, error)
	hwmon      func(dir string) (hwmonChip, bool)
}

func (self *GopsUtil) defaultGPUSampler() func([]gpuEntry, gpuCursor, time.Time) ([]gpuSample, gpuCursor) {
	return gpuSampler{
		pciDevices: pciDevicesRoot,
		thermal:    thermalRoot,
		nvidiaSmi:  runNvidiaSmi,
		hwmon:      self.readHwmonChip,
	}.sample
}

func (s gpuSampler) sample(entries []gpuEntry, prev gpuCursor, now time.Time) ([]gpuSample, gpuCursor) {
	samples := make([]gpuSample, len(entries))
	next := newGPUCursor(now)
	var nvidia []int

	for i, entry := range entries {
		sample := &samples[i]
		sample.Hwmon = "unknown"
		dev := filepath.Join(s.pciDevices, entry.BusId)

		// Most driver queries resume a runtime-suspended card, and a sleeping GPU
		// is idle by definition.
		if entry.BusId != "" && isRuntimeSuspended(dev) {
			sample.State = models.GPUStateSuspended
			sample.Usage = new(0.0)
			continue
		}
		sample.State = models.GPUStateActive

		switch entry.Driver {
		case "nvidia":
			nvidia = append(nvidia, i)
			continue
		case "amdgpu":
			readAMDGPU(dev, sample)
		case "i915", "xe":
			readIntelGPU(dev, entry.Driver, entry.BusId, sample, prev, next)
		}

		s.readGPUHwmon(dev, entry.BusId, sample, prev, next)
		readPCIeLink(dev, sample)
		if entry.Driver == "amdgpu" {
			readAMDGPUClocks(dev, sample)
			readAMDGPUPCIe(dev, sample)
		}
		if sample.Temperature == 0 {
			if temp := s.acpiTemperature(); temp > 0 {
				sample.Temperature, sample.Hwmon = temp, "acpitz"
			}
		}
	}

	if len(nvidia) > 0 {
		s.sampleNvidia(entries, nvidia, samples)
	}

	return samples, next
}

func readAMDGPU(dev string, sample *gpuSample) {
	sample.Usage = readFloat(filepath.Join(dev, "gpu_busy_percent"))
	used, total := readKB(filepath.Join(dev, "mem_info_vram_used")), readKB(filepath.Join(dev, "mem_info_vram_total"))
	if used != nil && total != nil && *total > 0 {
		sample.MemoryUsed, sample.MemoryTotal = used, total
	}
	sample.GTTUsed = readKB(filepath.Join(dev, "mem_info_gtt_used"))
	sample.GTTTotal = readKB(filepath.Join(dev, "mem_info_gtt_total"))
	sample.MemoryBusy = readFloat(filepath.Join(dev, "mem_busy_percent"))
}

func readAMDGPUClocks(dev string, sample *gpuSample) {
	cur, maxClock := readDPMLevels(filepath.Join(dev, "pp_dpm_sclk"))
	sample.CoreClock, sample.CoreClockMax = firstSet(sample.CoreClock, cur), maxClock
	cur, maxClock = readDPMLevels(filepath.Join(dev, "pp_dpm_mclk"))
	sample.MemoryClock, sample.MemoryClockMax = firstSet(sample.MemoryClock, cur), maxClock
}

// readDPMLevels parses amdgpu pp_dpm_* files; the "S:" sleep level is not a clock.
func readDPMLevels(path string) (cur, maxClock *float64) {
	for line := range strings.SplitSeq(readTrimmed(path), "\n") {
		level, rest, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(level) == "S" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			continue
		}
		mhz, err := strconv.ParseFloat(strings.TrimSuffix(strings.ToLower(fields[0]), "mhz"), 64)
		if err != nil {
			continue
		}
		if maxClock == nil || mhz > *maxClock {
			maxClock = &mhz
		}
		if len(fields) > 1 && fields[1] == "*" {
			cur = &mhz
		}
	}
	return cur, maxClock
}

func readPCIeLink(dev string, sample *gpuSample) {
	sample.PcieLinkSpeed = readLinkSpeed(filepath.Join(dev, "current_link_speed"))
	sample.PcieLinkSpeedMax = readLinkSpeed(filepath.Join(dev, "max_link_speed"))
	sample.PcieLinkWidth, _ = strconv.Atoi(readTrimmed(filepath.Join(dev, "current_link_width")))
	sample.PcieLinkWidthMax, _ = strconv.Atoi(readTrimmed(filepath.Join(dev, "max_link_width")))
}

// readAMDGPUPCIe takes the link from pp_dpm_pcie: a dGPU's sysfs link is its card's internal
// switch, while the SMU reports the slot's.
func readAMDGPUPCIe(dev string, sample *gpuSample) {
	var curSpeed, maxSpeed float64
	var curWidth, maxWidth int
	for line := range strings.SplitSeq(readTrimmed(filepath.Join(dev, "pp_dpm_pcie")), "\n") {
		_, rest, _ := strings.Cut(line, ":")
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}
		speed, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(fields[0], ","), "GT/s"), 64)
		width, werr := strconv.Atoi(strings.TrimPrefix(fields[1], "x"))
		if err != nil || werr != nil {
			continue
		}
		maxSpeed, maxWidth = max(maxSpeed, speed), max(maxWidth, width)
		if fields[len(fields)-1] == "*" {
			curSpeed, curWidth = speed, width
		}
	}
	if curSpeed == 0 {
		return
	}
	sample.PcieLinkSpeed = fmt.Sprintf("%.1f GT/s PCIe", curSpeed)
	sample.PcieLinkSpeedMax = fmt.Sprintf("%.1f GT/s PCIe", maxSpeed)
	sample.PcieLinkWidth, sample.PcieLinkWidthMax = curWidth, maxWidth
}

func readLinkSpeed(path string) string {
	s := readTrimmed(path)
	if s == "Unknown" {
		return ""
	}
	return s
}

func firstSet[T any](values ...*T) *T {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func isRuntimeSuspended(dev string) bool {
	switch readTrimmed(filepath.Join(dev, "power", "runtime_status")) {
	case "suspended", "suspending":
		return true
	}
	return false
}

// Intel exposes no busy counter without perf privileges, so usage is time the
// render GT spent out of RC6 between two samples. That overstates engine load
// somewhat, but it is what an unprivileged reader can get.
func readIntelGPU(dev, driver, busId string, sample *gpuSample, prev, next gpuCursor) {
	var idlePath, freqPath string
	switch driver {
	case "xe":
		gt := xeRenderGT(dev)
		idlePath = filepath.Join(gt, "gtidle", "idle_residency_ms")
		freqPath = filepath.Join(gt, "freq0", "act_freq")
		sample.CoreClockMax = readScaled(filepath.Join(gt, "freq0", "rp0_freq"), 1)
		readXeVRAM(dev, sample)
	case "i915":
		card := drmCardDir(dev)
		if card == "" {
			return
		}
		gt := filepath.Join(card, "gt", "gt0")
		idlePath = filepath.Join(gt, "rc6_residency_ms")
		freqPath = filepath.Join(gt, "rps_act_freq_mhz")
		if _, err := os.Stat(gt); err != nil {
			idlePath = filepath.Join(card, "power", "rc6_residency_ms")
			freqPath = filepath.Join(card, "gt_act_freq_mhz")
		}
		sample.CoreClockMax = firstSet(readScaled(filepath.Join(card, "gt_RP0_freq_mhz"), 1), readScaled(filepath.Join(card, "gt_max_freq_mhz"), 1))
	}

	if mhz := readFloat(freqPath); mhz != nil {
		sample.CoreClock = mhz
	}

	idleMs := readUint(idlePath)
	if idleMs == nil {
		return
	}
	next.set(busId, "idle", *idleMs)
	diff, seconds, ok := next.delta(prev, busId, "idle")
	if !ok || seconds <= 0 {
		return
	}
	busy := 100 * (1 - float64(diff)/1000/seconds)
	sample.Usage = new(min(100, max(0, busy)))
}

// xeRenderGT picks the render GT; media GTs report MC6 and are named gtN-mc.
func xeRenderGT(dev string) string {
	names, _ := filepath.Glob(filepath.Join(dev, "tile*", "gt*", "gtidle", "name"))
	sort.Strings(names)
	for _, name := range names {
		if strings.HasSuffix(readTrimmed(name), "-rc") {
			return filepath.Dir(filepath.Dir(name))
		}
	}
	return filepath.Join(dev, "tile0", "gt0")
}

func drmCardDir(dev string) string {
	cards, _ := filepath.Glob(filepath.Join(dev, "drm", "card[0-9]*"))
	for _, card := range cards {
		if !strings.Contains(filepath.Base(card), "-") {
			return card
		}
	}
	return ""
}

// readGPUHwmon fills whatever the driver's hwmon node exposes and the driver
// specific readers left empty.
func (s gpuSampler) readGPUHwmon(dev, busId string, sample *gpuSample, prev, next gpuCursor) {
	dirs, _ := filepath.Glob(filepath.Join(dev, "hwmon", "hwmon*"))
	sort.Strings(dirs)
	for _, dir := range dirs {
		chip, ok := s.hwmon(dir)
		// i915 adds per-GT energy-only nodes next to the card one.
		if !ok || strings.HasPrefix(chip.Name, "i915_gt") {
			continue
		}

		var power *float64
		for _, r := range chip.Readings {
			v := r.Value
			switch r.Kind {
			case sensorTemperature:
				// temp1 is amdgpu edge, i915 and nouveau; xe starts at the package sensor.
				if sample.Temperature == 0 && (r.File == "temp1_input" || r.Label == "pkg") {
					sample.Temperature, sample.Hwmon = v, filepath.Base(dir)
				}
				switch r.Label {
				case "junction":
					sample.Hotspot = firstSet(sample.Hotspot, &v)
				case "mem":
					sample.MemoryTemperature = firstSet(sample.MemoryTemperature, &v)
				}
			case sensorPower:
				if strings.HasPrefix(r.File, "power1_") && power == nil {
					power = &v
					if r.File == "power1_input" {
						power = firstSet(readScaled(filepath.Join(dir, "power1_average"), 1e6), power)
					}
					sample.PowerLimit = firstSet(sample.PowerLimit, r.Max)
				}
			case sensorFan:
				if sample.FanRpm == nil {
					sample.FanRpm = new(int(v))
					if r.Max != nil {
						sample.FanMaxRpm = new(int(*r.Max))
						if *r.Max > 0 {
							sample.FanPercent = new(min(v / *r.Max * 100, 100))
						}
					}
				}
			case sensorFrequency:
				switch r.Label {
				case "sclk":
					sample.CoreClock = firstSet(sample.CoreClock, &v)
				case "mclk":
					sample.MemoryClock = firstSet(sample.MemoryClock, &v)
				}
			}
		}

		if sample.Power == nil {
			sample.Power = power
		}
		if sample.Power == nil {
			sample.Power = readHwmonEnergyPower(dir, busId, prev, next)
		}

		// nvidia-smi reports PWM duty as fan speed, so prefer it over rpm.
		pwm, pwmMax := readFloat(filepath.Join(dir, "pwm1")), readFloat(filepath.Join(dir, "pwm1_max"))
		if pwm != nil && pwmMax != nil && *pwmMax > 0 {
			sample.FanPercent = new(min(*pwm / *pwmMax * 100, 100))
		}
	}
}

func readHwmonEnergyPower(dir, busId string, prev, next gpuCursor) *float64 {
	microJ := readUint(filepath.Join(dir, "energy1_input"))
	if microJ == nil {
		return nil
	}
	key := "energy:" + filepath.Base(dir)
	next.set(busId, key, *microJ)
	diff, seconds, ok := next.delta(prev, busId, key)
	if !ok || seconds <= 0 {
		return nil
	}
	return new(float64(diff) / 1e6 / seconds)
}

func (c gpuCursor) set(busId, name string, value uint64) {
	if c.Counters[busId] == nil {
		c.Counters[busId] = map[string]uint64{}
	}
	c.Counters[busId][name] = value
}

// delta returns how much a counter grew since prev and the elapsed time in
// seconds. ok is false on the first sample or when the counter went backwards.
func (c gpuCursor) delta(prev gpuCursor, busId, name string) (diff uint64, seconds float64, ok bool) {
	if prev.Timestamp == 0 || c.Timestamp <= prev.Timestamp {
		return 0, 0, false
	}
	cur, hasCur := c.Counters[busId][name]
	old, hasOld := prev.Counters[busId][name]
	if !hasCur || !hasOld || cur < old {
		return 0, 0, false
	}
	return cur - old, float64(c.Timestamp-prev.Timestamp) / float64(time.Second), true
}

func (s gpuSampler) acpiTemperature() float64 {
	entries, err := os.ReadDir(s.thermal)
	if err != nil {
		return 0
	}
	return getMaxACPITZTemperatureForGPU(s.thermal, entries, 20, 90)
}

func (s gpuSampler) sampleNvidia(entries []gpuEntry, indexes []int, samples []gpuSample) {
	busIds := make([]string, 0, len(indexes))
	for _, i := range indexes {
		if entries[i].BusId != "" {
			busIds = append(busIds, entries[i].BusId)
		}
	}
	if len(busIds) == 0 {
		return
	}

	fields := nvidiaSmiFields
	if !nvidiaSmiNoVideo.Load() {
		fields = append(fields[:len(fields):len(fields)], nvidiaSmiVideoFields...)
	}
	out, err := s.nvidiaSmi(fields, busIds)
	if len(fields) > len(nvidiaSmiFields) && nvidiaSmiRejected(out, err) {
		nvidiaSmiNoVideo.Store(true)
		out, err = s.nvidiaSmi(nvidiaSmiFields, busIds)
	}
	if err != nil {
		return
	}

	rows := parseNvidiaSmi(out)
	for _, i := range indexes {
		if row, ok := rows[entries[i].BusId]; ok {
			row.State = samples[i].State
			samples[i] = row
		}
	}
}

func nvidiaSmiRejected(out []byte, err error) bool {
	var exitErr *exec.ExitError
	return err != nil && (bytes.Contains(out, nvidiaSmiInvalidField) ||
		errors.As(err, &exitErr) && bytes.Contains(exitErr.Stderr, nvidiaSmiInvalidField))
}

func runNvidiaSmi(fields, busIds []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSmiTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu="+strings.Join(fields, ","),
		"--format=csv,noheader,nounits",
		"--id="+strings.Join(busIds, ","),
	).Output()
}

// parseNvidiaSmi reads rows in nvidiaSmiFields order by bus id; [N/A] and [Not Supported] stay nil.
func parseNvidiaSmi(out []byte) map[string]gpuSample {
	rows := map[string]gpuSample{}
	for line := range strings.SplitSeq(string(out), "\n") {
		f := strings.Split(line, ",")
		if len(f) != len(nvidiaSmiFields) && len(f) != len(nvidiaSmiFields)+len(nvidiaSmiVideoFields) {
			continue
		}
		for i := range f {
			f[i] = strings.TrimSpace(f[i])
		}

		mib := func(s string) *uint64 {
			v := parseNvidiaFloat(s)
			if v == nil {
				return nil
			}
			return new(uint64(*v) * 1024)
		}
		gen := func(s string) string {
			n, _ := strconv.Atoi(s)
			return pcieGenSpeeds[n]
		}

		row := gpuSample{
			Hwmon:            "unknown",
			Usage:            parseNvidiaFloat(f[2]),
			MemoryUsed:       mib(f[3]),
			MemoryTotal:      mib(f[4]),
			Power:            parseNvidiaFloat(f[5]),
			CoreClock:        parseNvidiaFloat(f[6]),
			MemoryClock:      parseNvidiaFloat(f[7]),
			CoreClockMax:     parseNvidiaFloat(f[8]),
			MemoryClockMax:   parseNvidiaFloat(f[9]),
			PowerLimit:       parseNvidiaFloat(f[10]),
			FanPercent:       parseNvidiaFloat(f[11]),
			PcieLinkSpeed:    gen(f[12]),
			PcieLinkSpeedMax: gen(f[13]),
		}
		row.PcieLinkWidth, _ = strconv.Atoi(f[14])
		row.PcieLinkWidthMax, _ = strconv.Atoi(f[15])
		if temp := parseNvidiaFloat(f[1]); temp != nil {
			row.Temperature, row.Hwmon = *temp, "nvidia"
		}
		if len(f) > len(nvidiaSmiFields) {
			row.EncoderUsage, row.DecoderUsage = parseNvidiaFloat(f[16]), parseNvidiaFloat(f[17])
		}
		rows[normalizeBusId(f[0])] = row
	}
	return rows
}

func parseNvidiaFloat(s string) *float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}
