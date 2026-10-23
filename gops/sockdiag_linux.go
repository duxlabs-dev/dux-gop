//go:build linux

package gops

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

type sockDiagEntry struct {
	Inode      uint32
	Cookie     uint64
	Protocol   uint8
	Remote     netip.AddrPort
	RxBytes    uint64
	TxBytes    uint64
	HasTCPInfo bool
}

// Layouts of inet_diag_req_v2 and inet_diag_msg (linux/inet_diag.h).
const (
	inetDiagReqV2Len = 56
	inetDiagMsgLen   = 72
	inetDiagInfo     = 2 // INET_DIAG_INFO attribute type
	// SKNLGRP_INET_TCP_DESTROY and SKNLGRP_INET6_TCP_DESTROY from linux/sock_diag.h.
	sknlgrpInetTCPDestroy  = 1
	sknlgrpInet6TCPDestroy = 3
	tcpListen              = 10
	tcpEstablished         = 1
	allTCPStates           = 0xfff
)

var (
	tcpInfoBytesAckedOff    = int(unsafe.Offsetof(unix.TCPInfo{}.Bytes_acked))
	tcpInfoBytesReceivedOff = int(unsafe.Offsetof(unix.TCPInfo{}.Bytes_received))
)

const sockDiagRecvTimeout = time.Second

// openSockDiag opens a sock_diag socket whose reads time out, so a stuck dump can't hang a request.
func openSockDiag() (int, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return -1, fmt.Errorf("sock_diag socket: %w", err)
	}
	tv := unix.NsecToTimeval(sockDiagRecvTimeout.Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("sock_diag timeout: %w", err)
	}
	return fd, nil
}

// udpUnsupported reports the ENOENT a kernel without udp_diag answers; TCP results are still good.
func udpUnsupported(protocol uint8, err error) bool {
	return protocol == unix.IPPROTO_UDP && errors.Is(err, unix.ENOENT)
}

// dumpSockets lists TCP (all states but LISTEN) and connected UDP sockets. Sockets without
// an inode are kept only with TCP counters.
func dumpSockets() ([]sockDiagEntry, error) {
	fd, err := openSockDiag()
	if err != nil {
		return nil, err
	}
	defer func() { _ = unix.Close(fd) }()

	requests := []struct {
		protocol uint8
		states   uint32
		ext      uint8
	}{
		{unix.IPPROTO_TCP, allTCPStates &^ (1 << tcpListen), 1 << (inetDiagInfo - 1)},
		{unix.IPPROTO_UDP, 1 << tcpEstablished, 0},
	}

	var entries []sockDiagEntry
	buf := make([]byte, 64*1024)
	seq := uint32(0)
	for _, family := range []uint8{unix.AF_INET, unix.AF_INET6} {
		for _, r := range requests {
			seq++
			req := encodeSockDiagRequest(seq, family, r.protocol, r.states, r.ext)
			if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
				return nil, fmt.Errorf("sock_diag send: %w", err)
			}
			for done := false; !done; {
				n, _, err := unix.Recvfrom(fd, buf, 0)
				if err == unix.EINTR { // SO_RCVTIMEO receives are not restarted after a signal
					continue
				}
				if err != nil {
					return nil, fmt.Errorf("sock_diag recv: %w", err)
				}
				var part []sockDiagEntry
				part, done, err = parseSockDiagResponse(buf[:n], seq, r.protocol)
				if udpUnsupported(r.protocol, err) {
					break
				}
				if err != nil {
					return nil, err
				}
				entries = append(entries, part...)
			}
		}
	}
	return entries, nil
}

