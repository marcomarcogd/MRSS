package database_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbpkg "MRSS/internal/database"
	"MRSS/internal/models"
)

func setupDBWithFeed(t *testing.T) *dbpkg.DB {
	t.Helper()
	db := setupTestDB(t)

	// Insert a feed to satisfy foreign key joins
	res, err := db.Exec(`INSERT INTO feeds (title, url, category, is_image_mode, hide_from_timeline) VALUES (?, ?, ?, ?, ?)`, "Test Feed", "https://example.com/feed", "news", 0, 0)
	if err != nil {
		t.Fatalf("insert feed error: %v", err)
	}
	_, _ = res.LastInsertId()
	return db
}

func TestCleanupBySizePreservesUnreadMetadataAndDeletesContentFirst(t *testing.T) {
	db := setupDBWithFeed(t)
	if err := db.SetSetting("max_cache_size_mb", "1"); err != nil {
		t.Fatalf("SetSetting error: %v", err)
	}

	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	res, err := db.Exec(
		`INSERT INTO articles (feed_id, title, url, published_at, is_read, is_favorite, is_read_later, unique_id) VALUES (?, ?, ?, ?, 0, 0, 0, ?)`,
		feedID,
		"Old unread article",
		"https://example.com/old",
		time.Now().AddDate(-10, 0, 0),
		"cleanup-preserve-unread",
	)
	if err != nil {
		t.Fatalf("insert article: %v", err)
	}
	articleID, _ := res.LastInsertId()

	if err := db.SetArticleContent(articleID, strings.Repeat("content ", 200000)); err != nil {
		t.Fatalf("SetArticleContent error: %v", err)
	}

	deleted, err := db.CleanupBySize()
	if err != nil {
		t.Fatalf("CleanupBySize error: %v", err)
	}
	if deleted == 0 {
		t.Fatalf("expected cleanup to delete cached content")
	}

	var isRead bool
	if err := db.QueryRow(`SELECT is_read FROM articles WHERE id = ?`, articleID).Scan(&isRead); err != nil {
		t.Fatalf("expected article metadata to remain: %v", err)
	}
	if isRead {
		t.Fatalf("expected unread state to be preserved")
	}

	_, found, err := db.GetArticleContent(articleID)
	if err != nil {
		t.Fatalf("GetArticleContent error: %v", err)
	}
	if found {
		t.Fatalf("expected cached article content to be removed")
	}
}

func TestCleanupReadArticlesOverPerFeedLimitKeepsFeedsIndependent(t *testing.T) {
	db := setupDBWithFeed(t)

	var busyFeedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&busyFeedID); err != nil {
		t.Fatalf("scan busy feed id: %v", err)
	}
	res, err := db.Exec(`INSERT INTO feeds (title, url, category) VALUES (?, ?, ?)`, "Slow Feed", "https://example.com/slow-feed", "blogs")
	if err != nil {
		t.Fatalf("insert slow feed: %v", err)
	}
	slowFeedID, _ := res.LastInsertId()

	now := time.Now()
	busyRows := []struct {
		title       string
		isRead      int
		isFavorite  int
		isReadLater int
	}{
		{"busy-newest", 1, 0, 0},
		{"busy-middle", 1, 0, 0},
		{"busy-unread-protected", 0, 0, 0},
		{"busy-readlater-protected", 1, 0, 1},
		{"busy-favorite-protected", 1, 1, 0},
		{"busy-oldest-read", 1, 0, 0},
	}
	for i, row := range busyRows {
		_, err := db.Exec(
			`INSERT INTO articles (feed_id, title, url, published_at, is_read, is_favorite, is_read_later, unique_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			busyFeedID,
			row.title,
			"https://example.com/"+row.title,
			now.Add(-time.Duration(i)*time.Hour),
			row.isRead,
			row.isFavorite,
			row.isReadLater,
			row.title,
		)
		if err != nil {
			t.Fatalf("insert busy article %q: %v", row.title, err)
		}
	}

	for i := 0; i < 2; i++ {
		title := fmt.Sprintf("slow-%d", i)
		_, err := db.Exec(
			`INSERT INTO articles (feed_id, title, url, published_at, is_read, is_favorite, is_read_later, unique_id) VALUES (?, ?, ?, ?, 1, 0, 0, ?)`,
			slowFeedID,
			title,
			"https://example.com/"+title,
			now.Add(-time.Duration(i)*time.Hour),
			title,
		)
		if err != nil {
			t.Fatalf("insert slow article %q: %v", title, err)
		}
	}

	deleted, err := db.CleanupReadArticlesOverPerFeedLimit(3)
	if err != nil {
		t.Fatalf("CleanupReadArticlesOverPerFeedLimit error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("expected 1 old read article deleted, got %d", deleted)
	}

	var deletedCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles WHERE title = ?`, "busy-oldest-read").Scan(&deletedCount); err != nil {
		t.Fatalf("count deleted article: %v", err)
	}
	if deletedCount != 0 {
		t.Fatalf("expected oldest unprotected busy article to be deleted")
	}

	protectedTitles := []string{"busy-unread-protected", "busy-readlater-protected", "busy-favorite-protected"}
	for _, title := range protectedTitles {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM articles WHERE title = ?`, title).Scan(&count); err != nil {
			t.Fatalf("count protected article %q: %v", title, err)
		}
		if count != 1 {
			t.Fatalf("expected protected article %q to remain", title)
		}
	}

	var slowCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles WHERE feed_id = ?`, slowFeedID).Scan(&slowCount); err != nil {
		t.Fatalf("count slow feed articles: %v", err)
	}
	if slowCount != 2 {
		t.Fatalf("expected slow feed to remain untouched, got %d articles", slowCount)
	}
}

