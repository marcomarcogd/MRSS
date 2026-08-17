package discovery

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/PuerkitoBio/goquery"
)

// DiscoverFromFeed discovers blogs from a feed's homepage
func (s *Service) DiscoverFromFeed(ctx context.Context, feedURL string) ([]DiscoveredBlog, error) {
	return s.DiscoverFromFeedWithProgress(ctx, feedURL, nil)
}

// DiscoverFromFeedWithProgress discovers blogs from a feed's homepage with progress updates
func (s *Service) DiscoverFromFeedWithProgress(ctx context.Context, feedURL string, progressCb ProgressCallback) ([]DiscoveredBlog, error) {
	// Report progress: fetching homepage
	if progressCb != nil {
		progressCb(Progress{
			Stage:   "fetching_homepage",
			Message: "Fetching homepage from feed",
			Detail:  feedURL,
		})
	}

	// First, try to parse the feed to get the homepage link
	homepage, err := s.getFeedHomepage(ctx, feedURL)
	if err != nil {
		return nil, fmt.Errorf("failed to get homepage from feed: %w", err)
	}

	// Report progress: finding friend links
	if progressCb != nil {
		progressCb(Progress{
			Stage:   "finding_friend_links",
			Message: "Searching for friend links",
			Detail:  homepage,
		})
	}

	// Fetch the homepage HTML
	friendLinks, err := s.findFriendLinksWithProgress(ctx, homepage, progressCb)
	if err != nil {
		return nil, fmt.Errorf("failed to find friend links: %w", err)
	}

	if len(friendLinks) == 0 {
		return []DiscoveredBlog{}, nil
	}

	// Report progress: checking RSS feeds
	if progressCb != nil {
		progressCb(Progress{
			Stage:   "checking_rss",
			Message: "Checking RSS feeds",
			Total:   len(friendLinks),
		})
	}

	// Discover RSS feeds from friend links (concurrent)
	discovered := s.discoverRSSFeedsWithProgress(ctx, friendLinks, progressCb)

	return discovered, nil
}

// getFeedHomepage extracts the homepage URL from a feed
func (s *Service) getFeedHomepage(ctx context.Context, feedURL string) (string, error) {
	feed, err := s.feedParser.ParseURLWithContext(feedURL, ctx)
	if err != nil {
		return "", err
	}

	if feed.Link != "" {
		return feed.Link, nil
	}

	// Fallback: try to extract base URL from feed URL
	u, err := url.Parse(feedURL)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%s://%s", u.Scheme, u.Host), nil
}

// fetchHTML fetches and parses HTML from a URL
func (s *Service) fetchHTML(ctx context.Context, urlStr string) (*goquery.Document, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "MRSS (Blog Discovery Bot)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error: %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	return doc, nil
}

// helper to suppress unused import warning
var _ = log.Println
