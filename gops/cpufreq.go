package gops

import "github.com/duxlabs-dev/dux-gop/models"

type cpuStatic struct {
	minMHz, maxMHz float64
	physicalCores  int
	baseMHz        float64
	sockets        int
	cache          *models.CPUCache
}

type cpuDynamic struct {
	coreMHz  []float64
	governor string
	epp      string
}