func TestGetArticlesWithUnreadFilterCombinesWithFavorites(t *testing.T) {
	db := setupDBWithFeed(t)

	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	rows := []struct {
		title      string
		url        string
		isRead     int
		isFavorite int
	}{
		{"Unread favorite", "https://example.com/unread-favorite", 0, 1},
		{"Read favorite", "https://example.com/read-favorite", 1, 1},
		{"Unread normal", "https://example.com/unread-normal", 0, 0},
	}
	for _, row := range rows {
		if _, err := db.Exec(
			`INSERT INTO articles (feed_id, title, url, published_at, is_read, is_favorite, is_read_later, unique_id) VALUES (?, ?, ?, ?, ?, ?, 0, ?)`,
			feedID,
			row.title,
			row.url,
			time.Now(),
			row.isRead,
			row.isFavorite,
			row.url,
		); err != nil {
			t.Fatalf("insert article %q: %v", row.title, err)
		}
	}

	articles, err := db.GetArticlesWithUnreadFilter("favorites", 0, "", false, true, 10, 0)
	if err != nil {
		t.Fatalf("GetArticlesWithUnreadFilter error: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 unread favorite, got %d", len(articles))
	}
	if articles[0].Title != "Unread favorite" || articles[0].IsRead || !articles[0].IsFavorite {
		t.Fatalf("unexpected article returned: %+v", articles[0])
	}
}

func TestSaveAndGetArticle(t *testing.T) {
	db := setupDBWithFeed(t)

	// Get feed id
	var feedID int64
	row := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed")
	if err := row.Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	a := &models.Article{
		FeedID:      feedID,
		Title:       "Hello",
		URL:         "https://example.com/article/1",
		ImageURL:    "https://example.com/img.jpg",
		PublishedAt: time.Now(),
	}

	if err := db.SaveArticle(a); err != nil {
		t.Fatalf("SaveArticle error: %v", err)
	}

	// Retrieve by GetArticles
	list, err := db.GetArticles("all", 0, "", false, 10, 0)
	if err != nil {
		t.Fatalf("GetArticles error: %v", err)
	}
	if len(list) == 0 {
		t.Fatalf("expected at least one article, got 0")
	}

	// Get by ID
	got, err := db.GetArticleByID(list[0].ID)
	if err != nil {
		t.Fatalf("GetArticleByID error: %v", err)
	}
	if got.URL != a.URL || got.Title != a.Title {
		t.Fatalf("retrieved article mismatch: %+v vs %+v", got, a)
	}
	if got.FirstSeenAt.IsZero() {
		t.Fatal("expected SaveArticle to set first_seen_at")
	}
}

func TestSaveArticlesPreservesFirstSeenAt(t *testing.T) {
	db := setupDBWithFeed(t)
	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	firstSeenAt := time.Date(2026, time.August, 10, 8, 0, 0, 0, time.UTC)
	article := &models.Article{
		FeedID:                feedID,
		Title:                 "immutable first seen",
		URL:                   "https://example.com/first-seen",
		PublishedAt:           firstSeenAt.Add(-time.Hour),
		FirstSeenAt:           firstSeenAt,
		HasValidPublishedTime: true,
	}
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("first SaveArticles: %v", err)
	}
	article.FirstSeenAt = firstSeenAt.Add(24 * time.Hour)
	article.URL += "?updated=1"
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("second SaveArticles: %v", err)
	}
	var stored time.Time
	if err := db.QueryRow(`SELECT first_seen_at FROM articles WHERE feed_id = ?`, feedID).Scan(&stored); err != nil {
		t.Fatalf("scan first_seen_at: %v", err)
	}
	if !stored.Equal(firstSeenAt) {
		t.Fatalf("first_seen_at changed on refresh: got %v, want %v", stored, firstSeenAt)
	}
	got, err := db.GetArticles("all", feedID, "", false, 10, 0)
	if err != nil {
		t.Fatalf("GetArticles: %v", err)
	}
	if len(got) != 1 || !got[0].FirstSeenAt.Equal(firstSeenAt) {
		t.Fatalf("GetArticles did not return first_seen_at: %+v", got)
	}
}

