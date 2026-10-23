//go:build linux

package gops

import (
	"context"
	"os/exec"
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
