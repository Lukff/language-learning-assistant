// services/jobs_notifier.go
package services

import (
	"assistente-idiomas/internal/jobs"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// JobUpdatedEvent é o nome do evento Wails emitido a cada transição de
// status de job — nenhuma tela consome isso ainda (fica pras Históras 5 e
// 7); esta história só monta o transporte.
const JobUpdatedEvent = "job:updated"

// WailsJobNotifier implementa jobs.Notifier emitindo eventos Wails — única
// peça do pipeline (História 4) que sabe que o Wails existe. internal/jobs
// em si não importa Wails (camada fina).
type WailsJobNotifier struct{}

func (WailsJobNotifier) JobChanged(e jobs.JobEvent) {
	application.Get().Event.Emit(JobUpdatedEvent, e)
}