func TestDailyReportDataLifecycleAndCandidates(t *testing.T) {
	db := setupDBWithFeed(t)
	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}
	periodStart := time.Date(2026, time.August, 18, 8, 0, 0, 0, time.UTC)
	periodEnd := periodStart.Add(24 * time.Hour)
	articles := []*models.Article{
		{FeedID: feedID, Title: "normal", URL: "https://example.com/normal", PublishedAt: periodStart.Add(time.Hour), FirstSeenAt: periodStart.Add(2 * time.Hour), HasValidPublishedTime: true, OriginalSummary: "normal summary"},
		{FeedID: feedID, Title: "late", URL: "https://example.com/late", PublishedAt: periodStart.Add(-48 * time.Hour), FirstSeenAt: periodStart.Add(3 * time.Hour), HasValidPublishedTime: true, OriginalSummary: "late summary"},
		{FeedID: feedID, Title: "future", URL: "https://example.com/future", PublishedAt: periodEnd.Add(48 * time.Hour), FirstSeenAt: periodStart.Add(4 * time.Hour), HasValidPublishedTime: true},
		{FeedID: feedID, Title: "no-date-in-window", URL: "https://example.com/no-date-in-window", PublishedAt: periodStart.Add(-96 * time.Hour), FirstSeenAt: periodStart.Add(6 * time.Hour), HasValidPublishedTime: false},
		{FeedID: feedID, Title: "no-date-outside", URL: "https://example.com/no-date-outside", PublishedAt: periodStart.Add(6 * time.Hour), FirstSeenAt: periodEnd.Add(time.Hour), HasValidPublishedTime: false},
		{FeedID: feedID, Title: "hidden", URL: "https://example.com/hidden", PublishedAt: periodStart.Add(5 * time.Hour), FirstSeenAt: periodStart.Add(5 * time.Hour), HasValidPublishedTime: true, IsHidden: true},
		{FeedID: feedID, Title: "old", URL: "https://example.com/old", PublishedAt: periodStart.Add(-72 * time.Hour), FirstSeenAt: periodStart.Add(-72 * time.Hour), HasValidPublishedTime: true},
	}
	if err := db.SaveArticles(context.Background(), articles); err != nil {
		t.Fatalf("SaveArticles: %v", err)
	}
	var normalID int64
	if err := db.QueryRow(`SELECT id FROM articles WHERE title = 'normal'`).Scan(&normalID); err != nil {
		t.Fatalf("scan normal article id: %v", err)
	}
	if err := db.SetArticleContent(normalID, "cached full content"); err != nil {
		t.Fatalf("SetArticleContent: %v", err)
	}
	if err := db.UpdateArticleSummaryWithMetadata(normalID, "cached AI summary", "ai_daily_report", "summary-fingerprint", "content-fingerprint"); err != nil {
		t.Fatalf("UpdateArticleSummaryWithMetadata: %v", err)
	}

	config, err := db.GetDailyReportConfig()
	if err != nil {
		t.Fatalf("GetDailyReportConfig: %v", err)
	}
	config.Enabled = true
	config.FeedScope = "selected"
	config.IncludeHidden = false
	config.LastHandledBoundary = &periodStart
	if err := db.SaveDailyReportConfig(config, []int64{feedID, feedID}); err != nil {
		t.Fatalf("SaveDailyReportConfig: %v", err)
	}
	selected, err := db.GetDailyReportSelectedFeedIDs()
	if err != nil || len(selected) != 1 || selected[0] != feedID {
		t.Fatalf("selected feeds = %v, err=%v", selected, err)
	}

	candidates, err := db.ListDailyReportCandidates(models.DailyReportCandidateFilter{
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		FeedIDs:     selected,
	})
	if err != nil {
		t.Fatalf("ListDailyReportCandidates: %v", err)
	}
	if len(candidates) != 4 {
		t.Fatalf("candidate count = %d, want 4: %+v", len(candidates), candidates)
	}
	lateByTitle := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		lateByTitle[candidate.Title] = candidate.LateArrival
		if candidate.Title == "normal" {
			if candidate.Content != "cached full content" {
				t.Fatalf("normal candidate content = %q", candidate.Content)
			}
			if candidate.GeneratedSummary != "cached AI summary" || candidate.SummarySource != "ai_daily_report" ||
				candidate.SummaryFingerprint != "summary-fingerprint" || candidate.SummaryContentHash != "content-fingerprint" {
				t.Fatalf("normal candidate summary metadata = %+v", candidate)
			}
		}
	}
	if !lateByTitle["late"] || lateByTitle["normal"] || lateByTitle["future"] || lateByTitle["no-date-in-window"] {
		t.Fatalf("unexpected late arrival flags: %v", lateByTitle)
	}
	if _, exists := lateByTitle["no-date-outside"]; exists {
		t.Fatal("article without pubDate was selected by fallback published_at instead of first_seen_at")
	}

	run := &models.DailyReportRun{
		Kind:           "auto",
		Status:         "completed",
		PeriodStart:    periodStart,
		PeriodEnd:      periodEnd,
		Title:          "Daily report",
		ConfigSnapshot: `{"feed_scope":"selected"}`,
		ArticleCount:   len(candidates),
	}
	runID, err := db.CreateDailyReportRun(run)
	if err != nil {
		t.Fatalf("CreateDailyReportRun: %v", err)
	}
	if _, err := db.CreateDailyReportRun(&models.DailyReportRun{Kind: "backfill", Status: "queued", PeriodStart: periodStart, PeriodEnd: periodEnd}); !errors.Is(err, dbpkg.ErrDailyReportRunExists) {
		t.Fatalf("duplicate automatic period error = %v", err)
	}
	manualID, err := db.CreateDailyReportRun(&models.DailyReportRun{Kind: "manual", Status: "queued", PeriodStart: periodStart, PeriodEnd: periodEnd})
	if err != nil || manualID == 0 {
		t.Fatalf("manual duplicate period: id=%d err=%v", manualID, err)
	}

	referenced := candidates[0]
	articleID := referenced.ArticleID
	sourceFeedID := referenced.FeedID
	if err := db.ReplaceDailyReportSources(runID, []models.DailyReportSource{{
		ArticleID:       &articleID,
		FeedID:          &sourceFeedID,
		ArticleUniqueID: referenced.UniqueID,
		ArticleTitle:    referenced.Title,
		FeedTitle:       referenced.FeedTitle,
		URL:             referenced.URL,
		ContentKind:     "content",
	}}); err != nil {
		t.Fatalf("ReplaceDailyReportSources: %v", err)
	}
	sources, err := db.GetDailyReportSources(runID)
	if err != nil || len(sources) != 1 || sources[0].SourceIndex != 1 {
		t.Fatalf("sources = %+v, err=%v", sources, err)
	}
	autoCandidates, err := db.ListDailyReportCandidates(models.DailyReportCandidateFilter{PeriodStart: periodStart, PeriodEnd: periodEnd})
	if err != nil {
		t.Fatalf("list deduplicated candidates: %v", err)
	}
	for _, candidate := range autoCandidates {
		if candidate.ArticleID == articleID {
			t.Fatalf("previously referenced article %d returned for auto report", articleID)
		}
	}
	manualCandidates, err := db.ListDailyReportCandidates(models.DailyReportCandidateFilter{PeriodStart: periodStart, PeriodEnd: periodEnd, Manual: true})
	if err != nil || len(manualCandidates) != 4 {
		t.Fatalf("manual candidates = %+v, err=%v", manualCandidates, err)
	}

	if unread, err := db.CountUnreadDailyReportRuns(); err != nil || unread != 1 {
		t.Fatalf("unread reports = %d, err=%v", unread, err)
	}
	if err := db.SetDailyReportRunRead(runID, true); err != nil {
		t.Fatalf("SetDailyReportRunRead: %v", err)
	}
	runs, total, err := db.ListDailyReportRuns(models.DailyReportRunFilter{Limit: 20})
	if err != nil || total != 2 || len(runs) != 2 {
		t.Fatalf("runs total=%d list=%d err=%v", total, len(runs), err)
	}
	if _, err := db.CreateDailyReportRun(&models.DailyReportRun{Kind: "manual", Status: "queued", PeriodStart: periodStart, PeriodEnd: periodEnd, ConfigSnapshot: `{"api_key":"secret"}`}); err == nil {
		t.Fatal("expected sensitive config snapshot to be rejected")
	}
	if err := db.MarkRunningDailyReportsInterrupted(periodEnd); err != nil {
		t.Fatalf("MarkRunningDailyReportsInterrupted: %v", err)
	}
	manualRun, err := db.GetDailyReportRun(manualID)
	if err != nil || manualRun == nil || manualRun.Status != "interrupted" {
		t.Fatalf("manual run after interruption = %+v, err=%v", manualRun, err)
	}
	failedStart := periodEnd.Add(24 * time.Hour)
	failedEnd := failedStart.Add(24 * time.Hour)
	if _, err := db.CreateDailyReportRun(&models.DailyReportRun{Kind: "auto", Status: "failed", PeriodStart: failedStart, PeriodEnd: failedEnd}); err != nil {
		t.Fatalf("create failed automatic run: %v", err)
	}
	if has, err := db.HasDailyReportRun(failedStart, failedEnd, "auto"); err != nil || has {
		t.Fatalf("failed run should release period: has=%v err=%v", has, err)
	}
	if _, err := db.CreateDailyReportRun(&models.DailyReportRun{Kind: "backfill", Status: "queued", PeriodStart: failedStart, PeriodEnd: failedEnd}); err != nil {
		t.Fatalf("failed period should be retryable: %v", err)
	}
	interruptedStart := failedEnd
	interruptedEnd := interruptedStart.Add(24 * time.Hour)
	if _, err := db.CreateDailyReportRun(&models.DailyReportRun{Kind: "auto", Status: "interrupted", PeriodStart: interruptedStart, PeriodEnd: interruptedEnd}); err != nil {
		t.Fatalf("create interrupted automatic run: %v", err)
	}
	if has, err := db.HasDailyReportRun(interruptedStart, interruptedEnd, "auto"); err != nil || has {
		t.Fatalf("interrupted run should release period: has=%v err=%v", has, err)
	}
	if _, err := db.CreateDailyReportRun(&models.DailyReportRun{Kind: "backfill", Status: "queued", PeriodStart: interruptedStart, PeriodEnd: interruptedEnd}); err != nil {
		t.Fatalf("interrupted period should be retryable: %v", err)
	}
	if err := db.SetDailyReportLastHandledBoundary(periodEnd); err != nil {
		t.Fatalf("SetDailyReportLastHandledBoundary: %v", err)
	}
	boundary, err := db.GetDailyReportLastHandledBoundary()
	if err != nil || boundary == nil || !boundary.Equal(periodEnd) {
		t.Fatalf("last handled boundary = %v, err=%v", boundary, err)
	}

	if _, err := db.Exec(`DELETE FROM articles WHERE id = ?`, articleID); err != nil {
		t.Fatalf("delete source article: %v", err)
	}
	sources, err = db.GetDailyReportSources(runID)
	if err != nil || len(sources) != 1 || sources[0].ArticleID != nil || sources[0].FeedID == nil {
		t.Fatalf("source snapshot after article delete = %+v, err=%v", sources, err)
	}
	var original *models.Article
	for _, article := range articles {
		if article.Title == referenced.Title {
			copy := *article
			original = &copy
			break
		}
	}
	if original == nil {
		t.Fatalf("original article %q not found", referenced.Title)
	}
	if err := db.SaveArticle(original); err != nil {
		t.Fatalf("reinsert cleaned source article: %v", err)
	}
	autoCandidates, err = db.ListDailyReportCandidates(models.DailyReportCandidateFilter{PeriodStart: periodStart, PeriodEnd: periodEnd})
	if err != nil {
		t.Fatalf("list candidates after source reinsert: %v", err)
	}
	for _, candidate := range autoCandidates {
		if candidate.UniqueID == referenced.UniqueID {
			t.Fatalf("reinserted source %q was duplicated in an automatic report", candidate.UniqueID)
		}
	}
	manualCandidates, err = db.ListDailyReportCandidates(models.DailyReportCandidateFilter{PeriodStart: periodStart, PeriodEnd: periodEnd, Manual: true})
	if err != nil {
		t.Fatalf("list manual candidates after source reinsert: %v", err)
	}
	foundReinserted := false
	for _, candidate := range manualCandidates {
		foundReinserted = foundReinserted || candidate.UniqueID == referenced.UniqueID
	}
	if !foundReinserted {
		t.Fatal("manual report should allow a previously referenced reinserted article")
	}
	if _, err := db.Exec(`DELETE FROM articles WHERE feed_id = ?`, feedID); err != nil {
		t.Fatalf("delete remaining feed articles: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM feeds WHERE id = ?`, feedID); err != nil {
		t.Fatalf("delete source feed: %v", err)
	}
	selected, err = db.GetDailyReportSelectedFeedIDs()
	if err != nil || len(selected) != 0 {
		t.Fatalf("selected feeds after feed delete = %v, err=%v", selected, err)
	}
	sources, err = db.GetDailyReportSources(runID)
	if err != nil || len(sources) != 1 || sources[0].FeedID != nil || sources[0].ArticleTitle == "" {
		t.Fatalf("source snapshot after feed delete = %+v, err=%v", sources, err)
	}
	if err := db.DeleteDailyReportRun(runID); err != nil {
		t.Fatalf("DeleteDailyReportRun: %v", err)
	}
	sources, err = db.GetDailyReportSources(runID)
	if err != nil || len(sources) != 0 {
		t.Fatalf("sources after run delete = %+v, err=%v", sources, err)
	}
}

