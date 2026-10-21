//go:build linux

package gops

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"
)

// GetProcessDetail reads the on-demand fields of one process. Fields that are
// unreadable (another user's process, an exited PID) stay unset.
func (self *GopsUtil) GetProcessDetail(pid int32) (*models.ProcessDetail, error) {
	dir := fmt.Sprintf("/proc/%d", pid)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("process %d: %w", pid, ErrProcessNotFound)
	} else if err != nil {
		return nil, err
	}
	d := &models.ProcessDetail{PID: pid}
	if read, write, ok := newProcReader().diskBytes(&process.Process{Pid: pid}); ok {
		d.ReadTotal, d.WriteTotal = &read, &write
	}
	if data, err := os.ReadFile(dir + "/status"); err == nil {
		status := string(data)
		d.SwapKB = procField(status, "VmSwap:")
		d.Privileges = parsePrivileges(status)
		fillStatusDetail(d, status)
	}
	if fds, err := os.ReadDir(dir + "/fd"); err == nil {
		d.OpenFiles = new(len(fds))
	}
	if data, err := os.ReadFile(dir + "/stat"); err == nil {
		st, _ := parseProcStatN(string(data), 39)
		d.Terminal = st.TTY != 0
		d.TTY = ttyName(st.TTY)
		if x := st.Ext; x != nil {
			d.UserTime, d.SystemTime = &x.UTime, &x.STime
			d.LastCPU, d.Priority, d.Session = &x.Processor, &x.Priority, &x.Session
			d.MinorFaults, d.MajorFaults = &x.MinFlt, &x.MajFlt
			d.Policy = schedPolicy(x.Policy)
		}
		d.IOClass, d.IOLevel = ioPriority(pid, st.Nice)
	}
	if data, err := os.ReadFile(dir + "/cgroup"); err == nil {
		d.Unit = cgroupUnit(string(data))
		d.Cgroup = cgroupPath(string(data))
	}
	d.OOMScore = readProcInt(dir + "/oom_score")
	d.OOMScoreAdj = readProcInt(dir + "/oom_score_adj")
	if cwd, err := os.Readlink(dir + "/cwd"); err == nil {
		d.Cwd = cwd
	}
	if scan, err := scanProcFDs("/proc", pid, nil, true); err == nil {
		d.Listening = listeningSockets(dir+"/net", scan.SocketInodes)
	}
	d.Sandbox = detectSandbox(dir)
	return d, nil
}

func fillStatusDetail(d *models.ProcessDetail, status string) {
	d.RSSKB = procField(status, "VmRSS:")
	d.RSSAnonKB = procField(status, "RssAnon:")
	d.RSSFileKB = procField(status, "RssFile:")
	d.RSSShmemKB = procField(status, "RssShmem:")
	d.PeakKB = procField(status, "VmHWM:")
	d.VirtualKB = procField(status, "VmSize:")
	d.VoluntarySwitches = procField(status, "voluntary_ctxt_switches:")
	d.InvoluntarySwitches = procField(status, "nonvoluntary_ctxt_switches:")
	for _, l := range strings.Split(status, "\n") {
		if v, ok := strings.CutPrefix(l, "Cpus_allowed_list:"); ok {
			d.Affinity = strings.TrimSpace(v)
		}
	}
	d.UID = firstID(status, "Uid:")
	if d.GID = firstID(status, "Gid:"); d.GID != nil {
		if g, err := user.LookupGroupId(strconv.Itoa(*d.GID)); err == nil {
			d.Group = g.Name
		}
	}
}

func firstID(status, key string) *int {
	for _, l := range strings.Split(status, "\n") {
		if v, ok := strings.CutPrefix(l, key); ok {
			if f := strings.Fields(v); len(f) > 0 {
				if n, err := strconv.Atoi(f[0]); err == nil {
					return &n
				}
			}
		}
	}
	return nil
}

func readProcInt(path string) *int {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return nil
	}
	return &n
}

func cgroupPath(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if p, ok := strings.CutPrefix(line, "0::"); ok {
			return p
		}
	}
	return ""
}

func schedPolicy(n int) string {
	switch n {
	case 0:
		return "normal"
	case 1:
		return "fifo"
	case 2:
		return "rr"
	case 3:
		return "batch"
	case 5:
		return "idle"
	case 6:
		return "deadline"
	}
	return ""
}

