package models

import "time"

type Feed struct {
	ID                 int64     `json:"id"`
	Title              string    `json:"title"`
	URL                string    `json:"url"`
	Link               string    `json:"link"` // Website homepage link
	Description        string    `json:"description"`
	Category           string    `json:"category"`
	ImageURL           string    `json:"image_url"` // New field
	Position           int       `json:"position"`  // Position within category for custom ordering
	LastUpdated        time.Time `json:"last_updated"`
	LastError          string    `json:"last_error,omitempty"`  // Track last fetch error
	DiscoveryCompleted bool      `json:"discovery_completed"`   // Track if discovery has been run
	ScriptPath         string    `json:"script_path,omitempty"` // Path to custom script for fetching feed
	HideFromTimeline   bool      `json:"hide_from_timeline"`    // Hide articles from timeline views
	ProxyURL           string    `json:"proxy_url,omitempty"`   // Custom proxy URL for this feed (overrides global)
	ProxyEnabled       bool      `json:"proxy_enabled"`         // Whether to use proxy for this feed
	RefreshInterval    int       `json:"refresh_interval"`      // Custom refresh interval in minutes (0 = use global, -1 = intelligent, -2 = never, >0 = custom minutes)
	IsImageMode        bool      `json:"is_image_mode"`         // Whether this feed is for image gallery mode
	// XPath support for HTML/XML scraping
	Type                string `json:"type"`                   // "HTML+XPath" or "XML+XPath"
	XPathItem           string `json:"xpath_item"`             // XPath to extract feed items
	XPathItemTitle      string `json:"xpath_item_title"`       // XPath to extract item title
	XPathItemContent    string `json:"xpath_item_content"`     // XPath to extract item content
	XPathItemUri        string `json:"xpath_item_uri"`         // XPath to extract item URI
	XPathItemAuthor     string `json:"xpath_item_author"`      // XPath to extract item author
	XPathItemTimestamp  string `json:"xpath_item_timestamp"`   // XPath to extract item timestamp
	XPathItemTimeFormat string `json:"xpath_item_time_format"` // Time format for parsing timestamp
	XPathItemThumbnail  string `json:"xpath_item_thumbnail"`   // XPath to extract item thumbnail
	XPathItemCategories string `json:"xpath_item_categories"`  // XPath to extract item categories
	XPathItemUid        string `json:"xpath_item_uid"`         // XPath to extract item unique ID
	ArticleViewMode     string `json:"article_view_mode"`      // Article view mode override ('global', 'webpage', 'rendered')
	AutoExpandContent   string `json:"auto_expand_content"`    // Auto expand content mode ('global', 'enabled', 'disabled')
	// Email/Newsletter support
	EmailAddress    string `json:"email_address,omitempty"`     // Email address for newsletter subscriptions
	EmailIMAPServer string `json:"email_imap_server,omitempty"` // IMAP server address
	EmailIMAPPort   int    `json:"email_imap_port"`             // IMAP server port (default 993)
	EmailUsername   string `json:"email_username,omitempty"`    // IMAP username
	EmailPassword   string `json:"email_password,omitempty"`    // IMAP password (encrypted)
	EmailFolder     string `json:"email_folder"`                // IMAP folder to monitor (default INBOX)
	EmailLastUID    int    `json:"email_last_uid"`              // Last processed email UID for incremental updates
	// FreshRSS integration
	IsFreshRSSSource bool   `json:"is_freshrss_source"` // Whether this feed is from FreshRSS sync
	FreshRSSStreamID string `json:"freshrss_stream_id"` // FreshRSS stream ID (e.g., "feed/http://...")
	// Statistics
	LatestArticleTime *time.Time `json:"latest_article_time,omitempty"` // Latest article publish time
	ArticlesPerMonth  float64    `json:"articles_per_month,omitempty"`  // Average articles per month (last 90 days / 3)
	LastUpdateStatus  string     `json:"last_update_status,omitempty"`  // Last update status ("success" or "failed")
	// Tags (populated by API handlers)
	Tags []Tag `json:"tags,omitempty"` // Tags assigned to this feed
}

type Article struct {
	ID                    int64     `json:"id"`
	FeedID                int64     `json:"feed_id"`
	Title                 string    `json:"title"`
	URL                   string    `json:"url"`
	ImageURL              string    `json:"image_url"`
	AudioURL              string    `json:"audio_url"`
	VideoURL              string    `json:"video_url"` // YouTube video URL for embedded player
	PublishedAt           time.Time `json:"published_at"`
	FirstSeenAt           time.Time `json:"first_seen_at"`
	HasValidPublishedTime bool      `json:"-"` // Internal field, not serialized
	IsRead                bool      `json:"is_read"`
	IsFavorite            bool      `json:"is_favorite"`
	IsHidden              bool      `json:"is_hidden"`
	IsReadLater           bool      `json:"is_read_later"`
	FeedTitle             string    `json:"feed_title,omitempty"` // Joined field
	Author                string    `json:"author,omitempty"`     // Article author
	TranslatedTitle       string    `json:"translated_title"`
	Summary               string    `json:"summary"`        // Cached AI-generated summary
	SummarySource         string    `json:"summary_source"` // ai_manual, ai_daily_report, local_manual, or legacy/unknown
	SummaryFingerprint    string    `json:"summary_fingerprint"`
	SummaryContentHash    string    `json:"summary_content_hash"`
	OriginalSummary       string    `json:"original_summary"` // Summary/description provided by the RSS item
	UniqueID              string    `json:"unique_id"`        // Unique identifier for deduplication (title+feed_id+published_date)
	FreshRSSItemID        string    `json:"freshrss_item_id"` // FreshRSS/Google Reader item ID for API operations
}