func encodeSockDiagRequest(seq uint32, family, protocol uint8, states uint32, ext uint8) []byte {
	b := make([]byte, unix.SizeofNlMsghdr+inetDiagReqV2Len)
	ne := binary.NativeEndian
	ne.PutUint32(b[0:], uint32(len(b)))
	ne.PutUint16(b[4:], unix.SOCK_DIAG_BY_FAMILY)
	ne.PutUint16(b[6:], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	ne.PutUint32(b[8:], seq)
	req := b[unix.SizeofNlMsghdr:]
	req[0] = family
	req[1] = protocol
	req[2] = ext
	ne.PutUint32(req[4:], states)
	return b
}

// parseSockDiagResponse decodes one dump datagram (seq 0 for destroy broadcasts),
// ignoring other sequence numbers; done reports NLMSG_DONE.
func parseSockDiagResponse(b []byte, seq uint32, protocol uint8) (entries []sockDiagEntry, done bool, err error) {
	msgs, err := syscall.ParseNetlinkMessage(b)
	if err != nil {
		return nil, false, fmt.Errorf("sock_diag parse: %w", err)
	}
	for _, m := range msgs {
		if m.Header.Seq != seq {
			continue
		}
		switch m.Header.Type {
		case unix.NLMSG_DONE:
			if len(m.Data) >= 4 {
				if code := int32(binary.NativeEndian.Uint32(m.Data)); code < 0 {
					return entries, true, fmt.Errorf("sock_diag dump: %w", unix.Errno(-code))
				}
			}
			return entries, true, nil
		case unix.NLMSG_ERROR:
			if len(m.Data) < 4 {
				return entries, true, fmt.Errorf("sock_diag: truncated error message")
			}
			code := int32(binary.NativeEndian.Uint32(m.Data))
			if code == 0 {
				continue
			}
			return entries, true, fmt.Errorf("sock_diag: %w", unix.Errno(-code))
		case unix.SOCK_DIAG_BY_FAMILY:
			if e, ok := parseInetDiagMsg(m.Data, protocol); ok && (e.Inode != 0 || e.HasTCPInfo) {
				entries = append(entries, e)
			}
		}
	}
	return entries, false, nil
}

func parseInetDiagMsg(b []byte, protocol uint8) (sockDiagEntry, bool) {
	if len(b) < inetDiagMsgLen {
		return sockDiagEntry{}, false
	}
	ne := binary.NativeEndian
	e := sockDiagEntry{
		Protocol: protocol,
		Inode:    ne.Uint32(b[68:]),
		// idiag_cookie is two u32s, low half first.
		Cookie: uint64(ne.Uint32(b[44:])) | uint64(ne.Uint32(b[48:]))<<32,
	}
	dport := binary.BigEndian.Uint16(b[6:])
	switch b[0] {
	case unix.AF_INET:
		e.Remote = netip.AddrPortFrom(netip.AddrFrom4([4]byte(b[24:28])), dport)
	case unix.AF_INET6:
		e.Remote = netip.AddrPortFrom(netip.AddrFrom16([16]byte(b[24:40])), dport)
	default:
		return sockDiagEntry{}, false
	}

	for attrs := b[inetDiagMsgLen:]; len(attrs) >= unix.SizeofRtAttr; {
		attrLen := int(binary.NativeEndian.Uint16(attrs))
		if attrLen < unix.SizeofRtAttr || attrLen > len(attrs) {
			break
		}
		if binary.NativeEndian.Uint16(attrs[2:]) == inetDiagInfo && protocol == unix.IPPROTO_TCP {
			info := attrs[unix.SizeofRtAttr:attrLen]
			if len(info) >= tcpInfoBytesReceivedOff+8 {
				e.TxBytes = binary.NativeEndian.Uint64(info[tcpInfoBytesAckedOff:])
				e.RxBytes = binary.NativeEndian.Uint64(info[tcpInfoBytesReceivedOff:])
				e.HasTCPInfo = true
			}
		}
		next := (attrLen + unix.RTA_ALIGNTO - 1) &^ (unix.RTA_ALIGNTO - 1)
		if next >= len(attrs) {
			break
		}
		attrs = attrs[next:]
	}
	return e, true
}

// listenSockDestroys emits every TCP socket destroyed in our network namespace, with
// its final counters, until stop is called.
func listenSockDestroys(emit func(sockDiagEntry)) (stop func(), err error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return nil, fmt.Errorf("sock_diag socket: %w", err)
	}
	// Broadcasts skip an unbound socket: its port id is still 0.
	err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK})
	for _, group := range []int{sknlgrpInetTCPDestroy, sknlgrpInet6TCPDestroy} {
		if err == nil {
			err = unix.SetsockoptInt(fd, unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, group)
		}
	}
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("sock_diag destroy groups: %w", err)
	}
	f := os.NewFile(uintptr(fd), "sock_diag")
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := f.Read(buf)
			if errors.Is(err, unix.ENOBUFS) { // events were dropped; later ones still arrive
				continue
			}
			if err != nil {
				return
			}
			entries, _, _ := parseSockDiagResponse(buf[:n], 0, unix.IPPROTO_TCP)
			for _, e := range entries {
				emit(e)
			}
		}
	}()
	return func() { _ = f.Close() }, nil
}
