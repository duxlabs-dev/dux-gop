//go:build linux

package gops

import (
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isInternetAddr reports whether a is a global unicast address outside
// private (RFC 1918, ULA) and CGNAT ranges. Documentation, benchmark, 6to4,
// Teredo and NAT64 ranges count as internet on purpose: they are never LAN peers.
func isInternetAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsValid() && a.IsGlobalUnicast() && !a.IsPrivate() && !cgnat.Contains(a)
}

// isLocalAddr reports a unicast remote that is not internet: private,
// CGNAT (e.g. Tailscale) or link-local. Loopback is neither.
func isLocalAddr(a netip.Addr) bool {
	a = a.Unmap()
	return (a.IsGlobalUnicast() || a.IsLinkLocalUnicast()) && !isInternetAddr(a)
}

func countedRemote(a netip.Addr) bool { return isInternetAddr(a) || isLocalAddr(a) }

var ownNetNS = sync.OnceValue(func() string {
	ns, _ := os.Readlink("/proc/self/ns/net")
	return ns
})

type procNetReader struct {
	netDump
	socks map[uint32]sockDiagEntry
	ok    bool
	// netNS is our network namespace; empty skips the check.
	netNS string

	nsMu       sync.Mutex
	loopbackNS map[string]bool
}

func (self *GopsUtil) newProcNetReader() *procNetReader {
	self.tails.start(self.sockDestroys)
	r := &procNetReader{netDump: netDump{tail: self.tails.get}, netNS: ownNetNS()}
	at := time.Now().UnixMilli()
	entries, err := self.sockDump()
	if err != nil {
		return r
	}
	r.at = at
	r.socks = make(map[uint32]sockDiagEntry, len(entries))
	for _, e := range entries {
		if !countedRemote(e.Remote.Addr()) {
			continue
		}
		if e.Inode != 0 {
			r.socks[e.Inode] = e
		}
		if e.Protocol == unix.IPPROTO_TCP && e.HasTCPInfo {
			r.netDump.socks = append(r.netDump.socks, dumpSocket{tcpCounters{e.Cookie, e.RxBytes, e.TxBytes}, e.Inode == 0})
		}
	}
	r.ok = true
	return r
}

// A socket held by several fds counts once.
func (self *procNetReader) fromScan(scan procFDScan) procNet {
	if !self.ok {
		return procNet{}
	}
	out := procNet{Known: true}
	seen := make(map[uint32]bool, len(scan.SocketInodes))
	for _, inode := range scan.SocketInodes {
		if seen[inode] {
			continue
		}
		seen[inode] = true
		e, found := self.socks[inode]
		if !found {
			continue
		}
		remote := e.Remote.Addr().Unmap()
		local := !isInternetAddr(remote)
		if local {
			out.LocalCount++
		} else {
			out.Count++
		}
		proto := "udp"
		if e.Protocol == unix.IPPROTO_TCP {
			proto = "tcp"
			if e.HasTCPInfo {
				out.TCP = append(out.TCP, tcpCounters{e.Cookie, e.RxBytes, e.TxBytes})
			} else {
				out.RatesUnknown = true
			}
		} else {
			out.UDP = true
		}
		if len(out.Remotes) < maxProcRemotes {
			out.Remotes = append(out.Remotes, procRemote{proto, netip.AddrPortFrom(remote, e.Remote.Port()).String(), local})
		}
	}
	return out
}

// foreign reports another network namespace; an unreadable ns link counts as ours.
func (self *procNetReader) foreign(scan procFDScan) bool {
	return self.netNS != "" && scan.NetNS != "" && scan.NetNS != self.netNS
}

// loopbackOnly reports a namespace whose only interface is lo (a browser sandbox).
func (self *procNetReader) loopbackOnly(procRoot string, pid int32, ns string) bool {
	self.nsMu.Lock()
	only, cached := self.loopbackNS[ns]
	self.nsMu.Unlock()
	if cached {
		return only
	}
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(int(pid)), "net", "dev"))
	lines := strings.Split(string(data), "\n")
	only = err == nil && len(lines) > 2
	for _, l := range lines[min(2, len(lines)):] {
		if name, _, found := strings.Cut(l, ":"); found && strings.TrimSpace(name) != "lo" {
			only = false
		}
	}
	self.nsMu.Lock()
	if self.loopbackNS == nil {
		self.loopbackNS = map[string]bool{}
	}
	self.loopbackNS[ns] = only
	self.nsMu.Unlock()
	return only
}

const (
	sockTailMaxAge = 2 * time.Minute
	maxSockTails   = 1 << 16
)

// sockTails keeps the final counters of destroyed sockets so the traffic between
// a socket's last dump and its close still counts. Entries are not consumed.
type sockTails struct {
	mu      sync.Mutex
	started bool
	stop    func()
	final   map[uint64]sockTail
	pruned  time.Time
}

type sockTail struct {
	rx, tx uint64
	at     time.Time
}

func (self *sockTails) start(listen func(emit func(sockDiagEntry)) (func(), error)) {
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.started || listen == nil {
		return
	}
	self.started = true
	stop, err := listen(func(e sockDiagEntry) { self.record(e, time.Now()) })
	if err == nil {
		self.stop = stop
	}
}

func (self *sockTails) close() {
	self.mu.Lock()
	stop := self.stop
	self.started, self.stop = true, nil
	self.mu.Unlock()
	if stop != nil {
		stop()
	}
}

func (self *sockTails) record(e sockDiagEntry, now time.Time) {
	if !e.HasTCPInfo || !countedRemote(e.Remote.Addr()) {
		return
	}
	self.mu.Lock()
	defer self.mu.Unlock()
	if self.final == nil {
		self.final = map[uint64]sockTail{}
	}
	if now.Sub(self.pruned) > sockTailMaxAge/4 {
		for cookie, t := range self.final {
			if now.Sub(t.at) > sockTailMaxAge {
				delete(self.final, cookie)
			}
		}
		self.pruned = now
	}
	if len(self.final) < maxSockTails {
		self.final[e.Cookie] = sockTail{e.RxBytes, e.TxBytes, now}
	}
}

func (self *sockTails) get(cookie uint64) (rx, tx uint64, ok bool) {
	self.mu.Lock()
	defer self.mu.Unlock()
	t, ok := self.final[cookie]
	return t.rx, t.tx, ok
}
