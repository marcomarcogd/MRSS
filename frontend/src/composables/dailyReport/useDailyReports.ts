import { computed, ref } from 'vue';
import { Events } from '@wailsio/runtime';
import type {
  DailyReportAPIErrorDetails,
  DailyReportCloudProcessing,
  DailyReportConfig,
  DailyReportDetail,
  DailyReportHistoryResponse,
  DailyReportPreview,
  DailyReportRun,
  DailyReportStatus,
  DailyReportStatusResponse,
} from '@/types/dailyReport';
import { DEFAULT_DAILY_REPORT_CONFIG } from '@/types/dailyReport';

const config = ref<DailyReportConfig>({ ...DEFAULT_DAILY_REPORT_CONFIG });
const cloudProcessing = ref<DailyReportCloudProcessing>({
  disclosure_version: 1,
  required: false,
  accepted: true,
  accepted_version: null,
  accepted_at: null,
  destination: null,
});
const status = ref<DailyReportStatusResponse>({
  enabled: false,
  is_running: false,
  progress: 0,
  unread_count: 0,
  missed_count: 0,
  notification_authorization: 'not_determined',
});
const history = ref<DailyReportRun[]>([]);
const historyTotal = ref(0);
const historyPage = ref(1);
const historyPageSize = ref(20);
const historyStatus = ref<DailyReportStatus | ''>('');
const selectedRunId = ref<number | null>(null);
const selectedDetail = ref<DailyReportDetail | null>(null);
const loadingConfig = ref(false);
const loadingHistory = ref(false);
const loadingDetail = ref(false);
const savingConfig = ref(false);
const missedPromptVisible = ref(false);
const initialised = ref(false);
const consentModalVisible = ref(false);
const consentActionRunning = ref(false);
const consentDismissalSequence = ref(0);
let pendingConsentAction: (() => void | Promise<void>) | null = null;
let retryingAfterConsent = false;
let detailRequestSequence = 0;
let detailLoadingSequence = 0;
let pollingTimer: ReturnType<typeof setInterval> | null = null;
let removeCompletedListener: (() => void) | null = null;
let removeOpenListener: (() => void) | null = null;

export class DailyReportAPIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code?: string,
    readonly details?: DailyReportAPIErrorDetails
  ) {
    super(message);
    this.name = 'DailyReportAPIError';
  }
}

async function readError(response: Response): Promise<DailyReportAPIError> {
  try {
    const body = await response.json();
    const message =
      (typeof body.error?.message === 'string' && body.error.message) ||
      (typeof body.error === 'string' && body.error) ||
      (typeof body.message === 'string' && body.message) ||
      `HTTP ${response.status}`;
    return new DailyReportAPIError(
      message,
      response.status,
      typeof body.error?.code === 'string' ? body.error.code : undefined,
      body.error?.details
    );
  } catch {
    return new DailyReportAPIError(`HTTP ${response.status}`, response.status);
  }
}

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const response = await fetch(url, init);
  if (!response.ok) throw await readError(response);
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

function normalizeConfig(value?: Partial<DailyReportConfig> | null): DailyReportConfig {
  return {
    ...DEFAULT_DAILY_REPORT_CONFIG,
    ...value,
    feed_ids: Array.isArray(value?.feed_ids) ? value.feed_ids : [],
    outline: Array.isArray(value?.outline) ? value.outline : [],
  };
}

async function fetchConfig(): Promise<DailyReportConfig> {
  loadingConfig.value = true;
  try {
    const data = await request<{
      config: DailyReportConfig;
      cloud_processing?: DailyReportCloudProcessing;
    }>('/api/daily-report/config');
    config.value = normalizeConfig(data.config);
    if (data.cloud_processing) cloudProcessing.value = data.cloud_processing;
    return config.value;
  } finally {
    loadingConfig.value = false;
  }
}

async function saveConfig(value: DailyReportConfig): Promise<DailyReportConfig> {
  savingConfig.value = true;
  try {
    const editableConfig = {
      enabled: value.enabled,
      schedule_time: value.schedule_time,
      feed_scope: value.feed_scope,
      feed_ids: value.feed_ids,
      include_hidden: value.include_hidden,
      ai_profile_id: value.ai_profile_id,
      article_summary_mode: value.article_summary_mode,
      focus: value.focus,
      outline: value.outline,
      language: value.language,
      title_template: value.title_template,
      in_app_notification: value.in_app_notification,
      system_notification: value.system_notification,
      notify_on_empty: value.notify_on_empty,
    };
    const data = await request<{
      config: DailyReportConfig;
      cloud_processing?: DailyReportCloudProcessing;
    }>('/api/daily-report/config', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(editableConfig),
    });
    config.value = normalizeConfig(data.config);
    if (data.cloud_processing) cloudProcessing.value = data.cloud_processing;
    await fetchStatus();
    return config.value;
  } finally {
    savingConfig.value = false;
  }
}

