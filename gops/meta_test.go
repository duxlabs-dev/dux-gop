package gops

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/duxlabs-dev/dux-gop/models"
)

func TestGetModulesListsNewModules(t *testing.T) {
	mods, err := NewGopsUtil().GetModules()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"sensors", "memory-modules", "process-gpu", "process-io", "process-net"} {
		if !slices.Contains(mods.Available, m) {
			t.Errorf("module %q not listed", m)
		}
	}
}

func TestGetMetaUnknownModule(t *testing.T) {
	if _, err := NewGopsUtil().GetMeta(context.Background(), []string{"nope"}, MetaParams{}); err == nil {
		t.Fatal("expected error for unknown module")
	}
}

func hasEntries(dir string) bool {
	e, err := os.ReadDir(dir)
	return err == nil && len(e) > 0
}

func TestGetMetaNewModules(t *testing.T) {
	check := func(t *testing.T, modules []string) {
		meta, err := NewGopsUtil().GetMeta(context.Background(), modules, MetaParams{NetRateAllInterfaces: true})
		if err != nil {
			t.Fatal(err)
		}
		if hasEntries("/sys/class/hwmon") && meta.Sensors == nil {
			t.Error("sensors missing")
		}
		if _, err := os.Stat("/run/udev/data/+dmi:id"); err == nil && meta.MemoryModules == nil {
			t.Error("memory-modules missing")
		}
	}
	t.Run("explicit", func(t *testing.T) { check(t, []string{"sensors", "memory-modules"}) })
	t.Run("all", func(t *testing.T) { check(t, []string{"all"}) })
}