func TestMarkReadAndReadLaterAndFavorites(t *testing.T) {
	db := setupDBWithFeed(t)

	// Get feed id
	var feedID int64
	_ = db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID)

	// Insert article
	res, err := db.Exec(`INSERT INTO articles (feed_id, title, url, published_at, is_read, is_favorite, is_read_later) VALUES (?, ?, ?, ?, 0, 0, 0)`, feedID, "A", "u1", time.Now())
	if err != nil {
		t.Fatalf("insert article: %v", err)
	}
	id, _ := res.LastInsertId()

	// Mark read
	if err := db.MarkArticleRead(id, true); err != nil {
		t.Fatalf("MarkArticleRead error: %v", err)
	}

	// Should be marked read and not read later
	var isRead, isReadLater int
	_ = db.QueryRow("SELECT is_read, is_read_later FROM articles WHERE id = ?", id).Scan(&isRead, &isReadLater)
	if isRead != 1 || isReadLater != 0 {
		t.Fatalf("unexpected read/readlater state: %d/%d", isRead, isReadLater)
	}

	// Toggle favorite
	if err := db.ToggleFavorite(id); err != nil {
		t.Fatalf("ToggleFavorite error: %v", err)
	}
	var isFav int
	_ = db.QueryRow("SELECT is_favorite FROM articles WHERE id = ?", id).Scan(&isFav)
	if isFav != 1 {
		t.Fatalf("expected favorite set, got %d", isFav)
	}

	// Toggle read later (will unset since currently 0 -> toggled to 0? ensure it works)
	if err := db.ToggleReadLater(id); err != nil {
		t.Fatalf("ToggleReadLater error: %v", err)
	}
}

