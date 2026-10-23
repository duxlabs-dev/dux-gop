package gops

import (
	"encoding/base64"
	"encoding/json"
	"math"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/disk"
)

type DiskRateCursor struct {
	Timestamp time.Time                      `json:"timestamp"`
	IOStats   map[string]disk.IOCountersStat `json:"iostats"`
}

func (self *GopsUtil) GetDiskRates(cursorStr string) (*models.DiskRateResponse, error) {
	// Get current disk stats
	diskIO, err := disk.IOCounters()
	if err != nil {
		return nil, err
	}

	currentStats := make(map[string]disk.IOCountersStat)
	for name, stats := range diskIO {
		currentStats[name] = stats
	}

	currentTime := time.Now()
	disks := make([]*models.DiskRateInfo, 0)

	// If we have a cursor, calculate rates
	if cursorStr != "" {
		cursor, err := parseDiskRateCursor(cursorStr)
		if err == nil {
			timeDiff := currentTime.Sub(cursor.Timestamp).Seconds()
			if timeDiff > 0 {
				for name, current := range currentStats {
					if prev, exists := cursor.IOStats[name]; exists {
						readRate := float64(current.ReadBytes-prev.ReadBytes) / timeDiff
						writeRate := float64(current.WriteBytes-prev.WriteBytes) / timeDiff

						info := &models.DiskRateInfo{
							Device:     name,
							ReadRate:   readRate,
							WriteRate:  writeRate,
							ReadTotal:  current.ReadBytes,
							WriteTotal: current.WriteBytes,
							ReadCount:  current.ReadCount,
							WriteCount: current.WriteCount,
						}
						if diskBusySupported {
							info.Busy, info.ResponseTime = diskBusyAndResponse(prev, current, timeDiff)
						}
						disks = append(disks, info)
					}
				}
			}
		}
	}

	// If no cursor or no rates calculated, return zero rates
	if len(disks) == 0 {
		for name, current := range currentStats {
			disks = append(disks, &models.DiskRateInfo{
				Device:     name,
				ReadRate:   0,
				WriteRate:  0,
				ReadTotal:  current.ReadBytes,
				WriteTotal: current.WriteBytes,
				ReadCount:  current.ReadCount,
				WriteCount: current.WriteCount,
			})
		}
	}

	// Create new cursor
	newCursor := DiskRateCursor{
		Timestamp: currentTime,
		IOStats:   currentStats,
	}

	newCursorStr, err := encodeDiskRateCursor(newCursor)
	if err != nil {
		return nil, err
	}

	return &models.DiskRateResponse{
		Disks:  disks,
		Cursor: newCursorStr,
	}, nil
}

// diskBusyAndResponse derives busy % and mean response time (ms) between two
// samples. Either is nil when its counters went backwards; response is also
// nil when no I/O completed.
func diskBusyAndResponse(prev, cur disk.IOCountersStat, elapsedSec float64) (busy, response *float64) {
	if cur.IoTime >= prev.IoTime && elapsedSec > 0 {
		b := float64(cur.IoTime-prev.IoTime) / (elapsedSec * 1000) * 100
		b = math.Min(math.Max(b, 0), 100)
		busy = &b
	}
	if cur.ReadCount >= prev.ReadCount && cur.WriteCount >= prev.WriteCount &&
		cur.ReadTime >= prev.ReadTime && cur.WriteTime >= prev.WriteTime {
		if ops := (cur.ReadCount - prev.ReadCount) + (cur.WriteCount - prev.WriteCount); ops > 0 {
			response = new(float64((cur.ReadTime-prev.ReadTime)+(cur.WriteTime-prev.WriteTime)) / float64(ops))
		}
	}
	return busy, response
}

func encodeDiskRateCursor(cursor DiskRateCursor) (string, error) {
	jsonData, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(jsonData), nil
}

func parseDiskRateCursor(cursorStr string) (DiskRateCursor, error) {
	var cursor DiskRateCursor

	jsonData, err := base64.StdEncoding.DecodeString(cursorStr)
	if err != nil {
		return cursor, err
	}

	err = json.Unmarshal(jsonData, &cursor)
	return cursor, err
}
