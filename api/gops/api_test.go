package gops_handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	gops_handler "github.com/duxlabs-dev/dux-gop/api/gops"
	"github.com/duxlabs-dev/dux-gop/api/server"
	"github.com/duxlabs-dev/dux-gop/gops"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Cursor written before the process extras existed: pids 1 and 2 only.
const legacyProcCursor = "H4sIAAAAAAAC_6tWKlGyMjQ3QAAdpYLMFCWraEMdo1gdpeSCUhAbJG4EJIAiKUAN0QY6prG1APp9R3w8AAAA"

func newAPI(t *testing.T) humatest.TestAPI {
	_, api := humatest.New(t)
	gops_handler.RegisterHandlers(&server.Server{Gops: gops.NewGopsUtil()}, huma.NewGroup(api, "/gops"))
	return api
}

func getJSON(t *testing.T, path string, want int) map[string]any {
	t.Helper()
	resp := newAPI(t).Get(path)
	require.Equal(t, want, resp.Code, "GET %s: %s", path, resp.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	return body
}

func obj(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	require.True(t, ok, "want object, got %T", v)
	return m
}

func list(v any) []any {
	l, _ := v.([]any)
	return l
}

// rowFor returns the row of the test process in a list of process rows.
func rowFor(t *testing.T, rows []any) map[string]any {
	t.Helper()
	for _, r := range rows {
		if row := obj(t, r); row["pid"] == float64(os.Getpid()) {
			return row
		}
	}
	require.FailNow(t, "own pid not in process list")
	return nil
}

func TestEndpointsAndErrors(t *testing.T) {
	for path, key := range map[string]string{"/gops/sensors": "sensors", "/gops/memory/modules": "modules"} {
		assert.Contains(t, getJSON(t, path, http.StatusOK), key, path)
	}
	getJSON(t, "/gops/net-rate?all_interfaces=true", http.StatusOK)
	getJSON(t, "/gops/meta?modules=sensors,gpu,memory-modules&gpu_cursor=not-a-cursor&net_rate_all_interfaces=true", http.StatusOK)
	getJSON(t, "/gops/meta?modules=processes,bogus", http.StatusBadRequest)
	getJSON(t, "/gops/process/2147483647", http.StatusNotFound)

	pid := getJSON(t, fmt.Sprintf("/gops/process/%d", os.Getpid()), http.StatusOK)["pid"]
	assert.Equal(t, float64(os.Getpid()), pid)
}

func TestProcessJourney(t *testing.T) {
	if _, err := os.ReadFile("/proc/self/io"); err != nil {
		t.Skipf("/proc/self/io unreadable: %v", err)
	}
	const path = "/gops/meta?modules=processes,process-gpu,process-io&limit=0&disable_proc_cpu=true"

	first := getJSON(t, path, http.StatusOK)
	cursor, _ := first["cursor"].(string)
	require.NotEmpty(t, cursor)
	for _, k := range []string{"diskRead", "diskWrite", "gpu"} {
		assert.NotNil(t, rowFor(t, list(first["processes"]))[k], "first call lacks %q", k)
	}

	time.Sleep(100 * time.Millisecond)
	second := getJSON(t, path+"&proc_cursor="+url.QueryEscape(cursor), http.StatusOK)
	for _, k := range []string{"diskRead", "diskWrite", "gpu"} {
		assert.NotNil(t, rowFor(t, list(second["processes"]))[k], "second call lacks %q", k)
	}
}

func TestLegacyProcessCursor(t *testing.T) {
	body := getJSON(t, "/gops/meta?modules=processes&limit=0&proc_cursor="+legacyProcCursor, http.StatusOK)
	rows := list(body["processes"])
	require.NotEmpty(t, rows)
	for _, r := range rows {
		for _, k := range []string{"diskRead", "diskWrite", "gpu", "vramKB"} {
			assert.NotContains(t, obj(t, r), k)
		}
	}
	assert.NotNil(t, body["cursor"])
}

func TestPerformanceRail(t *testing.T) {
	if b, err := os.ReadFile("/proc/diskstats"); err != nil || len(b) == 0 {
		t.Skip("/proc/diskstats unavailable")
	}
	const mods = "modules=cpu,memory,disk-rate,net-rate,gpu,sensors,memory-modules"
	cursorOf := func(m map[string]any, key string) string {
		s, _ := obj(t, m[key])["cursor"].(string)
		return s
	}

	first := getJSON(t, "/gops/meta?"+mods, http.StatusOK)
	assert.Contains(t, obj(t, first["cpu"]), "usage")
	assert.NotNil(t, obj(t, first["memory"])["total"])
	assert.Contains(t, obj(t, first["sensors"]), "sensors")
	disks := list(obj(t, first["diskrate"])["disks"])
	if len(disks) == 0 {
		t.Skip("no disks reported by /proc/diskstats")
	}
	for _, d := range disks {
		assert.Nil(t, obj(t, d)["busy"], "busy present without a prior sample")
	}

	q := url.Values{}
	q.Set("cpu_cursor", cursorOf(first, "cpu"))
	q.Set("disk_rate_cursor", cursorOf(first, "diskrate"))
	time.Sleep(100 * time.Millisecond)
	second := getJSON(t, "/gops/meta?"+mods+"&"+q.Encode(), http.StatusOK)

	assert.Contains(t, obj(t, second["cpu"]), "usage")
	assert.NotEqual(t, q.Get("cpu_cursor"), cursorOf(second, "cpu"), "cpu cursor did not advance")
	withBusy := 0
	for _, d := range list(obj(t, second["diskrate"])["disks"]) {
		if obj(t, d)["busy"] != nil {
			withBusy++
		}
	}
	assert.Positive(t, withBusy, "no disk row gained busy from the disk_rate_cursor")
}