func TestUnreadCountsAndMarkAll(t *testing.T) {
	db := setupDBWithFeed(t)

	// Insert feed id
	var feedID int64
	_ = db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID)

	// Insert multiple articles
	for i := 0; i < 5; i++ {
		_, err := db.Exec(`INSERT INTO articles (feed_id, title, url, published_at, is_read, is_hidden) VALUES (?, ?, ?, ?, 0, 0)`, feedID, fmt.Sprintf("t%d", i), fmt.Sprintf("u%d", i), time.Now())
		if err != nil {
			t.Fatalf("insert article: %v", err)
		}
	}

	total, err := db.GetTotalUnreadCount()
	if err != nil {
		t.Fatalf("GetTotalUnreadCount error: %v", err)
	}
	if total < 5 {
		t.Fatalf("expected at least 5 unread, got %d", total)
	}

	byFeed, err := db.GetUnreadCountByFeed(feedID)
	if err != nil {
		t.Fatalf("GetUnreadCountByFeed error: %v", err)
	}
	if byFeed < 1 {
		t.Fatalf("expected unread for feed, got %d", byFeed)
	}

	counts, err := db.GetUnreadCountsForAllFeeds()
	if err != nil {
		t.Fatalf("GetUnreadCountsForAllFeeds error: %v", err)
	}
	if counts[feedID] < 1 {
		t.Fatalf("expected counts map to include feed %d", feedID)
	}

	// Mark all as read for feed
	if err := db.MarkAllAsReadForFeed(feedID); err != nil {
		t.Fatalf("MarkAllAsReadForFeed error: %v", err)
	}
	totalAfter, _ := db.GetTotalUnreadCount()
	if totalAfter != 0 {
		t.Fatalf("expected 0 unread after marking all read, got %d", totalAfter)
	}
}

func TestCleanupOldAndUnimportantAndDBSize(t *testing.T) {
	db := setupDBWithFeed(t)

	// Insert old article (older than default 30 days)
	oldTime := time.Now().AddDate(0, 0, -100)
	var feedID int64
	_ = db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID)

	_, err := db.Exec(`INSERT INTO articles (feed_id, title, url, published_at, is_favorite, is_read_later) VALUES (?, ?, ?, ?, 0, 0)`, feedID, "old", "oldurl", oldTime)
	if err != nil {
		t.Fatalf("insert old article: %v", err)
	}

	// Insert unimportant article (unread, not favorite/readlater)
	_, err = db.Exec(`INSERT INTO articles (feed_id, title, url, published_at, is_read, is_favorite, is_read_later) VALUES (?, ?, ?, ?, 0, 0, 0)`, feedID, "tmp", "u2", time.Now())
	if err != nil {
		t.Fatalf("insert tmp article: %v", err)
	}

	// Cleanup old articles
	deleted, err := db.CleanupOldArticles()
	if err != nil {
		t.Fatalf("CleanupOldArticles error: %v", err)
	}
	if deleted < 1 {
		t.Fatalf("expected at least 1 deleted old article, got %d", deleted)
	}

	// Cleanup unimportant
	del2, err := db.CleanupUnimportantArticles()
	if err != nil {
		t.Fatalf("CleanupUnimportantArticles error: %v", err)
	}
	if del2 < 0 {
		t.Fatalf("unexpected deleted count: %d", del2)
	}

	// DB size
	sz, err := db.GetDatabaseSizeMB()
	if err != nil {
		t.Fatalf("GetDatabaseSizeMB error: %v", err)
	}
	if sz < 0 {
		t.Fatalf("unexpected db size: %f", sz)
	}
}

