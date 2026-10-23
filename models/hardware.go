package models

type BIOSInfo struct {
	Vendor      string `json:"vendor"`
	Version     string `json:"version"`
	Date        string `json:"date"`
	Motherboard string `json:"motherboard"`
}

type SystemHardware struct {
	Kernel   string   `json:"kernel"`
	Distro   string   `json:"distro"`
	Hostname string   `json:"hostname"`
	Arch     string   `json:"arch"`
	CPU      CPUBasic `json:"cpu"`
	BIOS     BIOSInfo `json:"bios"`
	Init     string   `json:"initSystem,omitempty" doc:"Name of PID 1, e.g. systemd"`
}

type CPUBasic struct {
	Count int    `json:"count"`
	Model string `json:"model"`
}

type GPU struct {
	Driver      string  `json:"driver"`
	Vendor      string  `json:"vendor"`
	DisplayName string  `json:"displayName"`
	FullName    string  `json:"fullName"`
	PciId       string  `json:"pciId"`
	RawLine     string  `json:"rawLine"`
	Temperature float64 `json:"temperature"`
	Hwmon       string  `json:"hwmon"`
	BusId       string  `json:"busId,omitempty"`

	// Live metrics are only filled for requested GPUs; nil means the driver does
	// not expose the value. Units: percent, KB, watts, MHz.
	State       string   `json:"state,omitempty"`
	Usage       *float64 `json:"usage,omitempty"`
	MemoryUsed  *uint64  `json:"memoryUsed,omitempty"`
	MemoryTotal *uint64  `json:"memoryTotal,omitempty"`
	Power       *float64 `json:"power,omitempty"`
	CoreClock   *float64 `json:"coreClock,omitempty"`
	MemoryClock *float64 `json:"memoryClock,omitempty"`

	GTTUsed           *uint64  `json:"gttUsed,omitempty" doc:"System memory mapped for the GPU, KB"`
	GTTTotal          *uint64  `json:"gttTotal,omitempty" doc:"KB"`
	MemoryBusy        *float64 `json:"memoryBusy,omitempty" doc:"Memory controller busy percentage"`
	EncoderUsage      *float64 `json:"encoderUsage,omitempty" doc:"Video encoder busy percentage"`
	DecoderUsage      *float64 `json:"decoderUsage,omitempty" doc:"Video decoder busy percentage"`
	CoreClockMax      *float64 `json:"coreClockMax,omitempty" doc:"MHz"`
	MemoryClockMax    *float64 `json:"memoryClockMax,omitempty" doc:"MHz"`
	Hotspot           *float64 `json:"hotspot,omitempty" doc:"Celsius"`
	MemoryTemperature *float64 `json:"memoryTemperature,omitempty" doc:"Celsius"`
	PowerLimit        *float64 `json:"powerLimit,omitempty" doc:"Watts"`
	FanRpm            *int     `json:"fanRpm,omitempty"`
	FanMaxRpm         *int     `json:"fanMaxRpm,omitempty"`
	FanPercent        *float64 `json:"fanPercent,omitempty"`
	PcieLinkSpeed     string   `json:"pcieLinkSpeed,omitempty" doc:"Kernel string, e.g. 16.0 GT/s PCIe"`
	PcieLinkSpeedMax  string   `json:"pcieLinkSpeedMax,omitempty"`
	PcieLinkWidth     int      `json:"pcieLinkWidth,omitempty"`
	PcieLinkWidthMax  int      `json:"pcieLinkWidthMax,omitempty"`
}

const (
	GPUStateActive    = "active"
	GPUStateSuspended = "suspended"
)

type GPUInfo struct {
	GPUs   []GPU  `json:"gpus"`
	Cursor string `json:"cursor,omitempty"`
}

type GPUTempInfo struct {
	Driver      string  `json:"driver"`
	Hwmon       string  `json:"hwmon"`
	Temperature float64 `json:"temperature"`
}

type GPUTempsInfo struct {
	GPUTemps []GPUTempInfo `json:"gputemps"`
}
