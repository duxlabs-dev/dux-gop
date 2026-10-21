package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
)

type ProcessInput struct {
	SortBy         gops.ProcSortBy `query:"sort_by" required:"true" default:"cpu"`
	Limit          int             `query:"limit"`
	DisableProcCPU bool            `query:"disable_proc_cpu" default:"false"`
	Cursor         string          `query:"cursor" required:"false"`
	MergeChildren  bool            `query:"merge_children" default:"true"`
	GPU            bool            `query:"gpu" default:"false"`
	IO             bool            `query:"io" default:"false"`
	Net            bool            `query:"net" default:"false"`
	GPUPciIds      []string        `query:"gpu_pci_ids" example:"10de:2684" doc:"With gpu, the GPUs to read; other GPUs are not touched"`
}

type ProcessResponse struct {
	Body struct {
		Data   []*models.ProcessInfo `json:"data"`
		Cursor string                `json:"cursor,omitempty"`
	}
}

// GET /processes
func (self *HandlerGroup) Processes(ctx context.Context, input *ProcessInput) (*ProcessResponse, error) {
	enableCPU := !input.DisableProcCPU

	result, err := self.srv.Gops.GetProcessesWithOptions(gops.ProcessOptions{
		SortBy: input.SortBy, Limit: input.Limit, EnableCPU: enableCPU, Cursor: input.Cursor,
		MergeChildren: input.MergeChildren, GPU: input.GPU, GPUPciIds: input.GPUPciIds, IO: input.IO, Net: input.Net,
	})
	if err != nil {
		log.Error("Error getting process info")
		return nil, huma.Error500InternalServerError("Unable to retrieve process info")
	}

	resp := &ProcessResponse{}
	resp.Body.Data = result.Processes
	resp.Body.Cursor = result.Cursor
	return resp, nil
}
