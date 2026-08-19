package dailyreport

import (
	"context"

	"MRSS/internal/models"
)

const (
	NotificationAuthorized    = "authorized"
	NotificationDenied        = "denied"
	NotificationNotDetermined = "not_determined"
	NotificationUnsupported   = "unsupported"
)

// Notifier abstracts platform notifications. Desktop mode injects a Wails
// implementation; server mode intentionally uses NoopNotifier.
type Notifier interface {
	Authorize(context.Context) (string, error)
	AuthorizationStatus(context.Context) string
	NotifyCompleted(context.Context, *models.DailyReportRun) error
}

type NoopNotifier struct{}

func (NoopNotifier) Authorize(context.Context) (string, error) {
	return NotificationUnsupported, nil
}
func (NoopNotifier) AuthorizationStatus(context.Context) string {
	return NotificationUnsupported
}
func (NoopNotifier) NotifyCompleted(context.Context, *models.DailyReportRun) error { return nil }

func validNotificationStatus(status string) string {
	switch status {
	case NotificationAuthorized, NotificationDenied, NotificationNotDetermined, NotificationUnsupported:
		return status
	default:
		return NotificationUnsupported
	}
}
