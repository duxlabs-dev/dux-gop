//go:build linux

package gops

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const hwmonRoot = "/sys/class/hwmon"

const (
	sensorTemperature = "temperature"
	sensorFan         = "fan"
	sensorPower       = "power"
	sensorFrequency   = "frequency"
)

// hwmonReading values: temperature °C, fan rpm, power W, frequency MHz.
type hwmonReading struct {
	Kind     string
	Label    string
	File     string // e.g. temp1_input
	Value    float64
	Max      *float64
	Critical *float64
}

type hwmonChip struct {
	Name     string
	Device   string
	Readings []hwmonReading
}

var hwmonInputRe = regexp.MustCompile(`^(temp|fan|power|freq)(\d+)_(input|average)$`)

// slowChipTime marks a chip slow: NVMe drives answer each hwmon read with an
// admin command, taking milliseconds per file.
const slowChipTime = 5 * time.Millisecond

var (
	hwmonMu    sync.Mutex
	hwmonCache = map[string]*hwmonState{}
)

type hwmonState struct {
	name   string
	device string
	async  bool
	inputs []hwmonInput

	readings []hwmonReading
	haveRead bool
	inflight bool
	done     chan struct{}
}

type hwmonInput struct {
	file     string
	kind     string
	label    string
	scale    float64
	max      *float64
	critical *float64
	// power limits are re-read on every sample, since users change caps at runtime.
	powerCap, powerMax string
}

func (self *GopsUtil) readHwmonChips(root string, skip map[string]bool) []hwmonChip {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var chips []hwmonChip
	seen := map[string]bool{}
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		seen[dir] = true
		if link, err := os.Readlink(filepath.Join(dir, "device")); err == nil && skip[filepath.Base(link)] {
			continue
		}
		if chip, ok := self.readHwmonChip(dir); ok {
			chips = append(chips, chip)
		}
	}
	hwmonMu.Lock()
	for dir := range hwmonCache {
		if strings.HasPrefix(dir, root+"/") && !seen[dir] {
			delete(hwmonCache, dir)
		}
	}
	hwmonMu.Unlock()
	return chips
}

// readHwmonChip is not ok for chips without a name or that must not be read:
// drivetemp, and any chip of a runtime-suspended device.
func (self *GopsUtil) readHwmonChip(dir string) (hwmonChip, bool) {
	name := readTrimmed(filepath.Join(dir, "name"))
	if name == "" || name == "drivetemp" {
		return hwmonChip{}, false
	}
	if isRuntimeSuspended(filepath.Join(dir, "device")) {
		return hwmonChip{}, false
	}
	st := hwmonStateFor(dir, name)
	chip := hwmonChip{Name: name, Device: st.device}
	hwmonMu.Lock()
	async, scanned := st.async, st.inputs != nil
	hwmonMu.Unlock()
	if async {
		chip.Readings = self.readChipAsync(dir, st)
		return chip, true
	}
	start := time.Now()
	chip.Readings = readHwmonReadings(dir)
	if scanned && time.Since(start) > slowChipTime {
		hwmonMu.Lock()
		st.async, st.readings, st.haveRead = true, chip.Readings, true
		hwmonMu.Unlock()
	}
	return chip, true
}

func hwmonStateFor(dir, name string) *hwmonState {
	hwmonMu.Lock()
	st := hwmonCache[dir]
	hwmonMu.Unlock()
	if st != nil && st.name == name {
		return st
	}
	st = &hwmonState{name: name}
	if link, err := os.Readlink(filepath.Join(dir, "device")); err == nil {
		st.device = filepath.Base(link)
	}
	if resolved, err := filepath.EvalSymlinks(filepath.Join(dir, "device")); err == nil && strings.Contains(resolved, "/i2c-") {
		st.async = true
	}
	hwmonMu.Lock()
	hwmonCache[dir] = st
	hwmonMu.Unlock()
	return st
}