func TestSaveArticlesBatchContextCancel(t *testing.T) {
	db := setupDBWithFeed(t)

	// Prepare articles
	// determine feed id
	var feedID2 int64
	_ = db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID2)

	articles := []*models.Article{}
	for i := 0; i < 10; i++ {
		articles = append(articles, &models.Article{FeedID: feedID2, Title: "b", URL: "u" + string(rune(i))})
	}

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := db.SaveArticles(ctx, articles); err == nil {
		t.Fatalf("expected error due to canceled context")
	}
}

func TestSaveArticlesRollsBackEntireBatchOnPermanentError(t *testing.T) {
	db := setupDBWithFeed(t)

	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	articles := []*models.Article{
		{
			FeedID:                feedID,
			Title:                 "valid article must be rolled back",
			URL:                   "https://example.com/atomic-valid",
			PublishedAt:           time.Now().UTC(),
			HasValidPublishedTime: true,
		},
		{
			FeedID:                feedID + 10000,
			Title:                 "invalid feed",
			URL:                   "https://example.com/atomic-invalid",
			PublishedAt:           time.Now().UTC(),
			HasValidPublishedTime: true,
		},
	}

	err := db.SaveArticles(context.Background(), articles)
	if err == nil {
		t.Fatal("expected the invalid foreign key to fail the batch")
	}
	if !strings.Contains(err.Error(), "article 2/2") || !strings.Contains(err.Error(), "attempt(s)") {
		t.Fatalf("expected batch and attempt context, got %v", err)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles WHERE url IN (?, ?)`, articles[0].URL, articles[1].URL).Scan(&count); err != nil {
		t.Fatalf("count batch articles: %v", err)
	}
	if count != 0 {
		t.Fatalf("batch committed partially: got %d stored articles, want 0", count)
	}
}

func TestSaveArticlesRetriesSQLiteWriteContention(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "write-contention.db")
	db, err := dbpkg.NewDB(databasePath)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if _, err := db.Exec(`PRAGMA busy_timeout = 1`); err != nil {
		t.Fatalf("set short busy timeout: %v", err)
	}
	result, err := db.Exec(`INSERT INTO feeds (title, url, category) VALUES (?, ?, ?)`, "Locked Feed", "https://example.com/locked-feed", "news")
	if err != nil {
		t.Fatalf("insert feed: %v", err)
	}
	feedID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("feed id: %v", err)
	}

	locker, err := dbpkg.NewDB(databasePath)
	if err != nil {
		t.Fatalf("NewDB locker: %v", err)
	}
	t.Cleanup(func() { _ = locker.Close() })
	lockTx, err := locker.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	if _, err := lockTx.Exec(`UPDATE settings SET value = value WHERE key = 'theme'`); err != nil {
		t.Fatalf("acquire write lock: %v", err)
	}

	previousLogWriter := log.Writer()
	var retryLog strings.Builder
	log.SetOutput(&retryLog)
	t.Cleanup(func() { log.SetOutput(previousLogWriter) })

	releaseDone := make(chan error, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		releaseDone <- lockTx.Rollback()
	}()

	article := &models.Article{
		FeedID:                feedID,
		Title:                 "saved after lock retry",
		URL:                   "https://example.com/saved-after-lock-retry",
		PublishedAt:           time.Now().UTC(),
		HasValidPublishedTime: true,
	}
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("SaveArticles after temporary lock: %v", err)
	}
	if err := <-releaseDone; err != nil && !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("release lock: %v", err)
	}
	if !strings.Contains(retryLog.String(), "retrying") {
		t.Fatalf("expected a contention retry, logs: %s", retryLog.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM articles WHERE url = ?`, article.URL).Scan(&count); err != nil {
		t.Fatalf("count retried article: %v", err)
	}
	if count != 1 {
		t.Fatalf("retried article count = %d, want 1", count)
	}
}

