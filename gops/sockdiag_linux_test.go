//go:build linux

package gops

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// Captured SOCK_DIAG_BY_FAMILY reply (seq 7) for the client end of a [::1]
// TCP connection that sent 5 bytes; tcp_info counts the SYN in bytes_acked.
const capturedTCPv6Msg = "" +
	"900100001400020007000000759e06000a0102008deca9f90000000000000000" +
	"0000000000000001000000000000000000000000000000010000000002000000" +
	"00000000983a00000000000000000000e8030000f7de1a000500080000000000" +
	"0c001500a63800000000000006001600120000001c010200010000000007aa01" +
	"2811030000000000008000001802000000000000000000000000000000000000" +
	"000000000000000000000000000000000000000000000100c4ff000028000000" +
	"16000000ffffff7f0a000000b8ff00000300000000000000c4ff000000000000" +
	"0d2f85cd03000000ffffffffffffffff06000000000000000000000000000000" +
	"0300000002000000000000000d0000000000000001000000d8893d9600000000" +
	"0000000000000000000000000000000000000000000000000200000000000000" +
	"0500000000000000000000000000000000000000000000000000000000000100" +
	"0000010000000000000000000000000000000000000000000000000000000000" +
	"00000000000000000000000000000000"

func nlMsg(typ uint16, seq uint32, payload []byte) []byte {
	b := make([]byte, unix.SizeofNlMsghdr, unix.SizeofNlMsghdr+len(payload)+3)
	binary.NativeEndian.PutUint32(b[0:], uint32(unix.SizeofNlMsghdr+len(payload)))
	binary.NativeEndian.PutUint16(b[4:], typ)
	binary.NativeEndian.PutUint32(b[8:], seq)
	b = append(b, payload...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func nlInt(v int32) []byte {
	return binary.NativeEndian.AppendUint32(nil, uint32(v))
}

func inetDiagV4(state uint8, local, remote string, inode uint32, attrs ...[]byte) []byte {
	b := make([]byte, inetDiagMsgLen)
	b[0] = unix.AF_INET
	b[1] = state
	l, r := netip.MustParseAddrPort(local), netip.MustParseAddrPort(remote)
	binary.BigEndian.PutUint16(b[4:], l.Port())
	binary.BigEndian.PutUint16(b[6:], r.Port())
	copy(b[8:], l.Addr().AsSlice())
	copy(b[24:], r.Addr().AsSlice())
	binary.NativeEndian.PutUint32(b[68:], inode)
	for _, a := range attrs {
		b = append(b, a...)
	}
	return b
}

func rtAttr(typ uint16, data []byte) []byte {
	b := make([]byte, unix.SizeofRtAttr, unix.SizeofRtAttr+len(data)+3)
	binary.NativeEndian.PutUint16(b[0:], uint16(unix.SizeofRtAttr+len(data)))
	binary.NativeEndian.PutUint16(b[2:], typ)
	b = append(b, data...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func TestParseSockDiagCapturedTCPv6(t *testing.T) {
	msg, err := hex.DecodeString(capturedTCPv6Msg)
	require.NoError(t, err)
	want := sockDiagEntry{
		Inode:      0x1adef7,
		Cookie:     2,
		Protocol:   unix.IPPROTO_TCP,
		Remote:     netip.MustParseAddrPort("[::1]:43513"),
		TxBytes:    6,
		RxBytes:    0,
		HasTCPInfo: true,
	}

	entries, done, err := parseSockDiagResponse(append(msg, nlMsg(unix.NLMSG_DONE, 7, nlInt(0))...), 7, unix.IPPROTO_TCP)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, []sockDiagEntry{want}, entries)
}

func TestParseSockDiagResponseEdgeCases(t *testing.T) {
	sock := inetDiagV4(1, "192.0.2.10:40000", "198.51.100.7:443", 42)

	t.Run("skips inode 0 without counters, short messages and other sequences", func(t *testing.T) {
		buf := append(nlMsg(unix.SOCK_DIAG_BY_FAMILY, 3, inetDiagV4(6, "192.0.2.10:1", "198.51.100.7:2", 0)),
			nlMsg(unix.SOCK_DIAG_BY_FAMILY, 3, sock[:40])...)
		buf = append(buf, nlMsg(unix.SOCK_DIAG_BY_FAMILY, 9, sock)...)
		buf = append(buf, nlMsg(unix.SOCK_DIAG_BY_FAMILY, 3, sock)...)
		entries, done, err := parseSockDiagResponse(buf, 3, unix.IPPROTO_UDP)
		require.NoError(t, err)
		assert.False(t, done)
		require.Len(t, entries, 1)
		assert.Equal(t, uint32(42), entries[0].Inode)
	})

	t.Run("NLMSG_ERROR", func(t *testing.T) {
		_, done, err := parseSockDiagResponse(nlMsg(unix.NLMSG_ERROR, 3, nlInt(-int32(unix.EPERM))), 3, unix.IPPROTO_TCP)
		assert.True(t, done)
		assert.True(t, errors.Is(err, unix.EPERM))
	})

	t.Run("truncated netlink header", func(t *testing.T) {
		buf := nlMsg(unix.SOCK_DIAG_BY_FAMILY, 3, sock)
		_, _, err := parseSockDiagResponse(buf[:len(buf)-8], 3, unix.IPPROTO_TCP)
		assert.Error(t, err)
	})
}
