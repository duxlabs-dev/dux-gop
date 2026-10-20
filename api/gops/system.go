package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/httpapi"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type SystemResponse struct {
	Body struct {
		Data *models.SystemInfo `json:"data"`
	}
}

// GET /system
func (self *HandlerGroup) System(ctx context.Context, _ *httpapi.EmptyInput) (*SystemResponse, error) {

	systemInfo, err := self.srv.Gops.GetSystemInfo()
	if err != nil {
		log.Error("Error getting system info")
		return nil, huma.Error500InternalServerError("Unable to retrieve system info")
	}

	resp := &SystemResponse{}
	resp.Body.Data = systemInfo
	return resp, nil
}
