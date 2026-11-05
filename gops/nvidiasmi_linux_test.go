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

func TestParseNvidiaPmon(t *testing.T) {
	// nvidia-smi 615.78.08, RTX 5070, `pmon --count 1 --select u`.
	current := `# gpu         pid   type     sm    mem    enc    dec    jpg    ofa    command 
# Idx           #    C/G      %      %      %      %      %      %    name 
    0       2155     G      6      1      -      -      -      -    Hyprland       
    0       2395     G      -      -      -      -      -      -    Xwayland       
    0       2716   C+G      2      0      -     35      -      -    qs             
    1       2716     C     71      9     12      -     40      -    qs             
`
	assert.Equal(t, map[int32]nvidiaUsage{
		2155: {SM: 6},
		2395: {},
		2716: {SM: 71, Video: 40},
	}, parseNvidiaPmon(current), "highest per process across GPUs, '-' is 0")

	// Older drivers print no jpg and ofa columns.
	older := `# gpu        pid  type    sm   mem   enc   dec   command
# Idx          #   C/G     %     %     %     %   name
    0       4021     C    55    20     0     8   python3
`
	assert.Equal(t, map[int32]nvidiaUsage{4021: {SM: 55, Video: 8}}, parseNvidiaPmon(older))

	idle := "# gpu        pid  type    sm   mem   enc   dec   command\n# Idx          #   C/G     %     %     %     %   name\n    0          -     -     -     -     -     -   -\n"
	assert.Equal(t, map[int32]nvidiaUsage{}, parseNvidiaPmon(idle), "no process is an empty, known sample")

	assert.Nil(t, parseNvidiaPmon("Failed to process command line.\n"), "no table is unknown")
	assert.Nil(t, parseNvidiaPmon(""))
}
