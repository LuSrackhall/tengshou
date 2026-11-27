package service

// Progress is one visible update of a long-running operation. Stage values are
// stable machine identifiers; Message is the user-facing Chinese text shown by
// the job progress panel. Current/Total are the real item counters when the
// operation can count items (batch generation, Excel rows); zero means the
// total is unknown and the UI renders an indeterminate bar instead of
// inventing a percentage.
type Progress struct {
	Stage   string
	Message string
	Current int
	Total   int
}

// Progress stages shared by the synchronous and background paths.
const (
	ProgressSaving     = "saving"
	ProgressConverting = "converting"
	ProgressParsing    = "parsing"
	ProgressRendering  = "rendering"
	ProgressWriting    = "writing"
	ProgressBatch      = "batch"
)

// ProgressFunc receives progress updates. A nil func means "no observer" and
// every call site must tolerate it: the synchronous API and MCP paths pass nil
// and keep their behavior byte-for-byte.
type ProgressFunc func(Progress)

// Report publishes one update. It is nil-safe so call sites never branch on
// whether a progress observer was supplied.
func (fn ProgressFunc) Report(stage, message string, current, total int) {
	if fn == nil {
		return
	}
	fn(Progress{Stage: stage, Message: message, Current: current, Total: total})
}
