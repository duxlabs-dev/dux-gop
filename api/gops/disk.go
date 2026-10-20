package gops_handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	"github.com/duxlabs-dev/dux-go/httpapi"
	"github.com/duxlabs-dev/dux-go/log"
	"github.com/duxlabs-dev/dux-gop/models"
)

type DiskResponse struct {
	Body struct {
		Data []*models.DiskInfo `json:"data"`
	}
}

// GET /disk
func (self *HandlerGroup) Disk(ctx context.Context, _ *httpapi.EmptyInput) (*DiskResponse, error) {
	diskInfo, err := self.srv.Gops.GetDiskInfo()
	if err != nil {
		log.Error("Error getting Disk info")
		return nil, huma.Error500InternalServerError("Unable to retrieve Disk info")
	}

	resp := &DiskResponse{}
	resp.Body.Data = diskInfo
	return resp, nil
}

// GET /disk/mounts
type DiskMountsResponse struct {
	Body struct {
		Data []*models.DiskMountInfo `json:"data"`
	}
}

func (self *HandlerGroup) DiskMounts(ctx context.Context, _ *httpapi.EmptyInput) (*DiskMountsResponse, error) {
	diskMountsInfo, err := self.srv.Gops.GetDiskMounts()
	if err != nil {
		log.Error("Error getting Disk Mounts info")
		return nil, huma.Error500InternalServerError("Unable to retrieve Disk Mounts info")
	}

	resp := &DiskMountsResponse{}
	resp.Body.Data = diskMountsInfo
	return resp, nil
}
