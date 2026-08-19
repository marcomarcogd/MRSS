package dailyreport

import (
	"context"
	"fmt"
	"sync"
	"time"

	"MRSS/internal/database"
	"MRSS/internal/feed"
	"MRSS/internal/freshrss"
	"MRSS/internal/models"
)

// FeedRefresher reuses the existing task manager for normal feeds and the
// FreshRSS sync service for remote streams. The caller owns the three-minute
// timeout and receives partial failures per feed.
type FeedRefresher struct {
	db      *database.DB
	fetcher *feed.Fetcher
}

func NewFeedRefresher(db *database.DB, fetcher *feed.Fetcher) *FeedRefresher {
	return &FeedRefresher{db: db, fetcher: fetcher}
}

func (r *FeedRefresher) Refresh(ctx context.Context, feedIDs []int64) []RefreshResult {
	if len(feedIDs) == 0 || r.fetcher == nil {
		return nil
	}
	allFeeds, err := r.db.GetFeeds()
	if err != nil {
		return []RefreshResult{{Error: err.Error()}}
	}
	wanted := make(map[int64]struct{}, len(feedIDs))
	for _, id := range feedIDs {
		wanted[id] = struct{}{}
	}
	normalFeeds := make([]models.Feed, 0, len(feedIDs))
	freshFeeds := make([]models.Feed, 0)
	found := make(map[int64]struct{}, len(feedIDs))
	for _, item := range allFeeds {
		if _, ok := wanted[item.ID]; !ok {
			continue
		}
		found[item.ID] = struct{}{}
		if item.IsFreshRSSSource {
			freshFeeds = append(freshFeeds, item)
		} else {
			normalFeeds = append(normalFeeds, item)
		}
	}
	results := make([]RefreshResult, 0)
	for _, id := range feedIDs {
		if _, ok := found[id]; !ok {
			results = append(results, RefreshResult{FeedID: id, Error: "feed not found"})
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	if len(normalFeeds) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.fetcher.GetTaskManager().AddGlobalRefresh(ctx, normalFeeds)
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()
			for {
				if !r.fetcher.GetTaskManager().IsRunning() {
					progress := r.fetcher.GetTaskManager().GetProgress()
					mu.Lock()
					for _, item := range normalFeeds {
						if message := progress.Errors[item.ID]; message != "" {
							results = append(results, RefreshResult{FeedID: item.ID, Error: message})
						}
					}
					mu.Unlock()
					return
				}
				select {
				case <-ctx.Done():
					mu.Lock()
					for _, item := range normalFeeds {
						results = append(results, RefreshResult{FeedID: item.ID, Error: ctx.Err().Error()})
					}
					mu.Unlock()
					return
				case <-ticker.C:
				}
			}
		}()
	}

	if len(freshFeeds) > 0 {
		serverURL, _ := r.db.GetSetting("freshrss_server_url")
		username, _ := r.db.GetSetting("freshrss_username")
		password, _ := r.db.GetEncryptedSetting("freshrss_api_password")
		if serverURL == "" || username == "" || password == "" {
			for _, item := range freshFeeds {
				results = append(results, RefreshResult{FeedID: item.ID, Error: "FreshRSS settings incomplete"})
			}
		} else {
			for _, item := range freshFeeds {
				item := item
				wg.Add(1)
				go func() {
					defer wg.Done()
					if item.FreshRSSStreamID == "" {
						mu.Lock()
						results = append(results, RefreshResult{FeedID: item.ID, Error: "FreshRSS stream ID is missing"})
						mu.Unlock()
						return
					}
					syncService := freshrss.NewBidirectionalSyncService(serverURL, username, password, r.db)
					if _, err := syncService.SyncFeed(ctx, item.FreshRSSStreamID); err != nil {
						mu.Lock()
						results = append(results, RefreshResult{FeedID: item.ID, Error: fmt.Sprintf("FreshRSS sync failed: %v", err)})
						mu.Unlock()
					}
				}()
			}
		}
	}
	wg.Wait()
	return results
}