// ttyName decodes the stat tty_nr device number.
func ttyName(nr int) string {
	major, minor := nr>>8&0xfff, nr&0xff|nr>>12&0xfff00
	switch {
	case major >= 136 && major <= 143:
		return fmt.Sprintf("pts/%d", minor+(major-136)*256)
	case major == 4 && minor < 64:
		return fmt.Sprintf("tty%d", minor)
	case major == 4:
		return fmt.Sprintf("ttyS%d", minor-64)
	}
	return ""
}

// ioPriority reads the I/O class and level; class "none" derives its level from nice.
func ioPriority(pid int32, nice int) (string, *int) {
	v, _, errno := unix.Syscall(unix.SYS_IOPRIO_GET, 1, uintptr(pid), 0)
	if errno != 0 {
		return "", nil
	}
	return decodeIOPrio(int(v), nice)
}

func decodeIOPrio(v, nice int) (string, *int) {
	level := v & 0xff
	var class string
	switch v >> 13 {
	case 0:
		class, level = "none", (nice+20)/5
	case 1:
		class = "realtime"
	case 2:
		class = "best-effort"
	case 3:
		class = "idle"
	default:
		return "", nil
	}
	return class, &level
}

func procField(text, key string) *uint64 {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, key) {
			continue
		}
		fields := strings.Fields(line[len(key):])
		if len(fields) == 0 {
			return nil
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return nil
		}
		return new(v)
	}
	return nil
}

// cgroupUnit returns the innermost .service or .scope of the cgroup v2 path
// ("0::/user.slice/.../app.slice/app-foo@1.scope" -> "app-foo@1.scope").
func cgroupUnit(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if !strings.HasPrefix(line, "0::") {
			continue
		}
		parts := strings.Split(strings.TrimPrefix(line, "0::"), "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if strings.HasSuffix(parts[i], ".service") || strings.HasSuffix(parts[i], ".scope") {
				return parts[i]
			}
		}
	}
	return ""
}

// parseListenTable returns the listening sockets of one /proc/net table whose
// inode is in inodes: TCP in LISTEN state, UDP with no remote port.
func parseListenTable(text, proto string, ipv6 bool, inodes map[uint32]bool) []models.ListenSocket {
	var out []models.ListenSocket
	lines := strings.Split(text, "\n")
	for _, l := range lines[min(1, len(lines)):] {
		f := strings.Fields(l)
		if len(f) < 10 {
			continue
		}
		if proto == "tcp" && f[3] != "0A" {
			continue
		}
		local, remote := strings.Split(f[1], ":"), strings.Split(f[2], ":")
		if len(local) != 2 || len(remote) != 2 {
			continue
		}
		if proto == "udp" {
			if rp, err := strconv.ParseUint(remote[1], 16, 16); err != nil || rp != 0 {
				continue
			}
		}
		inode, err := strconv.ParseUint(f[9], 10, 32)
		if err != nil || !inodes[uint32(inode)] {
			continue
		}
		port, err := strconv.ParseUint(local[1], 16, 16)
		addr, ok := parseHexAddr(local[0], ipv6)
		if err != nil || !ok {
			continue
		}
		out = append(out, models.ListenSocket{Protocol: proto, Address: addr.String(), Port: int(port)})
	}
	return out
}

func parseHexAddr(h string, ipv6 bool) (netip.Addr, bool) {
	n := 4
	if ipv6 {
		n = 16
	}
	if len(h) != n*2 {
		return netip.Addr{}, false
	}
	b := make([]byte, n)
	for i := 0; i < n; i += 4 {
		w, err := strconv.ParseUint(h[i*2:i*2+8], 16, 32)
		if err != nil {
			return netip.Addr{}, false
		}
		binary.NativeEndian.PutUint32(b[i:], uint32(w))
	}
	a, _ := netip.AddrFromSlice(b)
	return a, true
}

// listeningSockets reads the process's own net tables, so its network namespace applies.
func listeningSockets(netDir string, inodes []uint32) []models.ListenSocket {
	if len(inodes) == 0 {
		return nil
	}
	set := make(map[uint32]bool, len(inodes))
	for _, i := range inodes {
		set[i] = true
	}
	var out []models.ListenSocket
	for _, t := range []struct {
		file, proto string
		v6          bool
	}{{"tcp", "tcp", false}, {"tcp6", "tcp", true}, {"udp", "udp", false}, {"udp6", "udp", true}} {
		if data, err := os.ReadFile(netDir + "/" + t.file); err == nil {
			out = append(out, parseListenTable(string(data), t.proto, t.v6, set)...)
		}
	}
	slices.SortFunc(out, func(a, b models.ListenSocket) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		return strings.Compare(a.Protocol, b.Protocol)
	})
	return slices.Compact(out)
}

