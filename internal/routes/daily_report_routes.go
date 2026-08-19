package routes

import (
	"net/http"

	"MRSS/internal/handlers/core"
	dailyreport "MRSS/internal/handlers/dailyreport"
)

func registerDailyReportRoutes(mux *http.ServeMux, h *core.Handler) {
	mux.HandleFunc("/api/daily-report/config", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleConfig(h, w, r) })
	mux.HandleFunc("/api/daily-report/consent", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleConsent(h, w, r) })
	mux.HandleFunc("/api/daily-report/outline/optimize", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleOptimizeOutline(h, w, r) })
	mux.HandleFunc("/api/daily-report/status", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleStatus(h, w, r) })
	mux.HandleFunc("/api/daily-report/generate", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleGenerate(h, w, r) })
	mux.HandleFunc("/api/daily-report/missed-runs", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleMissedRuns(h, w, r) })
	mux.HandleFunc("/api/daily-report/history", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleHistory(h, w, r) })
	mux.HandleFunc("/api/daily-report/history/", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleHistory(h, w, r) })
	mux.HandleFunc("/api/daily-report/notifications/authorize", func(w http.ResponseWriter, r *http.Request) { dailyreport.HandleAuthorizeNotifications(h, w, r) })
}
