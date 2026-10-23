//go:build linux

package gops

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const nvidiaPIDsFixture = `
GPU 00000000:01:00.0
    Processes
        Process ID                        : 1234
            Type                          : G
            Name                          : /usr/lib/Xorg
            Used GPU Memory               : 150 MiB
        Process ID                        : 1234
            Type                          : C
            Name                          : /usr/lib/Xorg
            Used GPU Memory               : 50 MiB
        Process ID                        : 5678
            Type                          : C+G
            Name                          : game
            Used GPU Memory               : Not available
        Process ID                        : 9
            Type                          : G
            Name                          : other
            Used GPU Memory               : 1 GiB
GPU 00000000:02:00.0
    Processes                             : None
`

func TestParseNvidiaPIDs(t *testing.T) {
	got := parseNvidiaPIDs(nvidiaPIDsFixture)
	assert.Equal(t, map[int32]uint64{1234: 200 << 20, 5678: 0, 9: 1 << 30}, got)
	assert.Empty(t, parseNvidiaPIDs("GPU 0\n    Processes : None\n"))
}
