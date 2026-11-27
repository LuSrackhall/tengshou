package httpapi

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/LuSrackhall/tengshou/internal/service"
)

func waitJobStatus(t *testing.T, reg *jobRegistry, id, want string) jobSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap, ok := reg.Snapshot(id)
		if ok && snap.Status == want {
			return snap
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s never reached %q (last ok=%v snap=%+v)", id, want, ok, snap)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestJobRegistryExpiresFinishedJobs(t *testing.T) {
	reg := newJobRegistry(nil)
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	reg.now = func() time.Time { return now }
	reg.ttl = time.Minute

	id, err := reg.Start(context.Background(), "test", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		report(service.Progress{Stage: service.ProgressRendering, Message: "正在处理…", Current: 1, Total: 2})
		return map[string]any{"ok": true}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJobStatus(t, reg, id, jobStatusDone)
	if snap.Stage != service.ProgressRendering || snap.Current != 1 || snap.Total != 2 {
		t.Fatalf("snapshot = %+v", snap)
	}
	// The result is retained for the TTL window, then lazily pruned.
	now = now.Add(2 * time.Minute)
	if _, ok := reg.Snapshot(id); ok {
		t.Fatalf("finished job %s survived the TTL", id)
	}
}

func TestJobRegistryCapsRunningJobsAndCancelsEvicted(t *testing.T) {
	reg := newJobRegistry(nil)
	reg.max = 2
	reg.ttl = time.Hour

	started := make(chan context.Context, 3)
	run := func(ctx context.Context, report service.ProgressFunc) (any, error) {
		started <- ctx
		<-ctx.Done()
		return nil, ctx.Err()
	}
	var ids []string
	for i := 0; i < 3; i++ {
		id, err := reg.Start(context.Background(), "test", run)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	// Every runner publishes its context once scheduled; the evicted job's
	// context must already be cancelled.
	cancelled := 0
	for i := 0; i < 3; i++ {
		select {
		case ctx := <-started:
			if ctx.Err() != nil {
				cancelled++
			}
		case <-time.After(time.Second):
			t.Fatal("job runner did not start")
		}
	}
	if cancelled != 1 {
		t.Fatalf("cancelled contexts = %d, want 1", cancelled)
	}
	reg.mu.Lock()
	size := len(reg.items)
	_, firstKept := reg.items[ids[0]]
	reg.mu.Unlock()
	if size != 2 || firstKept {
		t.Fatalf("registry size=%d firstKept=%v", size, firstKept)
	}
	reg.CancelAll()
}

func TestJobRegistryCancelAllMarksFailed(t *testing.T) {
	reg := newJobRegistry(nil)
	started := make(chan struct{})
	id, err := reg.Start(context.Background(), "test", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	reg.CancelAll()
	snap, ok := reg.Snapshot(id)
	if !ok || snap.Status != jobStatusFailed {
		t.Fatalf("snapshot = %+v ok=%v", snap, ok)
	}
	if snap.Error["code"] != "cancelled" {
		t.Fatalf("error = %+v", snap.Error)
	}
}

// A failed job must keep the concrete cause in the server log: the snapshot
// only carries the sanitized envelope, so without this the error is swallowed.
func TestJobRegistryLogsFailedRunCause(t *testing.T) {
	var buf bytes.Buffer
	reg := newJobRegistry(slog.New(slog.NewTextHandler(&buf, nil)))
	cause := errors.New("模板文件写入失败：磁盘只读")
	id, err := reg.Start(context.Background(), "template_import", func(ctx context.Context, report service.ProgressFunc) (any, error) {
		return nil, cause
	})
	if err != nil {
		t.Fatal(err)
	}
	snap := waitJobStatus(t, reg, id, jobStatusFailed)
	if snap.Error["code"] != "internal" {
		t.Fatalf("error envelope = %+v", snap.Error)
	}
	log := buf.String()
	for _, want := range []string{"后台任务失败", id, "template_import", cause.Error()} {
		if !strings.Contains(log, want) {
			t.Fatalf("log %q missing %q", log, want)
		}
	}
}
