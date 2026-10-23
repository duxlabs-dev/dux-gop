//go:build !linux

package gops

import "github.com/shirou/gopsutil/v4/process"

func getPssDirty(_ int32) (uint64, error) {
	return 0, nil
}

// readProcStat recovers because gopsutil can panic on macOS for system
// processes or ones that exit mid-read.
func readProcStat(p *process.Process) (st procStat, ok bool) {
	defer func() {
		if recover() != nil {
			st, ok = procStat{}, false
		}
	}()
	times, err := p.Times()
	if err != nil || times == nil {
		return procStat{}, false
	}
	st.CPUTime = times.User + times.System
	st.PPID, _ = p.Ppid()
	if c, err := p.CreateTime(); err == nil {
		st.CreatedMillis, st.Start = c, uint64(c)
	}
	return st, true
}

// processDiskBytes returns cumulative disk bytes read and written by p.
// ok is false when the counters are unavailable (exited process, FreeBSD),
// so callers must not treat that as zero.
func processDiskBytes(p *process.Process) (read, write uint64, ok bool) {
	if !processIOSupported {
		return 0, 0, false
	}
	io, err := p.IOCounters()
	if err != nil || io == nil {
		return 0, 0, false
	}
	return io.DiskReadBytes, io.DiskWriteBytes, true
}

func listProcesses() ([]*process.Process, error) {
	return process.Processes()
}

type procReader struct{}

func newProcReader() *procReader { return &procReader{} }

func (*procReader) stat(p *process.Process) (procStat, bool) { return readProcStat(p) }

func (*procReader) diskBytes(p *process.Process) (read, write uint64, ok bool) {
	return processDiskBytes(p)
}

func (*procReader) basics(p *process.Process) (b procBasics) {
	b.Name, _ = p.Name()
	b.Cmdline, _ = p.Cmdline()
	b.Exe, _ = p.Exe()
	b.Username, _ = p.Username()
	if m, _ := p.MemoryInfo(); m != nil {
		b.RSS = m.RSS
	}
	return b
}

func refreshUsernames() {}
