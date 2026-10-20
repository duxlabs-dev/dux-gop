package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
)

type AllInput struct {
	SortBy         gops.ProcSortBy `query:"ps_sort_by" required:"true" default:"cpu"`
	Limit          int             `query:"ps_limit"`
	DisableProcCPU bool            `query:"disable_proc_cpu" default:"false"`
	MergeChildren  bool            `query:"merge_children" default:"true"`
}

type AllResponse struct {
	Body struct {
		Data *models.SystemMetrics `json:"data"`
	}
}

// GET /all
func (self *HandlerGroup) All(ctx context.Context, input *AllInput) (*AllResponse, error) {
	enableCPU := !input.DisableProcCPU
	all, err := self.srv.Gops.GetAllMetrics(input.SortBy, input.Limit, enableCPU, input.MergeChildren)
	if err != nil {
		log.Error("Error getting all metrics")
		return nil, huma.Error500InternalServerError("Unable to retrieve all metrics")
	}

	resp := &AllResponse{}
	resp.Body.Data = all
	return resp, nil
}
