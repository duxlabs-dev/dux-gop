package models

type SystemMetrics struct {
	Memory     *MemoryInfo      `json:"memory"`
	CPU        *CPUInfo         `json:"cpu"`
	Network    []*NetworkInfo   `json:"network"`
	Disk       []*DiskInfo      `json:"disk"`
	Processes  []*ProcessInfo   `json:"processes"`
	System     *SystemInfo      `json:"system"`
	DiskMounts []*DiskMountInfo `json:"diskmounts"`
}

type SystemInfo struct {
	LoadAvg   string `json:"loadavg"`
	Processes int    `json:"processes"`
	Threads   int    `json:"threads"`
	OpenFiles int    `json:"openFiles,omitempty" doc:"System-wide open file handles"`
	BootTime  string `json:"boottime"`
}

type MetaInfo struct {
	CPU        *CPUInfo             `json:"cpu,omitempty"`
	Memory     *MemoryInfo          `json:"memory,omitempty"`
	Network    []*NetworkInfo       `json:"network,omitempty"`
	NetRate    *NetworkRateResponse `json:"netrate,omitempty"`
	Disk       []*DiskInfo          `json:"disk,omitempty"`
	DiskRate   *DiskRateResponse    `json:"diskrate,omitempty"`
	DiskMounts []*DiskMountInfo     `json:"diskmounts,omitempty"`
	Processes  []*ProcessInfo       `json:"processes,omitempty"`
	System     *SystemInfo          `json:"system,omitempty"`
	Hardware   *SystemHardware      `json:"hardware,omitempty"`
	GPU        *GPUInfo             `json:"gpu,omitempty"`

	Sensors       *SensorsInfo       `json:"sensors,omitempty"`
	MemoryModules *MemoryModulesInfo `json:"memorymodules,omitempty"`
	Cursor        string             `json:"cursor,omitempty"`
}

type ModulesInfo struct {
	Available []string `json:"available"`
}
