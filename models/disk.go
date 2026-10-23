package models

type DiskInfo struct {
	Name  string `json:"name"`
	Read  uint64 `json:"read"`
	Write uint64 `json:"write"`
}

type DiskMountInfo struct {
	Device  string `json:"device"`
	Mount   string `json:"mount"`
	FSType  string `json:"fstype"`
	Size    string `json:"size"`
	Used    string `json:"used"`
	Avail   string `json:"avail"`
	Percent string `json:"percent"`
}

type DiskRateInfo struct {
	Device     string  `json:"device"`
	ReadRate   float64 `json:"readrate"`
	WriteRate  float64 `json:"writerate"`
	ReadTotal  uint64  `json:"readtotal"`
	WriteTotal uint64  `json:"writetotal"`
	ReadCount  uint64  `json:"readcount"`
	WriteCount uint64  `json:"writecount"`

	Busy         *float64 `json:"busy,omitempty" doc:"Percent of wall time with I/O in flight"`
	ResponseTime *float64 `json:"responseTime,omitempty" doc:"Mean time per completed I/O in milliseconds; absent when no I/O completed"`
}

type DiskRateResponse struct {
	Disks  []*DiskRateInfo `json:"disks"`
	Cursor string          `json:"cursor"`
}
