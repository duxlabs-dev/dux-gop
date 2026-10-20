package gops

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/net"
)

type NetworkRateCursor struct {
	Timestamp time.Time                     `json:"timestamp"`
	IOStats   map[string]net.IOCountersStat `json:"iostats"`
}

func (self *GopsUtil) GetNetworkRates(cursorStr string) (*models.NetworkRateResponse, error) {
	return self.GetNetworkRatesWithOptions(cursorStr, false)
}

// GetNetworkRatesWithOptions with allInterfaces set skips the interface name
// filter, so virtual interfaces such as VPN tunnels and lo are included.
func (self *GopsUtil) GetNetworkRatesWithOptions(cursorStr string, allInterfaces bool) (*models.NetworkRateResponse, error) {
	// Get current network stats
	netIO, err := net.IOCounters(true)
	if err != nil {
		return nil, err
	}

	currentStats := make(map[string]net.IOCountersStat)
	for _, n := range netIO {
		if includeNetworkInterface(n.Name, allInterfaces) {
			currentStats[n.Name] = n
		}
	}

	currentTime := time.Now()
	interfaces := make([]*models.NetworkRateInfo, 0)

	// If we have a cursor, calculate rates
	if cursorStr != "" {
		cursor, err := parseNetworkRateCursor(cursorStr)
		if err == nil {
			timeDiff := currentTime.Sub(cursor.Timestamp).Seconds()
			if timeDiff > 0 {
				for name, current := range currentStats {
					if prev, exists := cursor.IOStats[name]; exists {
						rxRate := float64(current.BytesRecv-prev.BytesRecv) / timeDiff
						txRate := float64(current.BytesSent-prev.BytesSent) / timeDiff

						interfaces = append(interfaces, &models.NetworkRateInfo{
							Interface: name,
							RxRate:    rxRate,
							TxRate:    txRate,
							RxTotal:   current.BytesRecv,
							TxTotal:   current.BytesSent,
						})
					}
				}
			}
		}
	}

	// If no cursor or no rates calculated, return zero rates
	if len(interfaces) == 0 {
		for name, current := range currentStats {
			interfaces = append(interfaces, &models.NetworkRateInfo{
				Interface: name,
				RxRate:    0,
				TxRate:    0,
				RxTotal:   current.BytesRecv,
				TxTotal:   current.BytesSent,
			})
		}
	}

	fillLinkInfos(interfaces)

	// Create new cursor
	newCursor := NetworkRateCursor{
		Timestamp: currentTime,
		IOStats:   currentStats,
	}

	newCursorStr, err := encodeNetworkRateCursor(newCursor)
	if err != nil {
		return nil, err
	}

	return &models.NetworkRateResponse{
		Interfaces: interfaces,
		Cursor:     newCursorStr,
	}, nil
}

func includeNetworkInterface(name string, allInterfaces bool) bool {
	return allInterfaces || matchesNetworkInterface(name)
}

func encodeNetworkRateCursor(cursor NetworkRateCursor) (string, error) {
	jsonData, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(jsonData), nil
}

func parseNetworkRateCursor(cursorStr string) (NetworkRateCursor, error) {
	var cursor NetworkRateCursor

	jsonData, err := base64.StdEncoding.DecodeString(cursorStr)
	if err != nil {
		return cursor, err
	}

	err = json.Unmarshal(jsonData, &cursor)
	return cursor, err
}
