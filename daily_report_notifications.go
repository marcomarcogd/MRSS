//go:build !server

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"

	"MRSS/internal/dailyreport"
	"MRSS/internal/models"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

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

	statusMu   sync.RWMutex
	lastStatus string
	deliveryMu sync.Mutex
	eventSent  map[int64]struct{}
	systemSent map[int64]struct{}
	opened     map[int64]struct{}
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
		eventSent:  make(map[int64]struct{}),
		systemSent: make(map[int64]struct{}),
		opened:     make(map[int64]struct{}),
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
	emitCompletion := n.claimDelivery(n.eventSent, run.ID)
	defer func() {
		if emitCompletion && n.app != nil {
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
	if !n.claimDelivery(n.systemSent, run.ID) {
		log.Printf("daily report: notification skipped run=%d channel=system reason=duplicate", run.ID)
		return nil
	}

	language := "en"
	if n.settings != nil {
		if value, err := n.settings.GetSetting("language"); err == nil {
			language = value
		}
	}

	body := dailyReportNotificationPreview(run)
	if language == "zh" || language == "zh-CN" || language == "zh-cn" {
		if body == "" {
			body = fmt.Sprintf("已汇总 %d 篇文章", run.ArticleCount)
		}
	} else if body == "" {
		body = fmt.Sprintf("%d articles summarized", run.ArticleCount)
	}

	options := notifications.NotificationOptions{
		ID:    fmt.Sprintf("daily-report-%d", run.ID),
		Title: run.Title,
		Body:  body,
		Data: map[string]interface{}{
			"run_id": strconv.FormatInt(run.ID, 10),
		},
	}

	if err := n.service.SendNotification(options); err != nil {
		return fmt.Errorf("send daily report notification: %w", err)
	}
	systemDelivered = true
	return nil
}

func (n *desktopDailyReportNotifier) claimDelivery(values map[int64]struct{}, runID int64) bool {
	n.deliveryMu.Lock()
	defer n.deliveryMu.Unlock()
	if _, exists := values[runID]; exists {
		return false
	}
	values[runID] = struct{}{}
	return true
}

func (n *desktopDailyReportNotifier) claimOpen(runID int64) bool {
	return n.claimDelivery(n.opened, runID)
}

func dailyReportNotificationPreview(run *models.DailyReportRun) string {
	if run == nil || strings.TrimSpace(run.ContentJSON) == "" {
		return ""
	}
	var content dailyreport.ReportContent
	if json.Unmarshal([]byte(run.ContentJSON), &content) != nil {
		return ""
	}
	var preview string
	for _, section := range content.Sections {
		if section.ID == "highlights" {
			preview = firstReportBlockText(section)
			break
		}
	}
	if preview == "" && len(content.Sections) > 0 {
		preview = firstReportBlockText(content.Sections[0])
	}
	preview = strings.Join(strings.Fields(preview), " ")
	runes := []rune(preview)
	if len(runes) > 140 {
		preview = strings.TrimSpace(string(runes[:140])) + "…"
	}
	return preview
}

func firstReportBlockText(section dailyreport.ReportSection) string {
	for _, block := range section.Blocks {
		for _, item := range block.Items {
			if strings.TrimSpace(item.Text) != "" {
				return item.Text
			}
		}
		if block.Type != dailyreport.ReportBlockHeading && strings.TrimSpace(block.Text) != "" {
			return block.Text
		}
	}
	return section.Summary
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
