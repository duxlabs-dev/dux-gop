package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/httpapi"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type NetworkResponse struct {
	Body struct {
		Data []*models.NetworkInfo `json:"data"`
	}
}

// GET /network
func (self *HandlerGroup) Network(ctx context.Context, _ *httpapi.EmptyInput) (*NetworkResponse, error) {

	networkInfo, err := self.srv.Gops.GetNetworkInfo()
	if err != nil {
		log.Error("Error getting Network info")
		return nil, huma.Error500InternalServerError("Unable to retrieve Network info")
	}

	resp := &NetworkResponse{}
	resp.Body.Data = networkInfo
	return resp, nil
}
