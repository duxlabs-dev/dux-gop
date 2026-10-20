package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type SystemHardwareResponse struct {
	Body *models.SystemHardware
}

type GPUResponse struct {
	Body *models.GPUInfo
}

type GPUTempInput struct {
	PciId string `query:"pciId" required:"true" example:"10de:2684"`
}

type GPUTempResponse struct {
	Body *models.GPUTempInfo
}

// GET /hardware
func (self *HandlerGroup) SystemHardware(ctx context.Context, input *struct{}) (*SystemHardwareResponse, error) {
	systemInfo, err := self.srv.Gops.GetSystemHardware()
	if err != nil {
		log.Error("Error getting system hardware info")
		return nil, huma.Error500InternalServerError("Unable to retrieve system hardware info")
	}

	return &SystemHardwareResponse{Body: systemInfo}, nil
}

// GET /gpu
func (self *HandlerGroup) GPU(ctx context.Context, input *struct{}) (*GPUResponse, error) {
	gpuInfo, err := self.srv.Gops.GetGPUInfo()
	if err != nil {
		log.Error("Error getting GPU info")
		return nil, huma.Error500InternalServerError("Unable to retrieve GPU info")
	}

	return &GPUResponse{Body: gpuInfo}, nil
}

// GET /gpu/temp
func (self *HandlerGroup) GPUTemp(ctx context.Context, input *GPUTempInput) (*GPUTempResponse, error) {
	gpuTempInfo, err := self.srv.Gops.GetGPUTemp(input.PciId)
	if err != nil {
		log.Error("Error getting GPU temperature")
		return nil, huma.Error400BadRequest(err.Error())
	}

	return &GPUTempResponse{Body: gpuTempInfo}, nil
}
