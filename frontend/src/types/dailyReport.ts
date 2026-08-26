export type DailyReportView = 'articles' | 'imageGallery' | 'dailyReports';

export type DailyReportFeedScope = 'all' | 'selected';
export type DailyReportLanguage = 'auto' | 'zh-CN' | 'en';
export type DailyReportArticleSummaryMode = 'ai' | 'local';
export type DailyReportStatus =
  | 'queued'
  | 'refreshing'
  | 'generating'
  | 'completed'
  | 'partial'
  | 'no_content'
  | 'failed'
  | 'interrupted';

export interface DailyReportOutlineItem {
  id: string;
  title: string;
  instruction: string;
}

export interface DailyReportConfig {
  enabled: boolean;
  schedule_time: string;
  feed_scope: DailyReportFeedScope;
  feed_ids: number[];
  include_hidden: boolean;
  ai_profile_id: number | null;
  article_summary_mode: DailyReportArticleSummaryMode;
  focus: string;
  outline: DailyReportOutlineItem[];
  language: DailyReportLanguage;
  title_template: string;
  in_app_notification: boolean;
  system_notification: boolean;
  notify_on_empty: boolean;
  last_handled_boundary?: string | null;
  created_at?: string;
  updated_at?: string;
}

export interface DailyReportSection {
  id: string;
  title: string;
  summary: string;
  source_ids: number[];
  blocks?: DailyReportBlock[];
}

export type DailyReportBlockType = 'paragraph' | 'heading' | 'unordered_list' | 'ordered_list';

export interface DailyReportBlockItem {
  text: string;
  source_ids?: number[];
}

export interface DailyReportBlock {
  type: DailyReportBlockType;
  text?: string;
  items?: DailyReportBlockItem[];
  source_ids?: number[];
}

export interface DailyReportContent {
  sections: DailyReportSection[];
}

export interface DailyReportRun {
  id: number;
  kind: 'auto' | 'manual' | 'backfill';
  status: DailyReportStatus;
  period_start: string;
  period_end: string;
  progress: number;
  current_step?: string;
  title: string;
  content: DailyReportContent | string | null;
  markdown: string;
  input_tokens: number;
  output_tokens: number;
  total_tokens?: number;
  ai_used?: boolean;
  article_count: number;
  is_read: boolean;
  error: string;
  retry_of_id?: number | null;
  failure_code?: string;
  generation_mode?: 'ai' | 'local';
  created_at: string;
  started_at?: string | null;
  completed_at?: string | null;
}

export interface DailyReportSource {
  id: number;
  source_index: number;
  article_id?: number | null;
  title: string;
  feed_id?: number | null;
  feed_title: string;
  author: string;
  url: string;
  published_at?: string | null;
  first_seen_at?: string | null;
  late_arrival: boolean;
  content_kind?: string;
}

export interface DailyReportDetail {
  run: DailyReportRun;
  sources: DailyReportSource[];
  retry_state: DailyReportRetryState;
}

export interface DailyReportRetryState {
  action: 'resume' | 'restart' | 'none';
  reason: 'checkpoint_valid' | 'inputs_changed' | 'checkpoint_missing' | 'not_recoverable';
}

export interface DailyReportStatusResponse {
  enabled: boolean;
  is_running: boolean;
  current_run_id?: number | null;
  progress: number;
  unread_count: number;
  next_scheduled_at?: string | null;
  missed_count: number;
  requires_feed_selection?: boolean;
  notification_authorization: 'authorized' | 'denied' | 'not_determined' | 'unsupported';
}

export interface DailyReportHistoryResponse {
  items: DailyReportRun[];
  total: number;
  page: number;
  page_size: number;
}

export interface DailyReportPreview {
  period_start: string;
  period_end: string;
  article_count: number;
  estimated_batches: number;
}

export interface DailyReportCloudDestination {
  profile_id: number | null;
  profile_name: string;
  endpoint: string;
}

export interface DailyReportCloudProcessing {
  disclosure_version: number;
  required: boolean;
  accepted: boolean;
  accepted_version: number | null;
  accepted_at: string | null;
  destination: DailyReportCloudDestination | null;
}

export interface DailyReportAPIErrorDetails {
  cloud_processing?: DailyReportCloudProcessing;
  [key: string]: unknown;
}

export const DEFAULT_DAILY_REPORT_CONFIG: DailyReportConfig = {
  enabled: false,
  schedule_time: '08:00',
  feed_scope: 'all',
  feed_ids: [],
  include_hidden: false,
  ai_profile_id: null,
  article_summary_mode: 'ai',
  focus: '',
  outline: [],
  language: 'auto',
  title_template: '24 小时 AI 日报 · {{date}}',
  in_app_notification: true,
  system_notification: true,
  notify_on_empty: false,
};