func flatpakAppID(info string) string {
	in := false
	sc := bufio.NewScanner(strings.NewReader(info))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(l, "[") {
			in = l == "[Application]"
		} else if v, ok := strings.CutPrefix(l, "name="); ok && in {
			return v
		}
	}
	return ""
}

func sandboxFromCgroup(text string) *models.Sandbox {
	var container *models.Sandbox
	for _, seg := range strings.FieldsFunc(text, func(r rune) bool { return r == '/' || r == '\n' }) {
		if rest, ok := strings.CutPrefix(seg, "snap."); ok {
			name, _, _ := strings.Cut(rest, ".")
			return &models.Sandbox{Kind: "snap", ID: name}
		}
		for _, p := range []string{"docker-", "libpod-"} {
			if id, ok := strings.CutPrefix(seg, p); ok && strings.HasSuffix(seg, ".scope") {
				container = &models.Sandbox{Kind: "container", ID: id[:min(12, len(id)-len(".scope"))]}
			}
		}
	}
	return container
}

func detectSandbox(dir string) *models.Sandbox {
	if info, err := os.ReadFile(dir + "/root/.flatpak-info"); err == nil {
		return &models.Sandbox{Kind: "flatpak", ID: flatpakAppID(string(info))}
	}
	cg, _ := os.ReadFile(dir + "/cgroup")
	sb := sandboxFromCgroup(string(cg))
	if sb != nil && sb.Kind == "snap" {
		return sb
	}
	ns, err1 := os.Readlink(dir + "/ns/pid")
	self, err2 := os.Readlink("/proc/self/ns/pid")
	if err1 != nil || err2 != nil || ns == self {
		return nil
	}
	if sb == nil {
		sb = &models.Sandbox{Kind: "container"}
	}
	return sb
}

var capNames = [...]string{
	"cap_chown", "cap_dac_override", "cap_dac_read_search", "cap_fowner", "cap_fsetid", "cap_kill",
	"cap_setgid", "cap_setuid", "cap_setpcap", "cap_linux_immutable", "cap_net_bind_service",
	"cap_net_broadcast", "cap_net_admin", "cap_net_raw", "cap_ipc_lock", "cap_ipc_owner",
	"cap_sys_module", "cap_sys_rawio", "cap_sys_chroot", "cap_sys_ptrace", "cap_sys_pacct",
	"cap_sys_admin", "cap_sys_boot", "cap_sys_nice", "cap_sys_resource", "cap_sys_time",
	"cap_sys_tty_config", "cap_mknod", "cap_lease", "cap_audit_write", "cap_audit_control",
	"cap_setfcap", "cap_mac_override", "cap_mac_admin", "cap_syslog", "cap_wake_alarm",
	"cap_block_suspend", "cap_audit_read", "cap_perfmon", "cap_bpf", "cap_checkpoint_restore",
}

func parsePrivileges(status string) *models.Privileges {
	p := &models.Privileges{}
	euid := uint64(0)
	for _, l := range strings.Split(status, "\n") {
		if v, ok := strings.CutPrefix(l, "Uid:"); ok {
			f := strings.Fields(v)
			if len(f) < 2 {
				return nil
			}
			e, err := strconv.ParseUint(f[1], 10, 32)
			if err != nil {
				return nil
			}
			euid, p.Setuid = e, f[0] != f[1]
		}
	}
	if euid != 0 {
		if v := procHexField(status, "CapEff:"); v != 0 {
			for i, n := range capNames {
				if v>>i&1 == 1 {
					p.Capabilities = append(p.Capabilities, n)
				}
			}
		}
	}
	if !p.Setuid && len(p.Capabilities) == 0 {
		return nil
	}
	return p
}

func procHexField(text, key string) uint64 {
	for _, l := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(l, key); ok {
			n, _ := strconv.ParseUint(strings.TrimSpace(v), 16, 64)
			return n
		}
	}
	return 0
}
