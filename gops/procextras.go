package gops

// procGPU is one process's GPU readings. Readable false means everything is
// unknown. UsageKnown false means a percentage can't be derived (a driver
// without engine data, NVIDIA without nvidia-smi pmon, or a GPU we don't
// classify), so only VRAM is as read.
type procGPU struct {
	Readable bool
	// Unmonitored: unknown because the process holds a GPU that is not queried.
	Unmonitored bool
	UsageKnown  bool
	VRAMBytes   uint64
	GTTBytes    uint64
	// Clients that report engines, for rating.
	Clients drmClientSet
	// Nvidia is the process's share of the NVIDIA GPUs from nvidia-smi pmon,
	// set only when it is known.
	Nvidia *nvidiaUsage
}

// nvidiaUsage is a process's busiest SM and video (encode, decode, JPEG)
// percentage in nvidia-smi pmon's latest sample, the highest across GPUs.
type nvidiaUsage struct {
	SM    float64
	Video float64
}

type gpuCursorClient struct {
	PDev string `json:"p"`
	ID   uint64 `json:"i"`
	// Engine name -> busy ns, cycles, total cycles.
	Engines map[string][3]uint64 `json:"e"`
}

type procRemote struct {
	Protocol string
	Remote   string
	Local    bool
}

type tcpCounters struct {
	Cookie  uint64
	RxBytes uint64
	TxBytes uint64
}

type procNet struct {
	// Known is false when the process's fds or the socket dump were
	// unreadable, or the process is in another network namespace with
	// interfaces besides lo.
	Known bool
	// Count is the exact number of sockets with an internet remote,
	// LocalCount those with a LAN, VPN or link-local one.
	Count      int
	LocalCount int
	Remotes    []procRemote
	// TCP lists the non-loopback TCP sockets.
	TCP []tcpCounters
	// UDP is set when a non-loopback UDP socket is open; UDP has no byte counters.
	UDP bool
	// RatesUnknown is set when a TCP socket came without byte counters, so
	// summing the rest would under-report.
	RatesUnknown bool
}

type dumpSocket struct {
	tcpCounters
	// Orphan is a socket whose last fd closed; it lingers until the
	// connection finishes closing.
	Orphan bool
}

// netDump is what one socket dump contributes to rates: its time (Unix ms),
// every non-loopback TCP socket with counters, and the final counters of
// sockets destroyed since, when known.
type netDump struct {
	at    int64
	socks []dumpSocket
	tail  func(cookie uint64) (rx, tx uint64, ok bool)
}
