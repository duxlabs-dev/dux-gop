package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type SensorsInput struct {
	GPUPciIds []string `query:"gpu_pci_ids" example:"1002:744c" doc:"PCI IDs or bus addresses of GPUs whose sensors to read; other GPUs are not touched"`
}

type SensorsResponse struct {
	Body *models.SensorsInfo
}

// GET /sensors
func (self *HandlerGroup) Sensors(ctx context.Context, input *SensorsInput) (*SensorsResponse, error) {
	sensorsInfo, err := self.srv.Gops.GetSensors(input.GPUPciIds)
	if err != nil {
		log.Error("Error getting sensors")
		return nil, huma.Error500InternalServerError("Unable to retrieve sensors")
	}

	return &SensorsResponse{Body: sensorsInfo}, nil
}
