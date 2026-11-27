package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/LuSrackhall/tengshou/internal/model"
	"github.com/LuSrackhall/tengshou/internal/service"
	"github.com/LuSrackhall/tengshou/internal/store"
)

// Background jobs give long operations (Word→PDF conversion, PDF rendering,
// batch generation, Excel import) real staged progress instead of a greyed-out
// button. Jobs live in memory only: TTL 10 minutes, at most 100 entries, and
// lazy cleanup on every start/read. The synchronous endpoints and the MCP
// tools stay untouched; only the desktop UI polls jobs.
const (
	jobTTL      = 10 * time.Minute
	jobMaxItems = 100

	jobStatusRunning = "running"
	jobStatusDone    = "done"
	jobStatusFailed  = "failed"
)

// jobRun is one background operation. report publishes progress updates;
// ctx is the server-lifetime context, so shutdown cancels conversions.
type jobRun func(ctx context.Context, report service.ProgressFunc) (any, error)

// jobEntry is the mutable state of one job. All fields are guarded by the
// registry mutex.
type jobEntry struct {
	id       string
	kind     string
	started  time.Time
	finished time.Time
	status   string
	stage    string
	message  string
	current  int
	total    int
	result   any
	errBody  map[string]any
	cancel   context.CancelFunc
}

// jobSnapshot is the JSON contract of GET /app/api/v1/jobs/{id}. On success
// result carries the same shape as the corresponding synchronous response; on
// failure error is the same error envelope the synchronous API returns.
type jobSnapshot struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Status    string         `json:"status"`
	Stage     string         `json:"stage"`
	Message   string         `json:"message"`
	Current   int            `json:"current"`
	Total     int            `json:"total"`
	ElapsedMS int64          `json:"elapsed_ms"`
	Result    any            `json:"result,omitempty"`
	Error     map[string]any `json:"error,omitempty"`
}

// jobRegistry owns every in-flight and recently finished job.
type jobRegistry struct {
	mu    sync.Mutex
	items map[string]*jobEntry
	order []string // insertion order, used for TTL cleanup and the size cap
	now   func() time.Time
	ttl   time.Duration
	max   int
	// logger records the concrete cause of failed jobs; jobErrorBody collapses
	// unknown errors into a generic envelope, so this is the only trail.
	// Nil is valid (tests, read-only embeds).
	logger *slog.Logger
}

func newJobRegistry(logger *slog.Logger) *jobRegistry {
	return &jobRegistry{
		items:  map[string]*jobEntry{},
		now:    time.Now,
		ttl:    jobTTL,
		max:    jobMaxItems,
		logger: logger,
	}
}

// Start registers one job and launches it. The returned id is what the UI
// polls; report updates are applied under the registry lock.
func (r *jobRegistry) Start(parent context.Context, kind string, run jobRun) (string, error) {
	id, err := model.NewID("job")
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithCancel(parent)
	now := r.now()
	entry := &jobEntry{
		id: id, kind: kind, started: now, status: jobStatusRunning,
		stage: "queued", message: "任务已创建…", cancel: cancel,
	}
	r.mu.Lock()
	r.pruneLocked(now)
	r.evictLocked()
	r.items[id] = entry
	r.order = append(r.order, id)
	r.mu.Unlock()

	go func() {
		result, runErr := run(ctx, func(p service.Progress) { r.update(id, p) })
		if runErr != nil && r.logger != nil {
			// The snapshot only carries the sanitized envelope; log the cause.
			r.logger.Error("后台任务失败", "job", id, "kind", kind, "error", runErr)
		}
		r.finish(id, result, runErr)
	}()
	return id, nil
}

// Snapshot returns the current visible state of one job.
func (r *jobRegistry) Snapshot(id string) (jobSnapshot, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.pruneLocked(now)
	entry, ok := r.items[id]
	if !ok {
		return jobSnapshot{}, false
	}
	return entry.snapshot(now), true
}

// CancelAll marks running jobs as cancelled and terminates their contexts.
// It runs on server shutdown so a Word conversion cannot outlive the process.
func (r *jobRegistry) CancelAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	for _, entry := range r.items {
		if entry.status != jobStatusRunning {
			continue
		}
		entry.status = jobStatusFailed
		entry.finished = now
		entry.errBody = map[string]any{
			"code": "cancelled", "message": "服务正在关闭，任务已取消",
		}
		if entry.cancel != nil {
			entry.cancel()
		}
	}
}

func (r *jobRegistry) update(id string, p service.Progress) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.items[id]
	if !ok {
		return
	}
	if p.Stage != "" {
		entry.stage = p.Stage
	}
	if p.Message != "" {
		entry.message = p.Message
	}
	entry.current = p.Current
	entry.total = p.Total
}

