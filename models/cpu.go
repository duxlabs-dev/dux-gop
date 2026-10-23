package models

type CPUInfo struct {
	Count       int         `json:"count"`
	Model       string      `json:"model"`
	Frequency   float64     `json:"frequency"`
	Temperature float64     `json:"temperature"`
	Usage       float64     `json:"usage"`
	CoreUsage   []float64   `json:"coreUsage"`
	Total       []float64   `json:"total"`
	Cores       [][]float64 `json:"cores"`
	Cursor      string      `json:"cursor,omitempty"`

	CoreFrequencies []float64 `json:"coreFrequencies,omitempty" doc:"Per-logical-CPU clock in MHz; 0 for offline CPUs"`
	MinFrequency    float64   `json:"minFrequency,omitempty" doc:"Lowest supported clock in MHz"`
	MaxFrequency    float64   `json:"maxFrequency,omitempty" doc:"Highest supported clock in MHz"`
	Governor        string    `json:"governor,omitempty"`
	EPP             string    `json:"epp,omitempty" doc:"Energy performance preference"`
	PhysicalCores   int       `json:"physicalCores,omitempty"`
	BaseFrequency   float64   `json:"baseFrequency,omitempty" doc:"Nominal (base) clock in MHz"`
	Sockets         int       `json:"sockets,omitempty"`
	Virtualization  string    `json:"virtualization,omitempty" doc:"AMD-V or VT-x when the CPU flag is present"`
	Cache           *CPUCache `json:"cache,omitempty"`
}

// CPUCache holds total cache sizes in bytes, summed over all instances.
type CPUCache struct {
	L1d uint64 `json:"l1d"`
	L1i uint64 `json:"l1i"`
	L2  uint64 `json:"l2"`
	L3  uint64 `json:"l3"`
}

type CPUCursorData struct {
	Total     []float64   `json:"total"`
	Cores     [][]float64 `json:"cores"`
	Timestamp int64       `json:"timestamp"`
}
