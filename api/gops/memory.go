package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/httpapi"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type MemoryResponse struct {
	Body struct {
		Data *models.MemoryInfo `json:"data"`
	}
}

// GET /memory
func (self *HandlerGroup) Memory(ctx context.Context, _ *httpapi.EmptyInput) (*MemoryResponse, error) {

	memoryInfo, err := self.srv.Gops.GetMemoryInfo()
	if err != nil {
		log.Error("Error getting memory info")
		return nil, huma.Error500InternalServerError("Unable to retrieve memory info")
	}

	resp := &MemoryResponse{}
	resp.Body.Data = memoryInfo
	return resp, nil
}

type MemoryModulesResponse struct {
	Body *models.MemoryModulesInfo
}

// GET /memory/modules
func (self *HandlerGroup) MemoryModules(ctx context.Context, _ *httpapi.EmptyInput) (*MemoryModulesResponse, error) {
	modules, err := self.srv.Gops.GetMemoryModules()
	if err != nil {
		log.Error("Error getting memory modules")
		return nil, huma.Error500InternalServerError("Unable to retrieve memory modules")
	}

	return &MemoryModulesResponse{Body: modules}, nil
}
