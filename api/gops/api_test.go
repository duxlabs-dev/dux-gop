package gops_handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	gops_handler "github.com/duxlabs-dev/dux-gop/api/gops"
	"github.com/duxlabs-dev/dux-gop/api/server"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/duxlabs-dev/dux-gop/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCollector answers the handlers under test; any other call reaches the
// nil embedded Collector and panics.
type fakeCollector struct {
	server.Collector
	meta    func(modules []string, params gops.MetaParams) (*models.MetaInfo, error)
	procs   func(opts gops.ProcessOptions) (*models.ProcessListResponse, error)
	detail  func(pid int32) (*models.ProcessDetail, error)
	modules func() (*models.MemoryModulesInfo, error)
}

func (self *fakeCollector) GetMeta(_ context.Context, modules []string, params gops.MetaParams) (*models.MetaInfo, error) {
	return self.meta(modules, params)
}

func (self *fakeCollector) GetProcessesWithOptions(opts gops.ProcessOptions) (*models.ProcessListResponse, error) {
	return self.procs(opts)
}

func (self *fakeCollector) GetProcessDetail(pid int32) (*models.ProcessDetail, error) {
	return self.detail(pid)
}

func (self *fakeCollector) GetMemoryModules() (*models.MemoryModulesInfo, error) {
	return self.modules()
}

func (self *fakeCollector) GetSensors([]string) (*models.SensorsInfo, error) {
	return &models.SensorsInfo{Sensors: []models.Sensor{}}, nil
}

func getJSON(t *testing.T, c server.Collector, path string, want int) map[string]any {
	t.Helper()
	_, api := humatest.New(t)
	gops_handler.RegisterHandlers(&server.Server{Gops: c}, huma.NewGroup(api, "/gops"))
	resp := api.Get(path)
	require.Equal(t, want, resp.Code, "GET %s: %s", path, resp.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	return body
}

func TestMetaPassesModulesAndParams(t *testing.T) {
	var gotModules []string
	var gotParams gops.MetaParams
	c := &fakeCollector{meta: func(modules []string, params gops.MetaParams) (*models.MetaInfo, error) {
		gotModules, gotParams = modules, params
		return &models.MetaInfo{Cursor: "next"}, nil
	}}
	q := url.Values{}
	q.Set("modules", "processes, process-io")
	q.Set("proc_cursor", "prev")
	q.Set("gpu_pci_ids", "10de:2684")
	q.Set("disable_proc_cpu", "true")
	q.Set("limit", "5")
	q.Set("net_rate_all_interfaces", "true")

	body := getJSON(t, c, "/gops/meta?"+q.Encode(), http.StatusOK)
	assert.Equal(t, "next", body["cursor"])
	assert.Equal(t, []string{"processes", "process-io"}, gotModules, "a comma list is split and trimmed")
	assert.Equal(t, gops.MetaParams{
		SortBy: gops.SortByCPU, ProcLimit: 5, MergeChildren: true,
		GPUPciIds: []string{"10de:2684"}, ProcCursor: "prev", NetRateAllInterfaces: true,
	}, gotParams)
}

func TestMetaUnknownModuleIsBadRequest(t *testing.T) {
	c := &fakeCollector{meta: func([]string, gops.MetaParams) (*models.MetaInfo, error) {
		return nil, errors.New("unknown module: bogus")
	}}
	body := getJSON(t, c, "/gops/meta?modules=processes,bogus", http.StatusBadRequest)
	assert.Equal(t, "unknown module: bogus", body["detail"])
}

func TestProcessesPassesOptions(t *testing.T) {
	var got gops.ProcessOptions
	c := &fakeCollector{procs: func(opts gops.ProcessOptions) (*models.ProcessListResponse, error) {
		got = opts
		return &models.ProcessListResponse{Processes: []*models.ProcessInfo{{PID: 7}}, Cursor: "c2"}, nil
	}}
	body := getJSON(t, c, "/gops/processes?gpu=true&io=true&net=true&cursor=c1&disable_proc_cpu=true&limit=3&merge_children=false", http.StatusOK)
	assert.Equal(t, gops.ProcessOptions{SortBy: gops.SortByCPU, Limit: 3, Cursor: "c1", GPU: true, IO: true, Net: true}, got)
	assert.Equal(t, "c2", body["cursor"])
	assert.Len(t, body["data"], 1)
}

func TestProcessDetailStatuses(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want int
	}{
		"found":      {nil, http.StatusOK},
		"not found":  {fmt.Errorf("process 42: %w", gops.ErrProcessNotFound), http.StatusNotFound},
		"unreadable": {errors.New("boom"), http.StatusInternalServerError},
	} {
		c := &fakeCollector{detail: func(pid int32) (*models.ProcessDetail, error) {
			if tc.err != nil {
				return nil, tc.err
			}
			return &models.ProcessDetail{PID: pid}, nil
		}}
		body := getJSON(t, c, "/gops/process/42", tc.want)
		if tc.err == nil {
			assert.Equal(t, float64(42), body["pid"], name)
		}
	}
}

func TestMemoryModulesAndSensors(t *testing.T) {
	c := &fakeCollector{modules: func() (*models.MemoryModulesInfo, error) {
		return &models.MemoryModulesInfo{Slots: 4, Modules: []models.MemoryModule{}}, nil
	}}
	assert.Contains(t, getJSON(t, c, "/gops/memory/modules", http.StatusOK), "modules")
	assert.Contains(t, getJSON(t, c, "/gops/sensors", http.StatusOK), "sensors")

	c.modules = func() (*models.MemoryModulesInfo, error) { return nil, errors.New("memory modules: no source") }
	body := getJSON(t, c, "/gops/memory/modules", http.StatusInternalServerError)
	assert.Equal(t, "Unable to retrieve memory modules", body["detail"])
}
