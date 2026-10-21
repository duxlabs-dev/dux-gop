//go:build linux

package gops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

func getPssDirty(pid int32) (uint64, error) {
	smapsRollupPath := fmt.Sprintf("/proc/%d/smaps_rollup", pid)
	contents, err := os.ReadFile(smapsRollupPath)
	if err != nil {
		return 0, err
	}

	for _, line := range strings.Split(string(contents), "\n") {
		if !strings.HasPrefix(line, "Pss_Dirty:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			val, err := strconv.ParseUint(fields[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return val, nil
		}
	}
	return 0, fmt.Errorf("Pss_Dirty not found")
}

// Linux reports stat times in USER_HZ, which is 100 on every architecture.
const clockTicks = 100

var bootMillis = sync.OnceValue(func() int64 {
	t, err := host.BootTime()
	if err != nil {
		return 0
	}
	return int64(t) * 1000
})

// procReader reads /proc/<pid> files into one reused buffer, so it belongs to one goroutine.
type procReader struct {
	root string
	buf  []byte
}

func newProcReader() *procReader {
	return &procReader{root: "/proc", buf: make([]byte, 4096)}
}

func (self *procReader) path(pid int32, name string) string {
	return self.root + "/" + strconv.Itoa(int(pid)) + "/" + name
}

// read returns the file's contents, valid until the next read. A short read
// is the end: seq_file and cmdline reads fill the buffer while data remains.
func (self *procReader) read(pid int32, name string) ([]byte, error) {
	fd, err := unix.Open(self.path(pid, name), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(fd) }()
	n := 0
	for {
		m, err := unix.Read(fd, self.buf[n:])
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return nil, err
		}
		n += m
		if n < len(self.buf) {
			return self.buf[:n], nil
		}
		self.buf = append(self.buf, make([]byte, len(self.buf))...)
	}
}

func (self *procReader) stat(p *process.Process) (procStat, bool) {
	data, err := self.read(p.Pid, "stat")
	if err != nil {
		return procStat{}, false
	}
	st, ok := parseProcStat(string(data))
	if ok && bootMillis() > 0 {
		st.CreatedMillis = bootMillis() + int64(st.Start)*1000/clockTicks
	}
	return st, ok
}

// basics reads status, cmdline and the exe link, naming the process as
// gopsutil does: comm, extended from argv[0] when comm is truncated.
func (self *procReader) basics(p *process.Process) (b procBasics) {
	uid, uidOK := uint32(0), false
	if data, err := self.read(p.Pid, "status"); err == nil {
		b.Name, uid, uidOK, b.RSS = parseProcStatus(data)
	}
	if data, err := self.read(p.Pid, "cmdline"); err == nil {
		b.Cmdline = joinCmdline(data)
		if len(b.Name) >= 15 {
			argv0, _, _ := bytes.Cut(data, []byte{0})
			if ext := filepath.Base(string(argv0)); strings.HasPrefix(ext, b.Name) {
				b.Name = ext
			}
		}
	}
	if n, err := unix.Readlink(self.path(p.Pid, "exe"), self.buf); err == nil {
		b.Exe = string(self.buf[:n])
	}
	if uidOK {
		b.Username = usernames.lookup(uid)
	}
	return b
}

