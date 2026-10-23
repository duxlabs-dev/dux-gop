package gops

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
)

func (self *GopsUtil) GetSystemInfo() (*models.SystemInfo, error) {
	loadAvg, _ := load.Avg()
	bootTime, _ := host.BootTime()
	processes, threads := taskCounts()
	fileNr, _ := readFile("/proc/sys/fs/file-nr")

	return &models.SystemInfo{
		LoadAvg:   fmt.Sprintf("%.2f %.2f %.2f", loadAvg.Load1, loadAvg.Load5, loadAvg.Load15),
		Processes: processes,
		Threads:   threads,
		OpenFiles: parseFileNr(fileNr),
		BootTime:  time.Unix(int64(bootTime), 0).Format("2006-01-02 15:04:05"),
	}, nil
}

// parseFileNr returns the allocated file handles, the first field of /proc/sys/fs/file-nr.
func parseFileNr(text string) int {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(fields[0])
	return n
}
