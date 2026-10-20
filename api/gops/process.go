package gops_handler

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
)

type ProcessDetailInput struct {
	PID int32 `path:"pid" minimum:"1" example:"1234"`
}

type ProcessDetailResponse struct {
	Body *models.ProcessDetail
}

// GET /process/{pid}
func (self *HandlerGroup) ProcessDetail(ctx context.Context, input *ProcessDetailInput) (*ProcessDetailResponse, error) {
	detail, err := self.srv.Gops.GetProcessDetail(input.PID)
	if errors.Is(err, gops.ErrProcessNotFound) {
		return nil, huma.Error404NotFound(err.Error())
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("Unable to retrieve process detail")
	}
	return &ProcessDetailResponse{Body: detail}, nil
}
