//go:build linux

package gops

import (
	"os"
	"strconv"
	"strings"
)

func taskCounts() (processes, threads int) {
	pids, _ := listProcPIDs("/proc")
	processes = len(pids)

	loadavg, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return processes, 0
	}
	return processes, parseLoadavgTasks(string(loadavg))
}

// Fourth /proc/loadavg field is running/total kernel scheduling entities, i.e. threads.
func parseLoadavgTasks(loadavg string) int {
	fields := strings.Fields(loadavg)
	if len(fields) < 4 {
		return 0
	}
	_, total, ok := strings.Cut(fields[3], "/")
	if !ok {
		return 0
	}
	threads, err := strconv.Atoi(total)
	if err != nil {
		return 0
	}
	return threads
}