async function fetchStatus(options: { promptMissed?: boolean } = {}): Promise<void> {
  const data = await request<DailyReportStatusResponse>('/api/daily-report/status');
  status.value = data;
  if (options.promptMissed && data.missed_count > 0) missedPromptVisible.value = true;
}

async function fetchHistory(
  page = historyPage.value,
  options: { silent?: boolean } = {}
): Promise<void> {
  if (!options.silent) loadingHistory.value = true;
  try {
    const params = new URLSearchParams({
      page: String(page),
      page_size: String(historyPageSize.value),
    });
    if (historyStatus.value) params.set('status', historyStatus.value);
    const data = await request<DailyReportHistoryResponse>(
      `/api/daily-report/history?${params.toString()}`
    );
    history.value = Array.isArray(data.items) ? data.items : [];
    historyTotal.value = data.total || 0;
    historyPage.value = data.page || page;
  } finally {
    if (!options.silent) loadingHistory.value = false;
  }
}

async function fetchDetail(
  id: number,
  options: { silent?: boolean } = {}
): Promise<DailyReportDetail> {
  const requestSequence = ++detailRequestSequence;
  const loadingSequence = options.silent ? 0 : ++detailLoadingSequence;
  if (!options.silent) loadingDetail.value = true;
  selectedRunId.value = id;
  try {
    const detail = await request<DailyReportDetail>(`/api/daily-report/history/${id}`);
    detail.sources = Array.isArray(detail.sources) ? detail.sources : [];
    if (!detail.retry_state) {
      detail.retry_state = { action: 'none', reason: 'not_recoverable' };
    }
    if (requestSequence === detailRequestSequence && selectedRunId.value === id) {
      selectedDetail.value = detail;
    }
    return detail;
  } finally {
    if (!options.silent && loadingSequence === detailLoadingSequence) {
      loadingDetail.value = false;
    }
  }
}

async function refreshSelectedRun(id = selectedRunId.value): Promise<DailyReportDetail | null> {
  if (!id) return null;
  const detail = await fetchDetail(id, { silent: true });
  await Promise.all([fetchHistory(historyPage.value, { silent: true }), fetchStatus()]);
  return detail;
}

async function markRead(id: number, read: boolean): Promise<void> {
  const data = await request<{ run: DailyReportRun }>(`/api/daily-report/history/${id}/read`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ read }),
  });
  const index = history.value.findIndex((item) => item.id === id);
  if (index >= 0) history.value[index] = data.run;
  if (selectedDetail.value?.run.id === id) selectedDetail.value.run = data.run;
  await fetchStatus();
}

async function retryRun(id: number, restart = false): Promise<DailyReportRun> {
  const suffix = restart ? '?restart=true' : '';
  const data = await request<{ run: DailyReportRun }>(
    `/api/daily-report/history/${id}/retry${suffix}`,
    { method: 'POST' }
  );
  await Promise.all([fetchHistory(), fetchStatus()]);
  return data.run;
}

async function createLocalFallback(id: number): Promise<DailyReportRun> {
  const data = await request<{ run: DailyReportRun }>(
    `/api/daily-report/history/${id}/local-fallback`,
    { method: 'POST' }
  );
  await Promise.all([fetchHistory(1), fetchStatus()]);
  return data.run;
}

async function deleteRun(id: number): Promise<void> {
  await request<void>(`/api/daily-report/history/${id}`, { method: 'DELETE' });
  if (selectedRunId.value === id) {
    selectedRunId.value = null;
    selectedDetail.value = null;
  }
  const maxPage = Math.max(
    1,
    Math.ceil(Math.max(0, historyTotal.value - 1) / historyPageSize.value)
  );
  await Promise.all([fetchHistory(Math.min(historyPage.value, maxPage)), fetchStatus()]);
}

async function previewGenerate(): Promise<DailyReportPreview> {
  return request<DailyReportPreview>('/api/daily-report/generate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action: 'preview' }),
  });
}

