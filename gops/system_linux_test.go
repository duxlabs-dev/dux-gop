//go:build linux

package gops

import (
	"testing"

	"github.com/duxlabs-dev/dux-gop/models"

	"github.com/stretchr/testify/assert"
)

func TestParseLoadavgTasks(t *testing.T) {
	assert.Equal(t, 3385, parseLoadavgTasks("0.37 0.85 1.53 1/3385 94690\n"))
	assert.Equal(t, 0, parseLoadavgTasks("0.37 0.85 1.53"))
	assert.Equal(t, 0, parseLoadavgTasks("0.37 0.85 1.53 garbage 94690"))
}

func TestParseFileNr(t *testing.T) {
	assert.Equal(t, 8472, parseFileNr("8472\t0\t9223372036854775807\n"))
	assert.Equal(t, 0, parseFileNr(""))
}

func TestParseProcSwaps(t *testing.T) {
	text := "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n" +
		"/dev/zram0                              partition\t32852988\t1234\t\t100\n" +
		"/swapfile                               file\t\t4194300\t\t0\t\t-2\n" +
		"/dev/nvme0n1p3                          partition\t8388604\t\t0\t\t-3\n"
	assert.Equal(t, []models.SwapDevice{
		{Name: "/dev/zram0", Kind: "zram", Size: 32852988, Used: 1234},
		{Name: "/swapfile", Kind: "file", Size: 4194300},
		{Name: "/dev/nvme0n1p3", Kind: "partition", Size: 8388604},
	}, parseProcSwaps(text))
	assert.Empty(t, parseProcSwaps("Filename\tType\tSize\tUsed\tPriority\n"))
	assert.Empty(t, parseProcSwaps(""))
}
