package models

type ProcessInfo struct {
	PID               int32   `json:"pid"`
	PPID              int32   `json:"ppid"`
	CPU               float64 `json:"cpu" doc:"Percentage of total machine CPU capacity used since the cursor, in the range 0-100."`
	PTicks            float64 `json:"pticks" doc:"Cumulative CPU seconds (user + system) consumed by this process."`
	MemoryPercent     float32 `json:"memoryPercent"`
	MemoryKB          uint64  `json:"memoryKB"`
	MemoryCalculation string  `json:"memoryCalculation"`
	RSSKB             uint64  `json:"rssKB"`
	RSSPercent        float32 `json:"rssPercent"`
	PSSKB             uint64  `json:"pssKB"`
	PSSPercent        float32 `json:"pssPercent"`
	Username          string  `json:"username"`
	Command           string  `json:"command"`
	FullCommand       string  `json:"fullCommand"`
	ExecutablePath    string  `json:"executablePath,omitempty"`
	ChildCount        int     `json:"childCount,omitempty"`
	// Only set for processes with an open GPU client when GPU sampling is enabled.
	GPU         *float64 `json:"gpu,omitempty" doc:"Percentage of the busiest GPU this process used since the cursor, in the range 0-100."`
	GPUMemoryKB *uint64  `json:"gpuMemoryKB,omitempty" doc:"Device-local GPU memory resident for this process, in KB."`
}

type ProcessCursorData struct {
	PID       int32   `json:"pid"`
	Ticks     float64 `json:"ticks"`
	Timestamp int64   `json:"timestamp"`
}

type ProcessListResponse struct {
	Processes []*ProcessInfo `json:"processes"`
	Cursor    string         `json:"cursor,omitempty"`
}
