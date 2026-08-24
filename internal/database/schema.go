package database

import (
	"database/sql"
)

// initSchema initializes the database schema by creating all tables and indexes.
// This is extracted from db.go for better code organization.
func initSchema(db *sql.DB) error {
	// First create tables
	query := `
	CREATE TABLE IF NOT EXISTS feeds (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		title TEXT,
		url TEXT UNIQUE,
		link TEXT DEFAULT '',
		description TEXT,
		category TEXT DEFAULT '',
		image_url TEXT DEFAULT '',
		last_updated DATETIME,
		last_error TEXT DEFAULT ''
	);

	CREATE TABLE IF NOT EXISTS articles (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		feed_id INTEGER,
		title TEXT,
		url TEXT,
		image_url TEXT,
		audio_url TEXT DEFAULT '',
		video_url TEXT DEFAULT '',
		translated_title TEXT,
		published_at DATETIME,
		first_seen_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		has_valid_published_time BOOLEAN NOT NULL DEFAULT 1,
		is_read BOOLEAN DEFAULT 0,
		is_favorite BOOLEAN DEFAULT 0,
		is_hidden BOOLEAN DEFAULT 0,
		is_read_later BOOLEAN DEFAULT 0,
		summary TEXT DEFAULT '',
		original_summary TEXT DEFAULT '',
		unique_id TEXT UNIQUE,
		FOREIGN KEY(feed_id) REFERENCES feeds(id)
	);

	-- Translation cache table to avoid redundant API calls
	CREATE TABLE IF NOT EXISTS translation_cache (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_text_hash TEXT NOT NULL,
		source_text TEXT NOT NULL,
		target_lang TEXT NOT NULL,
		translated_text TEXT NOT NULL,
		provider TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(source_text_hash, target_lang, provider)
	);

	-- Article content cache table to store full article content
	CREATE TABLE IF NOT EXISTS article_contents (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		article_id INTEGER NOT NULL UNIQUE,
		content TEXT NOT NULL,
		fetched_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(article_id) REFERENCES articles(id) ON DELETE CASCADE
	);

	-- Chat sessions table to store AI chat conversations per article
	CREATE TABLE IF NOT EXISTS chat_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		article_id INTEGER NOT NULL,
		title TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(article_id) REFERENCES articles(id) ON DELETE CASCADE
	);

	-- Chat messages table to store individual messages in chat sessions
	CREATE TABLE IF NOT EXISTS chat_messages (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id INTEGER NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		thinking TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
	);

	-- Singleton configuration for the 24-hour AI report feature.
	CREATE TABLE IF NOT EXISTS daily_report_config (
		id INTEGER PRIMARY KEY CHECK (id = 1),
		enabled BOOLEAN NOT NULL DEFAULT 0,
		schedule_time TEXT NOT NULL DEFAULT '08:00',
		feed_scope TEXT NOT NULL DEFAULT 'all' CHECK (feed_scope IN ('all', 'selected')),
		include_hidden BOOLEAN NOT NULL DEFAULT 0,
		ai_profile_id INTEGER,
		focus TEXT NOT NULL DEFAULT '',
		outline_json TEXT NOT NULL DEFAULT '[]',
		language TEXT NOT NULL DEFAULT 'auto',
		title_template TEXT NOT NULL DEFAULT '',
		in_app_notification BOOLEAN NOT NULL DEFAULT 1,
		system_notification BOOLEAN NOT NULL DEFAULT 1,
		notify_on_empty BOOLEAN NOT NULL DEFAULT 0,
		last_handled_boundary DATETIME,
		cloud_consent_version INTEGER NOT NULL DEFAULT 0,
		cloud_consent_at DATETIME,
		cloud_consent_destination_fingerprint TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY(ai_profile_id) REFERENCES ai_profiles(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS daily_report_config_feeds (
		config_id INTEGER NOT NULL DEFAULT 1,
		feed_id INTEGER NOT NULL,
		PRIMARY KEY(config_id, feed_id),
		FOREIGN KEY(config_id) REFERENCES daily_report_config(id) ON DELETE CASCADE,
		FOREIGN KEY(feed_id) REFERENCES feeds(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS daily_report_runs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		kind TEXT NOT NULL CHECK (kind IN ('auto', 'manual', 'backfill')),
		status TEXT NOT NULL CHECK (status IN ('queued', 'refreshing', 'generating', 'completed', 'partial', 'no_content', 'failed', 'interrupted')),
		period_start DATETIME NOT NULL,
		period_end DATETIME NOT NULL,
		progress INTEGER NOT NULL DEFAULT 0,
		current_step TEXT NOT NULL DEFAULT '',
		title TEXT NOT NULL DEFAULT '',
		content_json TEXT NOT NULL DEFAULT '',
		markdown TEXT NOT NULL DEFAULT '',
		config_snapshot TEXT NOT NULL DEFAULT '{}',
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		article_count INTEGER NOT NULL DEFAULT 0,
		ai_used BOOLEAN NOT NULL DEFAULT 0,
		is_read BOOLEAN NOT NULL DEFAULT 0,
		error TEXT NOT NULL DEFAULT '',
		failure_code TEXT NOT NULL DEFAULT '',
		generation_mode TEXT NOT NULL DEFAULT '',
		generation_fingerprint TEXT NOT NULL DEFAULT '',
		generation_checkpoint TEXT NOT NULL DEFAULT '',
		retry_of_id INTEGER,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		started_at DATETIME,
		completed_at DATETIME,
		FOREIGN KEY(retry_of_id) REFERENCES daily_report_runs(id) ON DELETE SET NULL
	);

	CREATE TABLE IF NOT EXISTS daily_report_sources (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		run_id INTEGER NOT NULL,
		source_index INTEGER NOT NULL,
		article_id INTEGER,
		feed_id INTEGER,
		article_title TEXT NOT NULL DEFAULT '',
		feed_title TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT '',
		url TEXT NOT NULL DEFAULT '',
		article_unique_id TEXT NOT NULL DEFAULT '',
		published_at DATETIME,
		first_seen_at DATETIME,
		late_arrival BOOLEAN NOT NULL DEFAULT 0,
		content_kind TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(run_id, source_index),
		FOREIGN KEY(run_id) REFERENCES daily_report_runs(id) ON DELETE CASCADE,
		FOREIGN KEY(article_id) REFERENCES articles(id) ON DELETE SET NULL,
		FOREIGN KEY(feed_id) REFERENCES feeds(id) ON DELETE SET NULL
	);

	-- Create indexes for better query performance
	CREATE INDEX IF NOT EXISTS idx_articles_feed_id ON articles(feed_id);
	CREATE INDEX IF NOT EXISTS idx_articles_published_at ON articles(published_at DESC);
	CREATE INDEX IF NOT EXISTS idx_articles_is_read ON articles(is_read);
	CREATE INDEX IF NOT EXISTS idx_articles_is_favorite ON articles(is_favorite);
	CREATE INDEX IF NOT EXISTS idx_articles_is_hidden ON articles(is_hidden);
	CREATE INDEX IF NOT EXISTS idx_articles_is_read_later ON articles(is_read_later);
	CREATE INDEX IF NOT EXISTS idx_feeds_category ON feeds(category);

	-- Composite indexes for common query patterns
	CREATE INDEX IF NOT EXISTS idx_articles_feed_published ON articles(feed_id, published_at DESC);
	CREATE INDEX IF NOT EXISTS idx_articles_read_published ON articles(is_read, published_at DESC);
	CREATE INDEX IF NOT EXISTS idx_articles_fav_published ON articles(is_favorite, published_at DESC);
	CREATE INDEX IF NOT EXISTS idx_articles_readlater_published ON articles(is_read_later, published_at DESC);

	-- Covering index for category queries (hidden + published_at)
	-- Optimizes queries with: WHERE is_hidden = 0 ORDER BY published_at DESC
	CREATE INDEX IF NOT EXISTS idx_articles_hidden_published ON articles(is_hidden, published_at DESC);

	-- Translation cache index
	CREATE INDEX IF NOT EXISTS idx_translation_cache_lookup ON translation_cache(source_text_hash, target_lang, provider);

	-- Article content cache index
	CREATE INDEX IF NOT EXISTS idx_article_contents_article_id ON article_contents(article_id);

	-- Chat sessions and messages indexes
	CREATE INDEX IF NOT EXISTS idx_chat_sessions_article_id ON chat_sessions(article_id);
	CREATE INDEX IF NOT EXISTS idx_chat_sessions_updated_at ON chat_sessions(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_chat_messages_session_id ON chat_messages(session_id);

	-- Daily report indexes. The first-seen article index is added by the
	-- migration after legacy article tables have gained the column.
	CREATE INDEX IF NOT EXISTS idx_daily_report_config_feeds_feed ON daily_report_config_feeds(feed_id);
	CREATE UNIQUE INDEX IF NOT EXISTS idx_daily_report_runs_period_unique
		ON daily_report_runs(period_start, period_end)
		WHERE kind != 'manual' AND status NOT IN ('failed', 'interrupted');
	CREATE INDEX IF NOT EXISTS idx_daily_report_runs_created ON daily_report_runs(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_daily_report_runs_status_created ON daily_report_runs(status, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_daily_report_runs_unread ON daily_report_runs(is_read, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_daily_report_sources_run ON daily_report_sources(run_id, source_index);
	CREATE INDEX IF NOT EXISTS idx_daily_report_sources_article ON daily_report_sources(article_id);
	CREATE INDEX IF NOT EXISTS idx_daily_report_sources_unique ON daily_report_sources(article_unique_id);
	`
	_, err := db.Exec(query)
	if err != nil {
		return err
	}

	// Then run migrations to ensure all columns exist
	// This must happen AFTER creating tables
	if err := runMigrations(db); err != nil {
		return err
	}

	return nil
}
