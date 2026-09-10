package database

import (
	"context"
	"reflect"
	"testing"
	"time"

	"MRSS/internal/models"
)

func TestMarkOldUnreadArticlesReadPreservesProtectedArticles(t *testing.T) {
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Init(); err != nil {
		t.Fatal(err)
	}

	feedID, err := db.AddFeed(&models.Feed{Title: "Feed", URL: "https://example.com/feed"})
	if err != nil {
		t.Fatal(err)
	}
	old := time.Now().AddDate(0, 0, -10)
	articles := []*models.Article{
		{FeedID: feedID, Title: "old", URL: "https://example.com/old", PublishedAt: old},
		{FeedID: feedID, Title: "favorite", URL: "https://example.com/favorite", PublishedAt: old, IsFavorite: true},
		{FeedID: feedID, Title: "later", URL: "https://example.com/later", PublishedAt: old, IsReadLater: true},
		{FeedID: feedID, Title: "hidden", URL: "https://example.com/hidden", PublishedAt: old, IsHidden: true},
		{FeedID: feedID, Title: "new", URL: "https://example.com/new", PublishedAt: time.Now()},
	}
	if err := db.SaveArticles(context.Background(), articles); err != nil {
		t.Fatal(err)
	}
	filter := models.DailyReportCandidateFilter{
		PeriodStart:   time.Now().Add(-time.Hour),
		PeriodEnd:     time.Now().Add(time.Hour),
		IncludeHidden: true,
	}
	before, err := db.ListDailyReportCandidates(filter)
	if err != nil || len(before) != len(articles) {
		t.Fatalf("daily report candidates before cleanup: count=%d err=%v", len(before), err)
	}

	count, err := db.MarkOldUnreadArticlesRead(time.Now().AddDate(0, 0, -7))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("marked %d articles, want 1", count)
	}

	got, err := db.GetArticles("all", 0, "", true, 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]bool, len(got))
	for _, article := range got {
		states[article.Title] = article.IsRead
		if article.FirstSeenAt.IsZero() {
			t.Fatalf("first_seen_at lost in article query: %s", article.Title)
		}
	}
	if !states["old"] || states["favorite"] || states["later"] || states["hidden"] || states["new"] {
		t.Fatalf("unexpected read states: %#v", states)
	}
	after, err := db.ListDailyReportCandidates(filter)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("automatic read marking changed daily report candidates: before=%+v after=%+v err=%v", before, after, err)
	}
}