// The kernel escapes these two in status but not in comm.
var commUnescaper = strings.NewReplacer(`\n`, "\n", `\\`, `\`)

func parseProcStatus(data []byte) (name string, uid uint32, uidOK bool, rss uint64) {
	for line := range bytes.Lines(data) {
		key, val, ok := bytes.Cut(line, []byte{':'})
		if !ok {
			continue
		}
		switch string(key) {
		case "Name":
			name = string(bytes.Trim(val, " \t\n"))
			if strings.IndexByte(name, '\\') >= 0 {
				name = commUnescaper.Replace(name)
			}
		case "Uid":
			f, _, _ := bytes.Cut(bytes.TrimLeft(val, " \t"), []byte{'\t'})
			if v, err := strconv.ParseUint(string(f), 10, 32); err == nil {
				uid, uidOK = uint32(v), true
			}
		case "VmRSS":
			f := bytes.TrimSuffix(bytes.TrimSpace(val), []byte(" kB"))
			if v, err := strconv.ParseUint(string(bytes.TrimSpace(f)), 10, 64); err == nil {
				rss = v * 1024
			}
			return name, uid, uidOK, rss
		}
	}
	return name, uid, uidOK, rss
}

func joinCmdline(data []byte) string {
	var sb strings.Builder
	sb.Grow(len(data))
	for f := range bytes.FieldsFuncSeq(data, func(r rune) bool { return r == 0 }) {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.Write(f)
	}
	return sb.String()
}

// diskBytes returns cumulative disk bytes read and written by p. ok is false
// when /proc/<pid>/io is unreadable or the process exited.
func (self *procReader) diskBytes(p *process.Process) (read, write uint64, ok bool) {
	data, err := self.read(p.Pid, "io")
	if err != nil {
		return 0, 0, false
	}
	var gotRead, gotWrite bool
	for line := range bytes.Lines(data) {
		key, val, found := bytes.Cut(line, []byte(": "))
		if !found {
			continue
		}
		v, err := strconv.ParseUint(string(bytes.TrimSpace(val)), 10, 64)
		switch {
		case err != nil:
		case string(key) == "read_bytes":
			read, gotRead = v, true
		case string(key) == "write_bytes":
			write, gotWrite = v, true
		}
	}
	return read, write, gotRead && gotWrite
}

// listProcesses skips gopsutil's NewProcess, which reads stat for every PID to fill an unused creation time.
func listProcesses() ([]*process.Process, error) {
	pids, err := listProcPIDs("/proc")
	if err != nil {
		return nil, err
	}
	all := make([]process.Process, len(pids))
	procs := make([]*process.Process, len(pids))
	for i, pid := range pids {
		all[i].Pid = pid
		procs[i] = &all[i]
	}
	return procs, nil
}

// uidNames caches os/user lookups, dropped when /etc/passwd changes.
type uidNames struct {
	mu       sync.Mutex
	path     string
	mtime    time.Time
	size     int64
	names    map[uint32]string
	lookupID func(uid string) (*user.User, error)
}

var usernames = &uidNames{path: "/etc/passwd", lookupID: user.LookupId}

func refreshUsernames() { usernames.revalidate() }

func (self *uidNames) revalidate() {
	self.mu.Lock()
	defer self.mu.Unlock()
	fi, err := os.Stat(self.path)
	if err != nil {
		self.names = nil
		return
	}
	if !fi.ModTime().Equal(self.mtime) || fi.Size() != self.size {
		self.mtime, self.size, self.names = fi.ModTime(), fi.Size(), nil
	}
}

func (self *uidNames) lookup(uid uint32) string {
	self.mu.Lock()
	name, ok := self.names[uid]
	self.mu.Unlock()
	if ok {
		return name
	}
	if u, err := self.lookupID(strconv.FormatUint(uint64(uid), 10)); err == nil {
		name = u.Username
	}
	self.mu.Lock()
	if self.names == nil {
		self.names = map[uint32]string{}
	}
	self.names[uid] = name
	self.mu.Unlock()
	return name
}

// parseProcStat counts fields from the last ')', since the command name may
// contain spaces and parentheses.
func parseProcStat(text string) (procStat, bool) {
	return parseProcStatN(text, 20)
}

// parseProcStatN reads the first want fields after the command; 39 adds Ext.
func parseProcStatN(text string, want int) (procStat, bool) {
	i := strings.LastIndexByte(text, ')')
	if i < 0 {
		return procStat{}, false
	}
	var f [39]string
	n := 0
	for field := range strings.FieldsSeq(text[i+1:]) {
		f[n] = field
		if n++; n == want {
			break
		}
	}
	if n < 20 {
		return procStat{}, false
	}
	ppid, err1 := strconv.ParseInt(f[1], 10, 32)
	tty, err2 := strconv.Atoi(f[4])
	utime, err3 := strconv.ParseUint(f[11], 10, 64)
	stime, err4 := strconv.ParseUint(f[12], 10, 64)
	nice, err5 := strconv.Atoi(f[16])
	threads, err6 := strconv.Atoi(f[17])
	start, err7 := strconv.ParseUint(f[19], 10, 64)
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7); err != nil {
		return procStat{}, false
	}
	st := procStat{
		PPID:    int32(ppid),
		State:   f[0],
		Nice:    nice,
		Threads: threads,
		TTY:     tty,
		CPUTime: float64(utime+stime) / clockTicks,
		Start:   start,
	}
	if n == 39 {
		session, e1 := strconv.Atoi(f[3])
		minflt, e2 := strconv.ParseUint(f[7], 10, 64)
		majflt, e3 := strconv.ParseUint(f[9], 10, 64)
		prio, e4 := strconv.Atoi(f[15])
		cpu, e5 := strconv.Atoi(f[36])
		policy, e6 := strconv.Atoi(f[38])
		if errors.Join(e1, e2, e3, e4, e5, e6) == nil {
			st.Ext = &procStatExt{
				Session: session, MinFlt: minflt, MajFlt: majflt,
				UTime: float64(utime) / clockTicks, STime: float64(stime) / clockTicks,
				Priority: prio, Processor: cpu, Policy: policy,
			}
		}
	}
	return st, true
}