// DailyReportConfig stores the singleton configuration for scheduled reports.
// AI credentials and custom headers are deliberately not copied into this
// structure so it is safe to serialize as a report configuration snapshot.
type DailyReportConfig struct {
	ID                      int64      `json:"id"`
	Enabled                 bool       `json:"enabled"`
	ScheduleTime            string     `json:"schedule_time"`
	FeedScope               string     `json:"feed_scope"`
	IncludeHidden           bool       `json:"include_hidden"`
	AIProfileID             *int64     `json:"ai_profile_id"`
	ArticleSummaryMode      string     `json:"article_summary_mode"`
	Focus                   string     `json:"focus"`
	OutlineJSON             string     `json:"outline_json"`
	Language                string     `json:"language"`
	TitleTemplate           string     `json:"title_template"`
	InAppNotification       bool       `json:"in_app_notification"`
	SystemNotification      bool       `json:"system_notification"`
	NotifyOnEmpty           bool       `json:"notify_on_empty"`
	LastHandledBoundary     *time.Time `json:"last_handled_boundary"`
	CloudConsentVersion     int        `json:"-"`
	CloudConsentAt          *time.Time `json:"-"`
	CloudConsentFingerprint string     `json:"-"`
	CreatedAt               time.Time  `json:"created_at"`
	UpdatedAt               time.Time  `json:"updated_at"`
	FeedIDs                 []int64    `json:"feed_ids,omitempty"`
}

// DailyReportCandidateFilter defines the article window used for a report.
type DailyReportCandidateFilter struct {
	PeriodStart       time.Time
	PeriodEnd         time.Time
	FeedIDs           []int64
	ExcludeArticleIDs []int64
	Manual            bool
	IncludeHidden     bool
}

// DailyReportCandidate contains the local article data available to the report
// generator. Content comes only from MRSS caches; no remote page is fetched.
type DailyReportCandidate struct {
	ArticleID             int64     `json:"article_id"`
	FeedID                int64     `json:"feed_id"`
	Title                 string    `json:"title"`
	Author                string    `json:"author"`
	URL                   string    `json:"url"`
	FeedTitle             string    `json:"feed_title"`
	OriginalSummary       string    `json:"original_summary"`
	GeneratedSummary      string    `json:"generated_summary"`
	SummarySource         string    `json:"summary_source"`
	SummaryFingerprint    string    `json:"summary_fingerprint"`
	SummaryContentHash    string    `json:"summary_content_hash"`
	Content               string    `json:"content"`
	UniqueID              string    `json:"unique_id"`
	PublishedAt           time.Time `json:"published_at"`
	FirstSeenAt           time.Time `json:"first_seen_at"`
	HasValidPublishedTime bool      `json:"has_valid_published_time"`
	LateArrival           bool      `json:"late_arrival"`
}

// DailyReportRun is one scheduled, backfilled, or manually requested report.
type DailyReportRun struct {
	ID             int64      `json:"id"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	PeriodStart    time.Time  `json:"period_start"`
	PeriodEnd      time.Time  `json:"period_end"`
	Progress       int        `json:"progress"`
	CurrentStep    string     `json:"current_step"`
	Title          string     `json:"title"`
	ContentJSON    string     `json:"content_json"`
	Markdown       string     `json:"markdown"`
	ConfigSnapshot string     `json:"config_snapshot"`
	InputTokens    int64      `json:"input_tokens"`
	OutputTokens   int64      `json:"output_tokens"`
	TotalTokens    int64      `json:"total_tokens"`
	ArticleCount   int        `json:"article_count"`
	AIUsed         bool       `json:"ai_used"`
	IsRead         bool       `json:"is_read"`
	Error          string     `json:"error"`
	FailureCode    string     `json:"failure_code"`
	GenerationMode string     `json:"generation_mode"`
	GenerationHash string     `json:"-"`
	CheckpointJSON string     `json:"-"`
	RetryOfID      *int64     `json:"retry_of_id"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at"`
	CompletedAt    *time.Time `json:"completed_at"`
}

// DailyReportRunFilter controls history pagination.
type DailyReportRunFilter struct {
	Status string
	Limit  int
	Offset int
}

// DailyReportSource is an immutable source snapshot for a generated report.
// ArticleID and FeedID become nil when the original records are deleted.
type DailyReportSource struct {
	ID              int64      `json:"id"`
	RunID           int64      `json:"run_id"`
	SourceIndex     int        `json:"source_index"`
	ArticleID       *int64     `json:"article_id"`
	FeedID          *int64     `json:"feed_id"`
	ArticleTitle    string     `json:"article_title"`
	FeedTitle       string     `json:"feed_title"`
	Author          string     `json:"author"`
	URL             string     `json:"url"`
	ArticleUniqueID string     `json:"-"`
	PublishedAt     *time.Time `json:"published_at"`
	FirstSeenAt     *time.Time `json:"first_seen_at"`
	LateArrival     bool       `json:"late_arrival"`
	ContentKind     string     `json:"content_kind"`
	CreatedAt       time.Time  `json:"created_at"`
}

// SavedFilter represents a user-saved article filter
type SavedFilter struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Conditions string    `json:"conditions"` // JSON string of FilterCondition[]
	Position   int       `json:"position"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Tag represents a user-defined tag for organizing feeds
type Tag struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Color    string `json:"color"`
	Position int    `json:"position"`
}

// AIProfile represents an AI configuration profile
type AIProfile struct {
	ID            int64     `json:"id"`
	Name          string    `json:"name"`
	APIKey        string    `json:"api_key,omitempty"` // Hidden in responses, only sent when needed
	Endpoint      string    `json:"endpoint"`
	Model         string    `json:"model"`
	CustomHeaders string    `json:"custom_headers"` // JSON string of key-value pairs
	IsDefault     bool      `json:"is_default"`     // Default profile for new features
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
