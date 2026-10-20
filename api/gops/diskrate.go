package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type DiskRateInput struct {
	Cursor string `query:"cursor" doc:"Base64 cursor for rate calculation"`
}

type DiskRateResponse struct {
	Body *models.DiskRateResponse
}

// GET /disk-rate
func (self *HandlerGroup) DiskRate(ctx context.Context, input *DiskRateInput) (*DiskRateResponse, error) {
	diskRateInfo, err := self.srv.Gops.GetDiskRates(input.Cursor)
	if err != nil {
		log.Error("Error getting disk rates")
		return nil, huma.Error500InternalServerError("Unable to retrieve disk rates")
	}

	resp := &DiskRateResponse{}
	resp.Body = diskRateInfo
	return resp, nil
}