func TestGetMetaKeysBackwardCompatible(t *testing.T) {
	meta, err := NewGopsUtil().GetMeta(context.Background(), []string{"cpu", "memory"}, MetaParams{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(meta)
	var out map[string]map[string]json.RawMessage
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	golden := map[string][]string{
		"cpu":    {"count", "model", "frequency", "usage", "temperature", "cursor"},
		"memory": {"total", "used", "free", "available"},
	}
	for mod, keys := range golden {
		if out[mod] == nil {
			t.Fatalf("module %s missing", mod)
		}
		for _, k := range keys {
			if _, ok := out[mod][k]; !ok {
				t.Errorf("%s.%s missing", mod, k)
			}
		}
	}
}

func ownRow(procs []*models.ProcessInfo) *models.ProcessInfo {
	for _, p := range procs {
		if p.PID == int32(os.Getpid()) {
			return p
		}
	}
	return nil
}

func TestGetMetaProcessModifiers(t *testing.T) {
	g := NewGopsUtil()
	ctx := context.Background()
	for _, mods := range [][]string{{"processes", "process-io"}, {"process-io"}, {"all", "process-io"}} {
		first, err := g.GetMeta(ctx, mods, MetaParams{ProcLimit: 0, EnableCPU: true})
		if err != nil || first.Cursor == "" {
			t.Fatalf("%v: err=%v cursor=%q", mods, err, first.Cursor)
		}
		time.Sleep(100 * time.Millisecond)
		second, err := g.GetMeta(ctx, mods, MetaParams{ProcLimit: 0, EnableCPU: true, ProcCursor: first.Cursor})
		if err != nil {
			t.Fatal(err)
		}
		if row := ownRow(second.Processes); row == nil || row.DiskRead == nil {
			t.Errorf("%v: own PID has no diskRead", mods)
		}
	}
}

func TestGetMetaProcessesNoExtras(t *testing.T) {
	g := NewGopsUtil()
	first, _ := g.GetMeta(context.Background(), []string{"processes"}, MetaParams{ProcLimit: 0})
	second, err := g.GetMeta(context.Background(), []string{"processes"}, MetaParams{ProcLimit: 0, ProcCursor: first.Cursor})
	if err != nil || len(second.Processes) == 0 {
		t.Fatal(err)
	}
	for _, p := range second.Processes {
		if p.GPU != nil || p.VRAMKB != nil || p.DiskRead != nil || p.DiskWrite != nil {
			t.Fatalf("unexpected extras on %d", p.PID)
		}
	}
}

func TestGetMetaAllProcessGPU(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("per-process GPU is Linux-only")
	}
	g := NewGopsUtil()
	// No detected GPU: nothing on the host is read, and the test process holds no GPU.
	g.detectGPUEntries = func() ([]gpuEntry, error) { return nil, nil }
	ctx := context.Background()
	first, err := g.GetMeta(ctx, []string{"all", "process-gpu"}, MetaParams{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.GetMeta(ctx, []string{"all", "process-gpu"}, MetaParams{ProcCursor: first.Cursor})
	if err != nil {
		t.Fatal(err)
	}
	row := ownRow(second.Processes)
	if row == nil || row.VRAMKB == nil || *row.VRAMKB != 0 || row.GPU == nil || *row.GPU != 0 {
		t.Fatalf("own row should be a known 0: %+v", row)
	}
}

func TestGetMetaModifierWithUnknownModule(t *testing.T) {
	if _, err := NewGopsUtil().GetMeta(context.Background(), []string{"process-io", "nope"}, MetaParams{}); err == nil {
		t.Fatal("expected error for unknown module alongside a modifier")
	}
}

func TestGetMetaTogglingModifiersKeepsCPURates(t *testing.T) {
	g := NewGopsUtil()
	ctx := context.Background()
	own := int32(os.Getpid())
	// Moving the own baseline back one CPU second makes every rate positive.
	rewind := func(cursor string) string {
		var entries []processSample
		for _, e := range decodeProcessCursor(cursor) {
			if e.PID == own {
				e.Ticks--
			}
			entries = append(entries, *e)
		}
		return encodeProcessCursor(entries)
	}
	first, err := g.GetMeta(ctx, []string{"processes"}, MetaParams{EnableCPU: true})
	if err != nil {
		t.Fatal(err)
	}
	cursor := first.Cursor
	for i, mods := range [][]string{{"processes", "process-io"}, {"processes"}, {"process-io"}} {
		next, err := g.GetMeta(ctx, mods, MetaParams{EnableCPU: true, ProcCursor: rewind(cursor)})
		if err != nil {
			t.Fatal(err)
		}
		if row := ownRow(next.Processes); row == nil || row.CPU <= 0 {
			t.Errorf("poll %d %v: own CPU rate lost: %+v", i, mods, row)
		}
		cursor = next.Cursor
	}
}

func TestMetaParamsProcessOptionsNet(t *testing.T) {
	opts, modifier := MetaParams{}.processOptions([]string{"process-net"})
	if !opts.Net || opts.IO || opts.GPU || !modifier {
		t.Fatalf("opts=%+v modifier=%v", opts, modifier)
	}
	if opts, _ := (MetaParams{}).processOptions([]string{"processes"}); opts.Net {
		t.Fatal("Net enabled without process-net")
	}
}

func TestMetaParamsProcessOptionsGPUPciIds(t *testing.T) {
	if opts, _ := (MetaParams{}).processOptions(nil); opts.GPUPciIds != nil {
		t.Errorf("nil: got %#v", opts.GPUPciIds)
	}
	opts, _ := MetaParams{GPUPciIds: []string{}}.processOptions(nil)
	if opts.GPUPciIds == nil || len(opts.GPUPciIds) != 0 {
		t.Errorf("empty: got %#v, want non-nil empty", opts.GPUPciIds)
	}
	opts, _ = MetaParams{GPUPciIds: []string{"10de:2684"}}.processOptions(nil)
	if len(opts.GPUPciIds) != 1 || opts.GPUPciIds[0] != "10de:2684" {
		t.Errorf("list: got %#v", opts.GPUPciIds)
	}
}
