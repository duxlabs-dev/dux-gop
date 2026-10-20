package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type CpuInput struct {
	Cursor string `query:"cursor" required:"false"`
}

type CpuResponse struct {
	Body struct {
		Data *models.CPUInfo `json:"data"`
	}
}

// GET /cpu
func (self *HandlerGroup) Cpu(ctx context.Context, input *CpuInput) (*CpuResponse, error) {
	cpuInfo, err := self.srv.Gops.GetCPUInfoWithCursor(input.Cursor)
	if err != nil {
		log.Error("Error getting CPU info")
		return nil, huma.Error500InternalServerError("Unable to retrieve CPU info")
	}

	resp := &CpuResponse{}
	resp.Body.Data = cpuInfo
	return resp, nil
}