func (r *jobRegistry) finish(id string, result any, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	entry, ok := r.items[id]
	if !ok {
		return
	}
	// CancelAll already recorded the shutdown outcome; keep it.
	if entry.status != jobStatusRunning {
		return
	}
	entry.finished = r.now()
	if err != nil {
		entry.status = jobStatusFailed
		entry.errBody = jobErrorBody(err)
		// A failed Excel commit still carries partial per-row results; the UI
		// renders them exactly like the synchronous 422 response.
		entry.result = result
		return
	}
	entry.status = jobStatusDone
	entry.result = result
	if entry.stage == "queued" {
		entry.stage = "done"
	}
	if entry.message == "" {
		entry.message = "已完成"
	}
}

// pruneLocked drops finished jobs older than ttl.
func (r *jobRegistry) pruneLocked(now time.Time) {
	if r.ttl <= 0 || len(r.order) == 0 {
		return
	}
	kept := r.order[:0]
	for _, id := range r.order {
		entry, ok := r.items[id]
		if !ok {
			continue
		}
		if entry.status != jobStatusRunning && !entry.finished.IsZero() &&
			now.Sub(entry.finished) > r.ttl {
			delete(r.items, id)
			continue
		}
		kept = append(kept, id)
	}
	r.order = kept
}

// evictLocked enforces the hard size cap by dropping the oldest job, even when
// it is still running (its context is cancelled so it stops promptly).
func (r *jobRegistry) evictLocked() {
	if r.max <= 0 {
		return
	}
	for len(r.items) >= r.max && len(r.order) > 0 {
		id := r.order[0]
		r.order = r.order[1:]
		entry, ok := r.items[id]
		if !ok {
			continue
		}
		if entry.status == jobStatusRunning && entry.cancel != nil {
			entry.cancel()
		}
		delete(r.items, id)
	}
}

func (j *jobEntry) snapshot(now time.Time) jobSnapshot {
	end := j.finished
	if j.status == jobStatusRunning || end.IsZero() {
		end = now
	}
	return jobSnapshot{
		ID: j.id, Kind: j.kind, Status: j.status,
		Stage: j.stage, Message: j.message,
		Current: j.current, Total: j.total,
		ElapsedMS: end.Sub(j.started).Milliseconds(),
		Result:    j.result,
		Error:     j.errBody,
	}
}

// jobErrorBody converts a service error into the same error envelope the
// synchronous endpoints write. Unknown errors become a generic internal error;
// the concrete cause is logged when the job finishes (see Start).
func jobErrorBody(err error) map[string]any {
	_, body := describeServiceError(err)
	if body == nil {
		return map[string]any{"code": "internal", "message": "internal server error"}
	}
	return body
}

// describeServiceError maps a service error to its HTTP status and error body.
// The synchronous handlers and the job registry share this table so both
// surfaces report identical codes and messages.
func describeServiceError(err error) (int, map[string]any) {
	var validation *service.ValidationErrors
	if errors.As(err, &validation) {
		return http.StatusUnprocessableEntity, map[string]any{
			"code": "validation", "message": validation.Error(), "fields": validation.Items,
		}
	}
	var businessErr *service.Error
	if errors.As(err, &businessErr) {
		status := http.StatusBadRequest
		switch businessErr.Code {
		case "not_found":
			status = http.StatusNotFound
		case "conflict", "immutable", "in_use", "duplicate", "ambiguous_party",
			"preview_required", "missing_asset", "invalid_template", "content_changed",
			"duplicate_id_number", "lease_conflict", "renewal_termination_confirm":
			status = http.StatusConflict
		case "import_errors", "converter_unavailable", "convert_failed",
			"invalid_pdf", "invalid_kind", "too_many_placements",
			"invalid_backup", "confirm_required", "conflict_unresolved",
			"missing_signature", "missing_field", "missing_party", "missing_seal",
			"preflight_failed", "too_many_contracts":
			status = http.StatusUnprocessableEntity
		case "file_too_large", "backup_too_large":
			status = http.StatusRequestEntityTooLarge
		case "font_unavailable":
			status = http.StatusServiceUnavailable
		}
		return status, map[string]any{"code": businessErr.Code, "message": businessErr.Message}
	}
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound, map[string]any{"code": "not_found", "message": "记录不存在"}
	}
	if errors.Is(err, context.Canceled) {
		return http.StatusServiceUnavailable, map[string]any{
			"code": "cancelled", "message": "任务已取消",
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusServiceUnavailable, map[string]any{
			"code": "timeout", "message": "任务超时，未在预期时间内完成",
		}
	}
	return http.StatusInternalServerError, nil
}