func TestSaveArticlesUpdatePreservesRelatedData(t *testing.T) {
	db := setupDBWithFeed(t)

	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	publishedAt := time.Date(2026, time.July, 13, 12, 0, 0, 0, time.UTC)
	article := &models.Article{
		FeedID:                feedID,
		Title:                 "Article with related data",
		URL:                   "https://example.com/article/original",
		PublishedAt:           publishedAt,
		HasValidPublishedTime: true,
	}
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("initial SaveArticles error: %v", err)
	}

	var articleID int64
	if err := db.QueryRow(`SELECT id FROM articles WHERE feed_id = ?`, feedID).Scan(&articleID); err != nil {
		t.Fatalf("scan article id: %v", err)
	}
	if err := db.SetArticleContent(articleID, "cached content"); err != nil {
		t.Fatalf("SetArticleContent error: %v", err)
	}
	sessionID, err := db.CreateChatSession(articleID, "Existing chat")
	if err != nil {
		t.Fatalf("CreateChatSession error: %v", err)
	}
	if _, err := db.CreateChatMessage(sessionID, "user", "Keep this message", ""); err != nil {
		t.Fatalf("CreateChatMessage error: %v", err)
	}

	article.URL = "https://example.com/article/updated"
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("update SaveArticles error: %v", err)
	}

	var updatedArticleID int64
	var updatedURL string
	if err := db.QueryRow(`SELECT id, url FROM articles WHERE feed_id = ?`, feedID).Scan(&updatedArticleID, &updatedURL); err != nil {
		t.Fatalf("scan updated article: %v", err)
	}
	if updatedArticleID != articleID {
		t.Fatalf("article id changed from %d to %d", articleID, updatedArticleID)
	}
	if updatedURL != article.URL {
		t.Fatalf("article URL = %q, want %q", updatedURL, article.URL)
	}

	content, found, err := db.GetArticleContent(articleID)
	if err != nil {
		t.Fatalf("GetArticleContent error: %v", err)
	}
	if !found || content != "cached content" {
		t.Fatalf("article content was not preserved: found=%v content=%q", found, content)
	}

	session, err := db.GetChatSession(sessionID)
	if err != nil {
		t.Fatalf("GetChatSession error: %v", err)
	}
	if session == nil || session.MessageCount != 1 {
		t.Fatalf("chat data was not preserved: session=%+v", session)
	}

	newerSessionID, err := db.CreateChatSession(articleID, "New conversation")
	if err != nil {
		t.Fatalf("CreateChatSession(newer) error: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE chat_sessions SET updated_at = '2026-08-25 10:00:00' WHERE id IN (?, ?)`,
		sessionID, newerSessionID,
	); err != nil {
		t.Fatalf("align chat session timestamps: %v", err)
	}
	firstMessageID, err := db.CreateChatMessage(newerSessionID, "user", "First in the same second", "")
	if err != nil {
		t.Fatalf("CreateChatMessage(first) error: %v", err)
	}
	secondMessageID, err := db.CreateChatMessage(newerSessionID, "assistant", "Second in the same second", "thinking")
	if err != nil {
		t.Fatalf("CreateChatMessage(second) error: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE chat_sessions SET updated_at = '2026-08-25 10:00:00' WHERE id IN (?, ?)`,
		sessionID, newerSessionID,
	); err != nil {
		t.Fatalf("restore aligned chat session timestamps: %v", err)
	}
	sessions, err := db.GetChatSessionsByArticle(articleID)
	if err != nil || len(sessions) != 2 || sessions[0].ID != newerSessionID {
		t.Fatalf("chat session order = %+v, err=%v", sessions, err)
	}
	messages, err := db.GetChatMessages(newerSessionID)
	if err != nil || len(messages) != 2 || messages[0].ID != firstMessageID || messages[1].ID != secondMessageID {
		t.Fatalf("chat message order = %+v, err=%v", messages, err)
	}
}

func TestArticleDeduplicationByUniqueID(t *testing.T) {
	db := setupDBWithFeed(t)

	// Get feed id
	var feedID int64
	row := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed")
	if err := row.Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	publishedAt := time.Now()

	// Save same article multiple times with different URLs (should be deduplicated by unique_id)
	article1 := &models.Article{
		FeedID:      feedID,
		Title:       "Test Article",
		URL:         "https://example.com/article/1",
		PublishedAt: publishedAt,
	}

	article2 := &models.Article{
		FeedID:      feedID,
		Title:       "Test Article",                                  // Same title
		URL:         "https://example.com/article/1?utm_source=test", // Different URL
		PublishedAt: publishedAt,                                     // Same time
	}

	// Save first article
	if err := db.SaveArticle(article1); err != nil {
		t.Fatalf("SaveArticle error: %v", err)
	}

	// Try to save the same article again (should be ignored due to unique_id)
	if err := db.SaveArticle(article2); err != nil {
		t.Fatalf("SaveArticle error: %v", err)
	}

	// Verify only one article exists
	articles, err := db.GetArticles("all", feedID, "", false, 10, 0)
	if err != nil {
		t.Fatalf("GetArticles error: %v", err)
	}

	if len(articles) != 1 {
		t.Fatalf("expected 1 article after deduplication, got %d", len(articles))
	}

	// Verify the article has the correct unique_id
	if articles[0].Title != "Test Article" {
		t.Fatalf("expected title 'Test Article', got '%s'", articles[0].Title)
	}
}

func TestArticleDifferentTitlesNotDeduplicated(t *testing.T) {
	db := setupDBWithFeed(t)

	// Get feed id
	var feedID int64
	row := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed")
	if err := row.Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	publishedAt := time.Now()

	// Save different articles with same feed and time (should NOT be deduplicated)
	article1 := &models.Article{
		FeedID:      feedID,
		Title:       "Article One",
		URL:         "https://example.com/article/1",
		PublishedAt: publishedAt,
	}

	article2 := &models.Article{
		FeedID:      feedID,
		Title:       "Article Two", // Different title
		URL:         "https://example.com/article/2",
		PublishedAt: publishedAt, // Same time
	}

	// Save both articles
	if err := db.SaveArticle(article1); err != nil {
		t.Fatalf("SaveArticle error: %v", err)
	}

	if err := db.SaveArticle(article2); err != nil {
		t.Fatalf("SaveArticle error: %v", err)
	}

	// Verify both articles exist
	articles, err := db.GetArticles("all", feedID, "", false, 10, 0)
	if err != nil {
		t.Fatalf("GetArticles error: %v", err)
	}

	if len(articles) != 2 {
		t.Fatalf("expected 2 articles with different titles, got %d", len(articles))
	}
}

func TestSaveArticlesKeepsRepeatedTitlesWithDifferentValidDates(t *testing.T) {
	db := setupDBWithFeed(t)
	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}
	firstPublished := time.Date(2026, time.August, 18, 8, 0, 0, 0, time.UTC)
	articles := []*models.Article{
		{FeedID: feedID, Title: "Daily bulletin", URL: "https://example.com/18", PublishedAt: firstPublished, HasValidPublishedTime: true},
		{FeedID: feedID, Title: "Daily bulletin", URL: "https://example.com/19", PublishedAt: firstPublished.Add(24 * time.Hour), HasValidPublishedTime: true},
	}
	if err := db.SaveArticles(context.Background(), articles); err != nil {
		t.Fatalf("SaveArticles failed: %v", err)
	}
	stored, err := db.GetArticles("all", feedID, "", false, 10, 0)
	if err != nil {
		t.Fatalf("GetArticles failed: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("same-title entries on different valid dates were merged: %+v", stored)
	}
}

// TestSaveArticlesNoPubDateKeepsFirstSeenTime verifies that articles without a
// valid pubDate keep their originally stored published_at on refresh when the
// article is unchanged, instead of being bumped to the current time every save.
func TestSaveArticlesNoPubDateKeepsFirstSeenTime(t *testing.T) {
	db := setupDBWithFeed(t)

	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	firstSeen := time.Now()
	article := &models.Article{
		FeedID:                feedID,
		Title:                 "No pubDate article",
		URL:                   "https://example.com/no-pubdate",
		PublishedAt:           firstSeen,
		HasValidPublishedTime: false, // feed item had no pubDate
	}

	// First save: article is inserted with the first-seen time
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("initial SaveArticles error: %v", err)
	}

	// Simulate the next feed refresh: processArticles stamps a NEW time.Now()
	// but the article content is unchanged.
	later := firstSeen.Add(2 * time.Hour)
	article.PublishedAt = later
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("refresh SaveArticles error: %v", err)
	}

	// Only one article must exist (dedup by title+feedID, no date part)
	articles, err := db.GetArticles("all", feedID, "", false, 10, 0)
	if err != nil {
		t.Fatalf("GetArticles error: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}

	stored := articles[0].PublishedAt
	// Must stay at the first-seen time, NOT be bumped to the refresh time
	if diff := stored.Sub(firstSeen); diff < 0 || diff > time.Minute {
		t.Fatalf("published_at bumped to refresh time: got %v, want ≈ %v (diff %v)", stored, firstSeen, diff)
	}
}

// TestSaveArticlesNoPubDateBumpsOnContentChange verifies that an article without
// a valid pubDate DOES move to the new time when its content actually changed
// (e.g. the source entry was edited).
func TestSaveArticlesNoPubDateBumpsOnContentChange(t *testing.T) {
	db := setupDBWithFeed(t)

	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	firstSeen := time.Now()
	article := &models.Article{
		FeedID:                feedID,
		Title:                 "Edited no-pubDate article",
		URL:                   "https://example.com/edited/1",
		PublishedAt:           firstSeen,
		OriginalSummary:       "old summary",
		HasValidPublishedTime: false,
	}

	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("initial SaveArticles error: %v", err)
	}

	// Next refresh: content changed (summary updated) and processArticles stamped
	// a new time — published_at should move to the new time.
	editedAt := firstSeen.Add(2 * time.Hour)
	article.PublishedAt = editedAt
	article.OriginalSummary = "updated summary"
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("refresh SaveArticles error: %v", err)
	}

	articles, err := db.GetArticles("all", feedID, "", false, 10, 0)
	if err != nil {
		t.Fatalf("GetArticles error: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}

	stored := articles[0].PublishedAt
	if diff := stored.Sub(editedAt); diff < 0 || diff > time.Minute {
		t.Fatalf("changed article time not bumped: got %v, want ≈ %v (diff %v)", stored, editedAt, diff)
	}
}

// TestSaveArticlesValidPubDateStillUpdatesTime guards the CASE expression:
// articles WITH a valid pubDate must still get their real published time on refresh.
func TestSaveArticlesValidPubDateStillUpdatesTime(t *testing.T) {
	db := setupDBWithFeed(t)

	var feedID int64
	if err := db.QueryRow(`SELECT id FROM feeds WHERE url = ?`, "https://example.com/feed").Scan(&feedID); err != nil {
		t.Fatalf("scan feed id: %v", err)
	}

	// Same calendar date so the unique_id (title+feedID+date) matches and the
	// second save hits the ON CONFLICT update path.
	t1 := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 13, 20, 0, 0, 0, time.UTC)
	article := &models.Article{
		FeedID:                feedID,
		Title:                 "With pubDate article",
		URL:                   "https://example.com/with-pubdate",
		PublishedAt:           t1,
		HasValidPublishedTime: true,
	}

	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("initial SaveArticles error: %v", err)
	}

	// Refresh with a new (valid) published time on the same day
	article.PublishedAt = t2
	if err := db.SaveArticles(context.Background(), []*models.Article{article}); err != nil {
		t.Fatalf("refresh SaveArticles error: %v", err)
	}

	articles, err := db.GetArticles("all", feedID, "", false, 10, 0)
	if err != nil {
		t.Fatalf("GetArticles error: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}

	stored := articles[0].PublishedAt
	if diff := stored.Sub(t2); diff < 0 || diff > time.Minute {
		t.Fatalf("valid pubDate not honored on refresh: got %v, want %v (diff %v)", stored, t2, diff)
	}
}