async function startGenerate(): Promise<DailyReportRun> {
  const data = await request<{ run: DailyReportRun }>('/api/daily-report/generate', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action: 'start' }),
  });
  await Promise.all([fetchHistory(1), fetchStatus()]);
  selectedRunId.value = data.run.id;
  return data.run;
}

async function optimizeOutline(input: {
  focus: string;
  language: DailyReportConfig['language'];
  ai_profile_id: number | null;
}): Promise<DailyReportConfig['outline']> {
  const data = await request<{ outline: DailyReportConfig['outline'] }>(
    '/api/daily-report/outline/optimize',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(input),
    }
  );
  return Array.isArray(data.outline) ? data.outline : [];
}

async function handleMissedRuns(action: 'latest' | 'all' | 'skip_all'): Promise<void> {
  await request<{ accepted: number; skipped: number }>('/api/daily-report/missed-runs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ action }),
  });
  missedPromptVisible.value = false;
  await Promise.all([fetchHistory(1), fetchStatus()]);
}

async function authorizeNotifications(): Promise<
  DailyReportStatusResponse['notification_authorization']
> {
  const data = await request<{ status: DailyReportStatusResponse['notification_authorization'] }>(
    '/api/daily-report/notifications/authorize',
    { method: 'POST' }
  );
  status.value.notification_authorization = data.status;
  return data.status;
}

async function fetchCloudProcessing(): Promise<DailyReportCloudProcessing> {
  const data = await request<{ cloud_processing: DailyReportCloudProcessing }>(
    '/api/daily-report/consent'
  );
  if (data.cloud_processing) cloudProcessing.value = data.cloud_processing;
  return cloudProcessing.value;
}

async function updateCloudProcessingConsent(
  action: 'grant' | 'revoke',
  options: { refreshConfig?: boolean } = {}
): Promise<DailyReportCloudProcessing> {
  const body = action === 'grant' ? { action, version: 1 } : { action };
  const data = await request<{ cloud_processing: DailyReportCloudProcessing }>(
    '/api/daily-report/consent',
    {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }
  );
  if (data.cloud_processing) cloudProcessing.value = data.cloud_processing;
  if (options.refreshConfig === false) {
    if (action === 'revoke') config.value.enabled = false;
    await fetchStatus();
  } else {
    await Promise.allSettled([fetchConfig(), fetchStatus()]);
  }
  return cloudProcessing.value;
}

function isCloudConsentRequired(error: unknown): error is DailyReportAPIError {
  return (
    error instanceof DailyReportAPIError &&
    error.status === 409 &&
    error.code === 'cloud_processing_consent_required'
  );
}

async function promptCloudConsent(
  error: unknown,
  retryAction: () => void | Promise<void>
): Promise<boolean> {
  if (retryingAfterConsent) return false;
  if (!isCloudConsentRequired(error)) return false;
  if (error.details?.cloud_processing) {
    cloudProcessing.value = error.details.cloud_processing;
  } else {
    try {
      await fetchCloudProcessing();
    } catch (refreshError) {
      console.error('Failed to refresh cloud processing disclosure:', refreshError);
      pendingConsentAction = null;
      consentModalVisible.value = false;
      return false;
    }
  }
  // A local-only configuration never requires disclosure. Refuse to show an
  // empty consent dialog if a stale backend response lacks a destination.
  if (!cloudProcessing.value.destination) return false;
  pendingConsentAction = retryAction;
  consentModalVisible.value = true;
  return true;
}

function openCloudConsentPrompt(retryAction?: () => void | Promise<void>): void {
  if (!cloudProcessing.value.destination) return;
  pendingConsentAction = retryAction || null;
  consentModalVisible.value = true;
}

async function grantCloudConsentAndRetry(): Promise<void> {
  if (consentActionRunning.value) return;
  consentActionRunning.value = true;
  try {
    const granted = await updateCloudProcessingConsent('grant');
    if (!granted.accepted || !granted.destination) {
      throw new Error('Cloud processing consent was not accepted for the current destination');
    }
    consentModalVisible.value = false;
    const action = pendingConsentAction;
    pendingConsentAction = null;
    retryingAfterConsent = true;
    try {
      await action?.();
    } finally {
      retryingAfterConsent = false;
    }
  } finally {
    consentActionRunning.value = false;
  }
}

function closeCloudConsentPrompt(): void {
  consentModalVisible.value = false;
  pendingConsentAction = null;
  consentDismissalSequence.value += 1;
}

function showMissedPrompt(): void {
  if (status.value.missed_count > 0) missedPromptVisible.value = true;
}