// readChipAsync returns the last readings and starts a new read; only a chip without readings waits (at most slowChipWait).
func (self *GopsUtil) readChipAsync(dir string, st *hwmonState) []hwmonReading {
	hwmonMu.Lock()
	if !st.inflight {
		st.inflight = true
		done := make(chan struct{})
		st.done = done
		go func() {
			r := self.slowChipRead(dir)
			hwmonMu.Lock()
			st.readings, st.haveRead, st.inflight = r, true, false
			hwmonMu.Unlock()
			close(done)
		}()
	}
	done, have := st.done, st.haveRead
	hwmonMu.Unlock()

	if !have {
		select {
		case <-done:
		case <-time.After(self.slowChipWait):
		}
	}

	hwmonMu.Lock()
	defer hwmonMu.Unlock()
	return st.readings
}

func readHwmonReadings(dir string) []hwmonReading {
	hwmonMu.Lock()
	st := hwmonCache[dir]
	var inputs []hwmonInput
	if st != nil {
		inputs = st.inputs
	}
	hwmonMu.Unlock()
	if inputs == nil {
		inputs = scanHwmonInputs(dir)
		if st != nil {
			hwmonMu.Lock()
			st.inputs = inputs
			hwmonMu.Unlock()
		}
	}

	var out []hwmonReading
	for _, in := range inputs {
		raw := readFloat(filepath.Join(dir, in.file))
		if raw == nil {
			continue
		}
		r := hwmonReading{Kind: in.kind, Label: in.label, File: in.file, Value: *raw / in.scale, Max: copyOf(in.max), Critical: copyOf(in.critical)}
		if in.kind == sensorPower {
			r.Max = readScaled(filepath.Join(dir, in.powerCap), 1e6)
			if r.Max == nil {
				r.Max = readScaled(filepath.Join(dir, in.powerMax), 1e6)
			}
		}
		out = append(out, r)
	}
	return out
}

func copyOf(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return new(*v)
}

func scanHwmonInputs(dir string) []hwmonInput {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []hwmonInput{}
	}

	type key struct {
		typ string
		n   int
	}
	files := map[key]string{}
	for _, entry := range entries {
		m := hwmonInputRe.FindStringSubmatch(entry.Name())
		if m == nil || (m[3] == "average" && m[1] != "power") {
			continue
		}
		n, _ := strconv.Atoi(m[2])
		k := key{m[1], n}
		if _, seen := files[k]; !seen || m[3] == "input" {
			files[k] = entry.Name()
		}
	}

	keys := make([]key, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].typ != keys[j].typ {
			return keys[i].typ < keys[j].typ
		}
		return keys[i].n < keys[j].n
	})

	out := make([]hwmonInput, 0, len(keys))
	for _, k := range keys {
		prefix := fmt.Sprintf("%s%d", k.typ, k.n)
		label := readTrimmed(filepath.Join(dir, prefix+"_label"))
		if label == "" {
			label = prefix
		}
		in := hwmonInput{file: files[k], label: label}
		switch k.typ {
		case "temp":
			in.kind, in.scale = sensorTemperature, 1000
			in.max = readTempLimit(filepath.Join(dir, prefix+"_max"))
			in.critical = readTempLimit(filepath.Join(dir, prefix+"_crit"))
		case "fan":
			in.kind, in.scale = sensorFan, 1
			in.max = readScaled(filepath.Join(dir, prefix+"_max"), 1)
		case "power":
			in.kind, in.scale = sensorPower, 1e6
			in.powerCap, in.powerMax = prefix+"_cap", prefix+"_max"
		case "freq":
			in.kind, in.scale = sensorFrequency, 1e6
		}
		out = append(out, in)
	}
	return out
}

// readTempLimit drops "unset" zeros and sentinel values no real limit has.
func readTempLimit(path string) *float64 {
	v := readScaled(path, 1000)
	if v != nil && *v > 200 {
		return nil
	}
	return v
}
