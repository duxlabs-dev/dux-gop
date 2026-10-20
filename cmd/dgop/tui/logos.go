package tui

import (
	"strings"

	"github.com/duxlabs-dev/dux-gop/models"
)

type distroArt struct {
	matches []string
	color   string
	art     []string
}

var genericArt = distroArt{
	color: "#7D56F4",
	art: []string{
		"    ___",
		"   (.. \\",
		"   (<> |",
		"  //  \\ \\",
		" ( |  | /|",
		"_/\\ __)/_)",
		"\\/____\\/",
	},
}

var distroArts = []distroArt{
	{
		matches: []string{"arch"},
		color:   "#1793D1",
		art: []string{
			"      /\\",
			"     /  \\",
			"    /    \\",
			"   /      \\",
			"  /   ,,   \\",
			" /   |  |   \\",
			"/_-''    ''-_\\",
		},
	},
	{
		matches: []string{"ubuntu"},
		color:   "#E95420",
		art: []string{
			"         _",
			"     ---(_)",
			" _/  ---  \\",
			"(_) |   |",
			"  \\  --- _/",
			"     ---(_)",
		},
	},
	{
		matches: []string{"fedora"},
		color:   "#0B57A4",
		art: []string{
			"        ,'''''.",
			"       |   ,.  | ",
			"       |  |  '_'",
			"  ,....|  |..",
			".'  ,_;|   ..'",
			"|  |   |  |",
			"|  ',_,'  |",
			" '.     ,'",
			"   '''''",
		},
	},
	{
		matches: []string{"nix"},
		color:   "#5294e2",
		art: []string{
			"  ▗▄   ▗▄ ▄▖",
			" ▄▄🬸█▄▄▄🬸█▛ ▃",
			"   ▟▛    ▜▃▟🬕",
			"🬋🬋🬫█      █🬛🬋🬋",
			" 🬷▛🮃▙    ▟▛",
			" 🮃 ▟█🬴▀▀▀█🬴▀▀",
			"  ▝▀ ▀▘   ▀▘",
		},
	},
	{
		matches: []string{"debian"},
		color:   "#D70A53",
		art: []string{
			"  _____",
			" /  __ \\",
			"|  /    |",
			"|  \\___-",
			"-_",
			"  --_",
		},
	},
	{
		matches: []string{"mint"},
		color:   "#3EB489",
		art: []string{
			" __________",
			"|_          \\",
			"  | | _____ |",
			"  | | | | | |",
			"  | | | | | |",
			"  | \\____/ |",
			"  \\_________/",
		},
	},
	{
		matches: []string{"gentoo"},
		color:   "#54487A",
		art: []string{
			" *-----*",
			"(       \\",
			"\\    0   \\",
			" \\        )",
			" /      _/",
			"(     _-",
			"\\____-",
		},
	},
	{
		matches: []string{"cachyos"},
		color:   "#08A283",
		art: []string{
			"   /''''''''''''/",
			"  /''''''''''''/",
			" /''''''/",
			"/''''''/",
			"\\......\\",
			" \\......\\",
			"  \\............../",
			"   \\............./",
		},
	},
	{
		matches: []string{"elementary"},
		color:   "#64BAFF",
		art: []string{
			"  _______",
			" / ____  \\",
			"/  |  /  /\\",
			"|__\\ /  / |",
			"\\   /__/  /",
			" \\_______/",
		},
	},
	{
		matches: []string{"pop"},
		color:   "#48B9C7",
		art: []string{
			"______",
			"\\   * \\        *_",
			" \\ \\ \\ \\      / /",
			"  \\ \\_\\ \\    / /",
			"   \\  ___\\  /_/",
			"    \\ \\    _",
			"   __\\_\\__(_)_",
			"  (___________)`",
		},
	},
	{
		matches: []string{"suse"},
		color:   "#73BA25",
		art: []string{
			"  _______",
			"**|   ** \\",
			"     / .\\ \\",
			"     \\__/ |",
			"   _______|",
			"   \\_______",
			"__________/",
		},
	},
	{
		matches: []string{"endeavour"},
		color:   "#7F3FBF",
		art: []string{
			"          /o.",
			"        /sssso-",
			"      /ossssssso:",
			"    /ssssssssssso+",
			"  /ssssssssssssssso+",
			"//osssssssssssssso+-",
			" `+++++++++++++++-`",
		},
	},
	{
		matches: []string{"macos", "mac os"},
		color:   "#FFFFFF",
		art: []string{
			"       .:'",
			"    __ :'__",
			" .'`  `-'  ``.",
			":          .-'",
			":         :",
			" :         `-;",
			"  `.__.-.__.'",
		},
	},
}

func getDistroInfo(hardware *models.SystemHardware) ([]string, string) {
	if hardware == nil {
		return genericArt.art, genericArt.color
	}

	distro := strings.ToLower(hardware.Distro)
	for _, d := range distroArts {
		for _, match := range d.matches {
			if strings.Contains(distro, match) {
				return d.art, d.color
			}
		}
	}

	return genericArt.art, genericArt.color
}