function closeMissedPrompt(): void {
  missedPromptVisible.value = false;
}

function selectRun(id: number | null): void {
  detailRequestSequence += 1;
  detailLoadingSequence += 1;
  selectedRunId.value = id;
  if (id === null) selectedDetail.value = null;
  loadingDetail.value = false;
}

async function refreshAfterCompletion(runId?: number): Promise<void> {
  await Promise.all([fetchStatus(), fetchHistory(1)]);
  if (runId && selectedRunId.value === runId) await fetchDetail(runId);
}

function installEventListeners(
  onOpen: (runId?: number) => void,
  onCompleted?: (
    runId?: number,
    runStatus?: DailyReportStatus,
    systemRequested?: boolean,
    systemDelivered?: boolean
  ) => void
): void {
  if (removeCompletedListener || removeOpenListener) return;
  try {
    removeCompletedListener = Events.On('daily-report:completed', (event) => {
      const runId = Number(event.data?.run_id || event.data?.id || event.data || 0) || undefined;
      void refreshAfterCompletion(runId);
      onCompleted?.(
        runId,
        event.data?.status as DailyReportStatus | undefined,
        event.data?.system_requested === true,
        event.data?.system_delivered === true
      );
    });
    removeOpenListener = Events.On('daily-report:open', (event) => {
      const runId = Number(event.data?.run_id || event.data?.id || event.data || 0) || undefined;
      onOpen(runId);
    });
  } catch (error) {
    console.warn('Daily report native events are unavailable:', error);
  }
}

function startPolling(): void {
  if (pollingTimer) return;
  pollingTimer = setInterval(() => {
    const wasRunning = status.value.is_running;
    const selectedWasRunning = Boolean(
      selectedDetail.value &&
      ['queued', 'refreshing', 'generating'].includes(selectedDetail.value.run.status)
    );
    void fetchStatus()
      .then(async () => {
        if (!wasRunning && !status.value.is_running && !selectedWasRunning) return;
        await fetchHistory(historyPage.value, { silent: true });
        if (
          selectedRunId.value &&
          (selectedWasRunning || selectedRunId.value === status.value.current_run_id)
        ) {
          await fetchDetail(selectedRunId.value, { silent: true });
        }
      })
      .catch((error) => console.error('Failed to poll daily report status:', error));
  }, 15_000);
}

function stopPolling(): void {
  if (pollingTimer) clearInterval(pollingTimer);
  pollingTimer = null;
  removeCompletedListener?.();
  removeOpenListener?.();
  removeCompletedListener = null;
  removeOpenListener = null;
  initialised.value = false;
}

async function initialize(
  onOpen: (runId?: number) => void,
  onCompleted?: (
    runId?: number,
    runStatus?: DailyReportStatus,
    systemRequested?: boolean,
    systemDelivered?: boolean
  ) => void
): Promise<void> {
  if (initialised.value) return;
  initialised.value = true;
  installEventListeners(onOpen, onCompleted);
  startPolling();
  try {
    await Promise.all([fetchConfig(), fetchStatus({ promptMissed: true })]);
  } catch (error) {
    initialised.value = false;
    console.error('Failed to initialize daily reports:', error);
  }
}

export function useDailyReports() {
  return {
    config,
    cloudProcessing,
    status,
    history,
    historyTotal,
    historyPage,
    historyPageSize,
    historyStatus,
    selectedRunId,
    selectedDetail,
    loadingConfig,
    loadingHistory,
    loadingDetail,
    savingConfig,
    missedPromptVisible,
    consentModalVisible,
    consentActionRunning,
    consentDismissalSequence,
    unreadCount: computed(() => status.value.unread_count || 0),
    totalPages: computed(() => Math.max(1, Math.ceil(historyTotal.value / historyPageSize.value))),
    fetchConfig,
    saveConfig,
    fetchStatus,
    fetchHistory,
    fetchDetail,
    refreshSelectedRun,
    markRead,
    retryRun,
    createLocalFallback,
    deleteRun,
    previewGenerate,
    startGenerate,
    optimizeOutline,
    handleMissedRuns,
    authorizeNotifications,
    fetchCloudProcessing,
    updateCloudProcessingConsent,
    isCloudConsentRequired,
    promptCloudConsent,
    openCloudConsentPrompt,
    grantCloudConsentAndRetry,
    closeCloudConsentPrompt,
    showMissedPrompt,
    closeMissedPrompt,
    selectRun,
    initialize,
    stopPolling,
  };
}
