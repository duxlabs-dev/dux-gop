//go:build linux

package gops

import (
	"context"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

// queryNvidiaPIDs returns VRAM bytes per PID from `nvidia-smi -q -d PIDS`, which also lists graphics
// processes; nil when nvidia-smi is missing or fails. It runs one nvidia-smi per GPU in series,
// so a request can wait up to nvidiaSmiTimeout per named NVIDIA GPU.
func queryNvidiaPIDs(busIds []string) map[int32]uint64 {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}
	// -q documents --id for a single GPU only.
	var out []byte
	for _, id := range busIds {
		ctx, cancel := context.WithTimeout(context.Background(), nvidiaSmiTimeout)
		b, err := exec.CommandContext(ctx, path, "-q", "-d", "PIDS", "--id="+id).Output()
		cancel()
		if err != nil {
			return nil
		}
		out = append(out, b...)
	}
	return parseNvidiaPIDs(string(out))
}

// queryNvidiaPmon returns each process's usage from one `nvidia-smi pmon`
// sample of the given GPUs; nil when nvidia-smi is missing, fails or prints
// no table (pmon is unsupported on some GeForce boards).
func queryNvidiaPmon(busIds []string) map[int32]nvidiaUsage {
	path, err := exec.LookPath("nvidia-smi")
	if err != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSmiTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "pmon", "--count", "1", "--select", "u", "--id", strings.Join(busIds, ",")).Output()
	if err != nil {
		return nil
	}
	return parseNvidiaPmon(string(out))
}

var pmonVideoColumns = []string{"enc", "dec", "jpg"}

// parseNvidiaPmon reads columns by the header, which differs between driver
// versions, and keeps a process's highest value across GPUs. "-" means no
// work in the sample, which is 0%. nil without a header.
func parseNvidiaPmon(text string) map[int32]nvidiaUsage {
	var columns map[string]int
	var result map[int32]nvidiaUsage
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "#" {
			if columns == nil && slices.Contains(fields, "pid") && slices.Contains(fields, "sm") {
				columns = map[string]int{}
				for i, name := range fields[1:] {
					columns[name] = i
				}
				result = map[int32]nvidiaUsage{}
			}
			continue
		}
		if columns == nil {
			continue
		}
		value := func(name string) (float64, bool) {
			i, ok := columns[name]
			if !ok || i >= len(fields) {
				return 0, false
			}
			if fields[i] == "-" {
				return 0, true
			}
			v, err := strconv.ParseFloat(fields[i], 64)
			return min(v, 100), err == nil && v >= 0
		}
		pidCol, ok := columns["pid"]
		if !ok || pidCol >= len(fields) {
			continue
		}
		pid, err := strconv.ParseInt(fields[pidCol], 10, 32)
		sm, smOK := value("sm")
		if err != nil || pid <= 0 || !smOK {
			continue
		}
		u := result[int32(pid)]
		u.SM = max(u.SM, sm)
		for _, name := range pmonVideoColumns {
			if v, ok := value(name); ok {
				u.Video = max(u.Video, v)
			}
		}
		result[int32(pid)] = u
	}
	return result
}

// parseNvidiaPIDs sums "Used GPU Memory" per PID; a non-numeric value is kept as 0.
func parseNvidiaPIDs(text string) map[int32]uint64 {
	result := map[int32]uint64{}
	var pid int32
	for line := range strings.SplitSeq(text, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "Process ID":
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
			if err != nil || n <= 0 {
				n = 0
			}
			pid = int32(n)
		case "Used GPU Memory":
			if pid == 0 {
				continue
			}
			n, _ := parseDRMValue(value)
			result[pid] += n
		}
	}
	return result
}
