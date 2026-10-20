package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type NetRateInput struct {
	Cursor        string `query:"cursor" doc:"Base64 cursor for rate calculation"`
	AllInterfaces bool   `query:"all_interfaces" default:"false" doc:"Include all interfaces, not only the default set"`
}

type NetRateResponse struct {
	Body *models.NetworkRateResponse
}

// GET /net-rate
func (self *HandlerGroup) NetRate(ctx context.Context, input *NetRateInput) (*NetRateResponse, error) {
	netRateInfo, err := self.srv.Gops.GetNetworkRatesWithOptions(input.Cursor, input.AllInterfaces)
	if err != nil {
		log.Error("Error getting network rates")
		return nil, huma.Error500InternalServerError("Unable to retrieve network rates")
	}

	resp := &NetRateResponse{}
	resp.Body = netRateInfo
	return resp, nil
}
