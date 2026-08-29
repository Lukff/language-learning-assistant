// services/jobs_notifier.go
package services

import (
	"assistente-idiomas/internal/jobs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// JobUpdatedEvent is the name of the Wails event emitted on every job
// status transition — no screen consumes it yet (that's for Stories 5 and
// 7); this story only sets up the transport.
const JobUpdatedEvent = "job:updated"

// WailsJobNotifier implements jobs.Notifier by emitting Wails events — the only
// piece of the pipeline (Story 4) that knows Wails exists. internal/jobs
// itself does not import Wails (thin layer).
type WailsJobNotifier struct{}

func (WailsJobNotifier) JobChanged(e jobs.JobEvent) {
	app := application.Get()
	if app == nil {
		// The job worker is started before application.New() runs
		// (main.go — intentional ordering). In that window, application.Get()
		// returns nil and emitting would panic in the worker's goroutine.
		// Discarding the event here is harmless: no screen consumes
		// job:updated yet.
		return
	}
	app.Event.Emit(JobUpdatedEvent, e)
}
