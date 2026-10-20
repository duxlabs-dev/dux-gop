//go:build linux

package gops

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"

	"github.com/duxlabs-dev/dux-gop/models"
)

const netSysRoot = "/sys/class/net"

// ARPHRD types of IP tunnels (tun/WireGuard report none, 65534).
var tunnelARPTypes = map[string]bool{"65534": true, "768": true, "769": true, "776": true, "778": true, "823": true}

func fillLinkInfos(infos []*models.NetworkRateInfo) {
	addrs := interfaceAddrs()
	for _, info := range infos {
		for _, ipnet := range addrs[readLinkSysfs(netSysRoot, info)] {
			if ipnet.IP.To4() != nil {
				info.IPv4 = append(info.IPv4, ipnet.String())
			} else {
				info.IPv6 = append(info.IPv6, ipnet.String())
			}
		}
	}
}

// interfaceAddrs returns the addresses of every interface by index, from one
// netlink dump. Like net.Interface.Addrs, it takes IFA_LOCAL over
// IFA_ADDRESS, which is the peer on point-to-point links.
func interfaceAddrs() map[int][]net.IPNet {
	tab, err := syscall.NetlinkRIB(syscall.RTM_GETADDR, syscall.AF_UNSPEC)
	if err != nil {
		return nil
	}
	msgs, err := syscall.ParseNetlinkMessage(tab)
	if err != nil {
		return nil
	}
	out := map[int][]net.IPNet{}
	for _, m := range msgs {
		if m.Header.Type != syscall.RTM_NEWADDR || len(m.Data) < syscall.SizeofIfAddrmsg {
			continue
		}
		family, prefix := m.Data[0], int(m.Data[1])
		index := int(binary.NativeEndian.Uint32(m.Data[4:8]))
		attrs, err := syscall.ParseNetlinkRouteAttr(&m)
		if err != nil {
			continue
		}
		var ip []byte
		for _, a := range attrs {
			if a.Attr.Type == syscall.IFA_LOCAL || a.Attr.Type == syscall.IFA_ADDRESS && ip == nil {
				ip = a.Value
			}
		}
		switch {
		case family == syscall.AF_INET && len(ip) >= net.IPv4len:
			out[index] = append(out[index], net.IPNet{IP: net.IPv4(ip[0], ip[1], ip[2], ip[3]), Mask: net.CIDRMask(prefix, 8*net.IPv4len)})
		case family == syscall.AF_INET6 && len(ip) >= net.IPv6len:
			out[index] = append(out[index], net.IPNet{IP: net.IP(append([]byte(nil), ip[:net.IPv6len]...)), Mask: net.CIDRMask(prefix, 8*net.IPv6len)})
		}
	}
	return out
}

type linkStatic struct {
	index    int
	driver   string
	linkType string
}

// Interface names churn (veth pairs), so the cache is cleared once it grows
// past maxLinkStatics rather than tracked per name.
const maxLinkStatics = 128

var linkStatics = struct {
	sync.Mutex
	m map[string]linkStatic
}{m: map[string]linkStatic{}}

func readLinkSysfs(root string, info *models.NetworkRateInfo) int {
	dir := filepath.Join(root, info.Interface)
	index, _ := strconv.Atoi(readTrimmed(filepath.Join(dir, "ifindex")))
	linkStatics.Lock()
	st, ok := linkStatics.m[dir]
	linkStatics.Unlock()
	if !ok || index == 0 || st.index != index {
		st = readLinkStatic(dir)
		st.index = index
		if index != 0 {
			linkStatics.Lock()
			if len(linkStatics.m) >= maxLinkStatics {
				clear(linkStatics.m)
			}
			linkStatics.m[dir] = st
			linkStatics.Unlock()
		}
	}
	info.Driver, info.LinkType = st.driver, st.linkType
	info.MTU, _ = strconv.Atoi(readTrimmed(filepath.Join(dir, "mtu")))
	// Virtual links report a nominal speed and duplex.
	if info.LinkType == "ethernet" || info.LinkType == "wireless" {
		if speed, err := strconv.Atoi(readTrimmed(filepath.Join(dir, "speed"))); err == nil && speed > 0 {
			info.Speed = speed
		}
		if duplex := readTrimmed(filepath.Join(dir, "duplex")); duplex == "full" || duplex == "half" {
			info.Duplex = duplex
		}
	}
	return index
}

func readLinkStatic(dir string) (st linkStatic) {
	if driver, err := os.Readlink(filepath.Join(dir, "device", "driver")); err == nil {
		st.driver = filepath.Base(driver)
	}
	arpType := readTrimmed(filepath.Join(dir, "type"))
	switch {
	case arpType == "772":
		st.linkType = "loopback"
	case exists(filepath.Join(dir, "wireless")) || exists(filepath.Join(dir, "phy80211")):
		st.linkType = "wireless"
	case tunnelARPTypes[arpType] || exists(filepath.Join(dir, "tun_flags")):
		st.linkType = "tunnel"
	case arpType == "1" && exists(filepath.Join(dir, "device")):
		st.linkType = "ethernet"
	case arpType != "":
		st.linkType = "other"
	}
	return st
}
