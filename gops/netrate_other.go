//go:build !linux

package gops

import "github.com/duxlabs-dev/dux-gop/models"

func fillLinkInfos(_ []*models.NetworkRateInfo) {}
