//go:build linux

package gops

import (
	"slices"
	"testing"

	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCgroupUnit(t *testing.T) {
	assert.Equal(t, "app-zen@a1b2.scope", cgroupUnit("0::/user.slice/user-1000.slice/user@1000.service/app.slice/app-zen@a1b2.scope\n"))
	assert.Equal(t, "pipewire.service", cgroupUnit("0::/user.slice/user-1000.slice/user@1000.service/session.slice/pipewire.service\n"))
	assert.Equal(t, "", cgroupUnit("0::/\n"))
	assert.Equal(t, "", cgroupUnit("1:name=systemd:/foo\n"))
}

func TestParseListenTable(t *testing.T) {
	tcp := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 111 1 0\n" +
		"   1: 0100007F:1F90 0100007F:D2A4 01 00000000:00000000 00:00000000 00000000  1000 0 222 1 0\n" +
		"   2: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000 0 333 1 0\n"
	got := parseListenTable(tcp, "tcp", false, map[uint32]bool{111: true, 222: true})
	assert.Equal(t, []models.ListenSocket{{Protocol: "tcp", Address: "127.0.0.1", Port: 8080}}, got)

	tcp6 := "hdr\n   0: 00000000000000000000000001000000:0016 00000000000000000000000000000000:0000 0A 0:0 0:0 0 0 0 5 1 0\n"
	got = parseListenTable(tcp6, "tcp", true, map[uint32]bool{5: true})
	assert.Equal(t, []models.ListenSocket{{Protocol: "tcp", Address: "::1", Port: 22}}, got)

	udp := "hdr\n   0: 00000000:0035 00000000:0000 07 0:0 0:0 0 0 0 7 2 0\n   1: 00000000:0044 0100007F:0035 01 0:0 0:0 0 0 0 8 2 0\n"
	got = parseListenTable(udp, "udp", false, map[uint32]bool{7: true, 8: true})
	assert.Equal(t, []models.ListenSocket{{Protocol: "udp", Address: "0.0.0.0", Port: 53}}, got)
}

func TestSandboxParsing(t *testing.T) {
	assert.Equal(t, "org.mozilla.firefox", flatpakAppID("[Application]\nname=org.mozilla.firefox\nruntime=x\n[Instance]\nname=other\n"))
	assert.Equal(t, "", flatpakAppID("[Instance]\nname=other\n"))

	assert.Equal(t, &models.Sandbox{Kind: "snap", ID: "firefox"},
		sandboxFromCgroup("0::/user.slice/user-1000.slice/user@1000.service/app.slice/snap.firefox.firefox-1234.scope\n"))
	assert.Equal(t, &models.Sandbox{Kind: "container", ID: "0123456789ab"},
		sandboxFromCgroup("0::/system.slice/docker-0123456789abcdef0123.scope\n"))
	assert.Equal(t, &models.Sandbox{Kind: "container", ID: "abc"},
		sandboxFromCgroup("0::/machine.slice/libpod-abc.scope\n"))
	assert.Nil(t, sandboxFromCgroup("0::/user.slice/app.slice/app-foo.scope\n"))
}

func TestParsePrivileges(t *testing.T) {
	// cap_net_bind_service (10) and cap_net_raw (13)
	user := "Uid:\t1000\t1000\t1000\t1000\nCapEff:\t0000000000002400\n"
	p := parsePrivileges(user)
	require.NotNil(t, p)
	assert.False(t, p.Setuid)
	assert.True(t, slices.Equal([]string{"cap_net_bind_service", "cap_net_raw"}, p.Capabilities))

	p = parsePrivileges("Uid:\t1000\t0\t0\t0\nCapEff:\t000001ffffffffff\n")
	require.NotNil(t, p)
	assert.True(t, p.Setuid)
	assert.Empty(t, p.Capabilities)

	assert.Nil(t, parsePrivileges("Uid:\t1000\t1000\t1000\t1000\nCapEff:\t0000000000000000\n"))
	assert.Nil(t, parsePrivileges("Uid:\t0\t0\t0\t0\nCapEff:\t000001ffffffffff\n"))
}
