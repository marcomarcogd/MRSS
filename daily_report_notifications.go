//go:build !server

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"

	"MRSS/internal/dailyreport"
	"MRSS/internal/models"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

const dailyReportNotificationCategory = "daily-report-completed"

type dailyReportSettings interface {
	GetSetting(string) (string, error)
}

// desktopDailyReportNotifier bridges the report service to the Wails
// notification service. Application events are emitted independently of the
// operating-system notification so the in-app badge and history stay current
// even when notification permission is unavailable.
type desktopDailyReportNotifier struct {
	service  *notifications.NotificationService
	app      *application.App
	settings dailyReportSettings

	categoryOnce sync.Once
	categoryErr  error
	statusMu     sync.RWMutex
	lastStatus   string
}

func newDesktopDailyReportNotifier(
	service *notifications.NotificationService,
	app *application.App,
	settings dailyReportSettings,
) *desktopDailyReportNotifier {
	return &desktopDailyReportNotifier{
		service:    service,
		app:        app,
		settings:   settings,
		lastStatus: dailyreport.NotificationNotDetermined,
	}
}

func (n *desktopDailyReportNotifier) Authorize(_ context.Context) (string, error) {
	if n.service == nil {
		return dailyreport.NotificationUnsupported, nil
	}

	authorized, err := n.service.RequestNotificationAuthorization()
	if err != nil {
		n.setStatus(dailyreport.NotificationUnsupported)
		return dailyreport.NotificationUnsupported, fmt.Errorf("request notification authorization: %w", err)
	}
	if !authorized {
		n.setStatus(dailyreport.NotificationDenied)
		return dailyreport.NotificationDenied, nil
	}

	n.setStatus(dailyreport.NotificationAuthorized)
	return dailyreport.NotificationAuthorized, nil
}

func (n *desktopDailyReportNotifier) AuthorizationStatus(_ context.Context) string {
	if n.service == nil {
		return dailyreport.NotificationUnsupported
	}

	authorized, err := n.service.CheckNotificationAuthorization()
	if err != nil {
		return dailyreport.NotificationUnsupported
	}
	if authorized {
		n.setStatus(dailyreport.NotificationAuthorized)
		return dailyreport.NotificationAuthorized
	}

	n.statusMu.RLock()
	defer n.statusMu.RUnlock()
	if n.lastStatus == dailyreport.NotificationDenied {
		return dailyreport.NotificationDenied
	}
	return dailyreport.NotificationNotDetermined
}

func (n *desktopDailyReportNotifier) NotifyCompleted(_ context.Context, run *models.DailyReportRun) error {
	if run == nil {
		return nil
	}

	var snapshot struct {
		SystemNotification bool `json:"system_notification"`
	}
	systemRequested := false
	systemDelivered := false
	defer func() {
		if n.app != nil {
			n.app.Event.Emit("daily-report:completed", map[string]interface{}{
				"run_id":           run.ID,
				"status":           run.Status,
				"system_requested": systemRequested,
				"system_delivered": systemDelivered,
			})
		}
	}()
	if run.ConfigSnapshot != "" {
		if err := json.Unmarshal([]byte(run.ConfigSnapshot), &snapshot); err != nil {
			log.Printf("Daily report %d has an invalid notification snapshot: %v", run.ID, err)
			return nil
		}
	}
	systemRequested = snapshot.SystemNotification
	if !systemRequested || n.AuthorizationStatus(context.Background()) != dailyreport.NotificationAuthorized {
		return nil
	}

	if err := n.ensureCategory(); err != nil {
		return err
	}

	language := "en"
	if n.settings != nil {
		if value, err := n.settings.GetSetting("language"); err == nil {
			language = value
		}
	}

	actionTitle := "View report"
	body := fmt.Sprintf("%d articles summarized", run.ArticleCount)
	if language == "zh" || language == "zh-CN" || language == "zh-cn" {
		actionTitle = "查看日报"
		body = fmt.Sprintf("已汇总 %d 篇文章", run.ArticleCount)
	}
	if run.Status == "partial" {
		if language == "zh" || language == "zh-CN" || language == "zh-cn" {
			body += "（部分内容已降级生成）"
		} else {
			body += " (completed with fallback content)"
		}
	}

	options := notifications.NotificationOptions{
		ID:         fmt.Sprintf("daily-report-%d", run.ID),
		Title:      run.Title,
		Body:       body,
		CategoryID: dailyReportNotificationCategory,
		ThreadID:   "mrss-daily-reports",
		Data: map[string]interface{}{
			"run_id": strconv.FormatInt(run.ID, 10),
		},
	}

	// Registering the category carries the localized action title. Re-register
	// when a non-English label is needed before sending the first notification.
	if actionTitle != "View report" {
		// The service treats registration as an update on supported platforms.
		_ = n.service.RegisterNotificationCategory(notifications.NotificationCategory{
			ID: dailyReportNotificationCategory,
			Actions: []notifications.NotificationAction{{
				ID:    "view",
				Title: actionTitle,
			}},
		})
	}

	if err := n.service.SendNotificationWithActions(options); err != nil {
		return fmt.Errorf("send daily report notification: %w", err)
	}
	systemDelivered = true
	return nil
}

func (n *desktopDailyReportNotifier) ensureCategory() error {
	n.categoryOnce.Do(func() {
		n.categoryErr = n.service.RegisterNotificationCategory(notifications.NotificationCategory{
			ID: dailyReportNotificationCategory,
			Actions: []notifications.NotificationAction{{
				ID:    "view",
				Title: "View report",
			}},
		})
	})
	if n.categoryErr != nil {
		return fmt.Errorf("register daily report notification category: %w", n.categoryErr)
	}
	return nil
}

func (n *desktopDailyReportNotifier) setStatus(status string) {
	n.statusMu.Lock()
	n.lastStatus = status
	n.statusMu.Unlock()
}

func dailyReportIDFromNotification(result notifications.NotificationResult) int64 {
	if result.Error != nil {
		return 0
	}
	value, ok := result.Response.UserInfo["run_id"]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case string:
		id, _ := strconv.ParseInt(typed, 10, 64)
		return id
	case float64:
		return int64(typed)
	case int64:
		return typed
	case int:
		return int64(typed)
	default:
		return 0
	}
}
