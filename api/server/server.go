package server

import (
	"github.com/duxlabs-dev/dux-gop/config"
	"github.com/duxlabs-dev/dux-gop/gops"
)

type Server struct {
	Cfg  *config.Config
	Gops *gops.GopsUtil
}
