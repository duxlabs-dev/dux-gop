package gops_handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	gops_handler "github.com/duxlabs-dev/dux-gop/api/gops"
	"github.com/duxlabs-dev/dux-gop/api/server"
	"github.com/duxlabs-dev/dux-gop/gops"
)

func newAPI(t *testing.T) humatest.TestAPI {
	_, api := humatest.New(t)
	gops_handler.RegisterHandlers(&server.Server{Gops: gops.NewGopsUtil()}, huma.NewGroup(api, "/gops"))
	return api
}

func TestNewEndpoints(t *testing.T) {
	api := newAPI(t)
	cases := []struct{ path, key string }{
		{"/gops/sensors", "sensors"},
		{"/gops/memory/modules", "modules"},
	}
	for _, c := range cases {
		resp := api.Get(c.path)
		if resp.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", c.path, resp.Code, resp.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if _, ok := body[c.key]; !ok {
			t.Errorf("%s: missing key %q in %s", c.path, c.key, resp.Body.String())
		}
	}
}

func TestNetRateAllInterfaces(t *testing.T) {
	if resp := newAPI(t).Get("/gops/net-rate?all_interfaces=true"); resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
}

func TestMetaNewParams(t *testing.T) {
	resp := newAPI(t).Get("/gops/meta?modules=sensors,gpu,memory-modules&gpu_cursor=&net_rate_all_interfaces=true")
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
}

func TestOpenAPIListsNewOperations(t *testing.T) {
	api := newAPI(t)
	for _, p := range []string{"/gops/sensors", "/gops/memory/modules", "/gops/process/{pid}"} {
		if api.OpenAPI().Paths[p] == nil {
			t.Errorf("%s missing from OpenAPI", p)
		}
	}
}

func TestProcessesIOCursorJourney(t *testing.T) {
	if _, err := os.ReadFile("/proc/self/io"); err != nil {
		t.Skipf("/proc/self/io unreadable: %v", err)
	}
	api := newAPI(t)
	var first struct {
		Cursor string `json:"cursor"`
	}
	resp := api.Get("/gops/processes?io=true&gpu=true&limit=0")
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if first.Cursor == "" {
		t.Fatal("first response returned no cursor")
	}
	resp = api.Get("/gops/processes?io=true&limit=0&sort_by=disk&cursor=" + url.QueryEscape(first.Cursor))
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, p := range out.Data {
		if int(p["pid"].(float64)) == os.Getpid() {
			if _, ok := p["diskRead"]; !ok {
				t.Fatal("diskRead missing on own pid row")
			}
			return
		}
	}
	t.Fatal("own pid not in process list")
}

func TestProcessesDefaultShape(t *testing.T) {
	resp := newAPI(t).Get("/gops/processes?limit=0")
	var out struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &out); err != nil || resp.Code != http.StatusOK {
		t.Fatalf("status %d: %v", resp.Code, err)
	}
	for _, p := range out.Data {
		for _, k := range []string{"diskRead", "diskWrite", "gpu"} {
			if _, ok := p[k]; ok {
				t.Fatalf("unexpected %s in default output", k)
			}
		}
	}
}

func TestProcessDetailEndpoint(t *testing.T) {
	api := newAPI(t)
	resp := api.Get(fmt.Sprintf("/gops/process/%d", os.Getpid()))
	if resp.Code != http.StatusOK {
		t.Fatalf("status %d: %s", resp.Code, resp.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["pid"] != float64(os.Getpid()) {
		t.Errorf("pid = %v, want %d", body["pid"], os.Getpid())
	}
}

func TestProcessDetailEndpointNotFound(t *testing.T) {
	resp := newAPI(t).Get("/gops/process/2147483647")
	if resp.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404: %s", resp.Code, resp.Body.String())
	}
}
