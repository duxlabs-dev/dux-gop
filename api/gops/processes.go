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
	EnableProcGPU  bool            `query:"enable_proc_gpu" default:"false" doc:"Sample per-process GPU usage (implied by sort_by=gpu)"`
	GPUPciIds      []string        `query:"gpu_pci_ids" example:"10de:2684,0000:03:00.0" doc:"PCI IDs or bus addresses of GPUs to sample per-process usage for"`
	Cursor         string          `query:"cursor" required:"false"`
	MergeChildren  bool            `query:"merge_children" default:"true"`
}

type ProcessResponse struct {
	Body struct {
		Data   []*models.ProcessInfo `json:"data"`
		Cursor string                `json:"cursor,omitempty"`
	}
}

// GET /processes
func (self *HandlerGroup) Processes(ctx context.Context, input *ProcessInput) (*ProcessResponse, error) {
	result, err := self.srv.Gops.GetProcessesWithOptions(gops.ProcessOptions{
		SortBy:        input.SortBy,
		Limit:         input.Limit,
		EnableCPU:     !input.DisableProcCPU,
		EnableGPU:     input.EnableProcGPU,
		GPUPciIds:     input.GPUPciIds,
		Cursor:        input.Cursor,
		MergeChildren: input.MergeChildren,
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
