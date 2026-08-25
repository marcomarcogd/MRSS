<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import { Clipboard } from '@wailsio/runtime';
import {
  PhArrowLeft,
  PhArticle,
  PhCaretLeft,
  PhCaretRight,
  PhCheckCircle,
  PhCircleNotch,
  PhClock,
  PhCopy,
  PhDownloadSimple,
  PhEnvelopeOpen,
  PhEnvelopeSimple,
  PhGear,
  PhNewspaperClipping,
  PhPlay,
  PhRepeat,
  PhTrash,
  PhWarningCircle,
} from '@phosphor-icons/vue';
import BaseModal from '@/components/common/BaseModal.vue';
import DailyReportConfigModal from './DailyReportConfigModal.vue';
import { useDailyReports } from '@/composables/dailyReport/useDailyReports';
import { useAppStore } from '@/stores/app';
import { openInBrowser } from '@/utils/browser';
import type {
  DailyReportContent,
  DailyReportPreview,
  DailyReportRun,
  DailyReportSource,
  DailyReportStatus,
} from '@/types/dailyReport';

const { t, locale } = useI18n();
const store = useAppStore();
const {
  status,
  history,
  historyTotal,
  historyPage,
  historyStatus,
  selectedRunId,
  selectedDetail,
  loadingHistory,
  loadingDetail,
  totalPages,
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
  promptCloudConsent,
  showMissedPrompt,
  selectRun,
} = useDailyReports();

const showConfig = ref(false);
const preview = ref<DailyReportPreview | null>(null);
const previewing = ref(false);
const starting = ref(false);
const retryingRunId = ref<number | null>(null);
const fallbackRunId = ref<number | null>(null);
const mobileDetail = ref(false);
const activeRunStatuses: DailyReportStatus[] = ['queued', 'refreshing', 'generating'];
const detailPollInterval = 2_000;
let detailPollTimer: ReturnType<typeof setTimeout> | null = null;
let detailPollInFlight = false;
let detailPollingDisposed = false;

const statuses: Array<DailyReportStatus | ''> = [
  '',
  'queued',
  'refreshing',
  'generating',
  'completed',
  'partial',
  'no_content',
  'failed',
  'interrupted',
];

const sections = computed(() => parseContent(selectedDetail.value?.run.content));
const isSelectedRunActive = computed(() =>
  selectedDetail.value ? activeRunStatuses.includes(selectedDetail.value.run.status) : false
);
const selectedProgress = computed(() =>
  selectedDetail.value ? runProgress(selectedDetail.value.run) : 0
);
const selectedStepLabel = computed(() =>
  selectedDetail.value ? progressStepLabel(selectedDetail.value.run) : ''
);
const displayError = computed(() => {
  const run = selectedDetail.value?.run;
  if (!run?.error) return '';
  if (run.generation_mode === 'local' && run.failure_code === 'usage_limit_reached') {
    return t('dailyReport.detail.localFallbackUsageLimit');
  }
  if (run.generation_mode === 'local' && run.failure_code === 'no_ai_provider') {
    return t('dailyReport.detail.localFallbackNoProvider');
  }
  if (
    run.failure_code === 'checkpoint_invalidated' ||
    selectedDetail.value?.retry_state.reason === 'inputs_changed'
  ) {
    return t('dailyReport.detail.checkpointInvalidated');
  }
  if (run.status === 'partial') return t('dailyReport.detail.partialError');
  if (run.status === 'interrupted') return t('dailyReport.detail.interruptedError');
  return t('dailyReport.detail.failedError');
});
const canRecoverAI = computed(() => {
  const run = selectedDetail.value?.run;
  return Boolean(
    run &&
    run.generation_mode === 'ai' &&
    ['failed', 'interrupted'].includes(run.status) &&
    selectedDetail.value?.retry_state.action !== 'none'
  );
});
const retryAction = computed(() => selectedDetail.value?.retry_state.action || 'none');
const sourcesById = computed(() => {
  const map = new Map<number, DailyReportSource>();
  selectedDetail.value?.sources.forEach((source) => map.set(source.source_index, source));
  return map;
});

onMounted(async () => {
  try {
    await Promise.all([fetchStatus(), fetchHistory(1)]);
    if (selectedRunId.value) await selectReport(selectedRunId.value);
    else if (history.value[0]) await selectReport(history.value[0].id, false);
    showMissedPrompt();
  } catch (error) {
    console.error('Failed to load daily reports:', error);
    window.showToast(t('dailyReport.toast.loadFailed'), 'error');
  }
});

onBeforeUnmount(() => {
  detailPollingDisposed = true;
  stopDetailPolling();
});

watch(
  [
    selectedRunId,
    () => selectedDetail.value?.run.status,
    () => selectedDetail.value?.run.current_step,
  ],
  () => syncDetailPolling(),
  { flush: 'post' }
);

watch(historyStatus, async () => {
  try {
    await fetchHistory(1);
    if (!history.value.some((run) => run.id === selectedRunId.value)) selectRun(null);
  } catch (error) {
    console.error('Failed to filter daily reports:', error);
    window.showToast(t('dailyReport.toast.loadFailed'), 'error');
  }
});

function parseContent(content: DailyReportRun['content']): DailyReportContent['sections'] {
  if (!content) return [];
  try {
    const parsed = typeof content === 'string' ? JSON.parse(content) : content;
    return Array.isArray(parsed?.sections) ? parsed.sections : [];
  } catch (error) {
    console.error('Invalid daily report content:', error);
    return [];
  }
}

function statusLabel(value: DailyReportStatus): string {
  return t(`dailyReport.status.${value}`);
}

function statusClass(value: DailyReportStatus): string {
  if (value === 'completed') return 'status-success';
  if (value === 'partial' || value === 'interrupted') return 'status-warning';
  if (value === 'failed') return 'status-error';
  if (value === 'no_content') return 'status-neutral';
  return 'status-running';
}

function runProgress(run: DailyReportRun): number {
  const stored = Math.max(0, Math.min(100, Math.round(run.progress || 0)));
  const step = run.current_step || '';
  const extraction = step.match(/^extracting:(\d+)\/(\d+)$/);
  if (extraction) {
    const current = Number(extraction[1]);
    const total = Math.max(1, Number(extraction[2]));
    return Math.max(stored, Math.min(75, 55 + Math.round((current / total) * 20)));
  }
  const merge = step.match(/^merging:(\d+):(\d+)\/(\d+)$/);
  if (merge) {
    const current = Number(merge[2]);
    const total = Math.max(1, Number(merge[3]));
    return Math.max(stored, Math.min(86, 76 + Math.round((current / total) * 10)));
  }
  if (step === 'finalizing') return Math.max(stored, 88);
  if (step === 'saving' || step === 'completed') return Math.max(stored, 90);
  return stored;
}

function progressStepLabel(run: DailyReportRun): string {
  const step = run.current_step || run.status;
  const extraction = step.match(/^extracting:(\d+)\/(\d+)$/);
  if (extraction) {
    return t('dailyReport.progress.stages.extracting', {
      current: Number(extraction[1]),
      total: Number(extraction[2]),
    });
  }
  const merge = step.match(/^merging:(\d+):(\d+)\/(\d+)$/);
  if (merge) {
    return t('dailyReport.progress.stages.merging', {
      round: Number(merge[1]),
      current: Number(merge[2]),
      total: Number(merge[3]),
    });
  }
  const knownStep = [
    'queued',
    'refreshing',
    'collecting',
    'generating',
    'finalizing',
    'saving',
    'completed',
  ].includes(step)
    ? step
    : 'generating';
  return t(`dailyReport.progress.stages.${knownStep}`);
}

function formatNumber(value?: number): string {
  return new Intl.NumberFormat(locale.value).format(value || 0);
}

function stopDetailPolling(): void {
  if (detailPollTimer) clearTimeout(detailPollTimer);
  detailPollTimer = null;
}

function syncDetailPolling(): void {
  stopDetailPolling();
  if (detailPollingDisposed || !selectedRunId.value || !isSelectedRunActive.value) return;
  detailPollTimer = setTimeout(() => void pollSelectedDetail(), detailPollInterval);
}

async function pollSelectedDetail(): Promise<void> {
  stopDetailPolling();
  const runId = selectedRunId.value;
  if (!runId || !isSelectedRunActive.value || detailPollInFlight) {
    syncDetailPolling();
    return;
  }
  detailPollInFlight = true;
  try {
    await refreshSelectedRun(runId);
  } catch (error) {
    console.error('Failed to refresh active daily report detail:', error);
  } finally {
    detailPollInFlight = false;
    syncDetailPolling();
  }
}

function formatDate(value?: string | null, options?: Intl.DateTimeFormatOptions): string {
  if (!value) return '—';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat(
    locale.value,
    options || { dateStyle: 'medium', timeStyle: 'short' }
  ).format(date);
}

async function selectReport(id: number, revealMobile = true): Promise<void> {
  try {
    const detail = await fetchDetail(id);
    if (selectedRunId.value !== id) return;
    mobileDetail.value = revealMobile;
    if (revealMobile && !detail.run.is_read) await markRead(detail.run.id, true);
  } catch (error) {
    console.error('Failed to load daily report detail:', error);
    window.showToast(t('dailyReport.toast.detailFailed'), 'error');
  }
}

async function toggleSelectedRead(): Promise<void> {
  const run = selectedDetail.value?.run;
  if (!run) return;
  try {
    await markRead(run.id, !run.is_read);
  } catch (error) {
    console.error('Failed to update daily report read state:', error);
    window.showToast(t('dailyReport.toast.readStateFailed'), 'error');
  }
}

async function changePage(page: number): Promise<void> {
  if (page < 1 || page > totalPages.value) return;
  try {
    await fetchHistory(page);
    if (history.value[0]) await selectReport(history.value[0].id, false);
  } catch (error) {
    console.error('Failed to change daily report page:', error);
    window.showToast(t('dailyReport.toast.loadFailed'), 'error');
  }
}

async function openGeneratePreview(): Promise<void> {
  previewing.value = true;
  try {
    preview.value = await previewGenerate();
  } catch (error) {
    console.error('Failed to preview daily report:', error);
    window.showToast(t('dailyReport.toast.previewFailed'), 'error');
  } finally {
    previewing.value = false;
  }
}

async function confirmGenerate(): Promise<void> {
  starting.value = true;
  try {
    const run = await startGenerate();
    preview.value = null;
    await selectReport(run.id);
    window.showToast(t('dailyReport.toast.generationStarted'), 'success');
  } catch (error) {
    if (await promptCloudConsent(error, confirmGenerate)) return;
    console.error('Failed to start daily report:', error);
    window.showToast(t('dailyReport.toast.generationFailed'), 'error');
  } finally {
    starting.value = false;
  }
}

async function handleRetry(run: DailyReportRun, action: 'resume' | 'restart'): Promise<void> {
  if (retryingRunId.value !== null) return;
  retryingRunId.value = run.id;
  try {
    const retried = await retryRun(run.id, action === 'restart');
    await selectReport(retried.id);
    window.showToast(
      action === 'restart'
        ? t('dailyReport.toast.restartStarted')
        : t('dailyReport.toast.resumeStarted'),
      'success'
    );
  } catch (error) {
    if (await promptCloudConsent(error, () => handleRetry(run, action))) return;
    if (error instanceof Error && 'code' in error && error.code === 'checkpoint_invalidated') {
      await fetchDetail(run.id);
      window.showToast(t('dailyReport.toast.checkpointChanged'), 'warning');
      return;
    }
    console.error('Failed to retry daily report:', error);
    window.showToast(t('dailyReport.toast.retryFailed'), 'error');
  } finally {
    retryingRunId.value = null;
  }
}

async function handleLocalFallback(run: DailyReportRun): Promise<void> {
  if (fallbackRunId.value !== null) return;
  fallbackRunId.value = run.id;
  try {
    const fallback = await createLocalFallback(run.id);
    await selectReport(fallback.id);
    window.showToast(t('dailyReport.toast.localFallbackStarted'), 'success');
  } catch (error) {
    console.error('Failed to create local daily report fallback:', error);
    window.showToast(t('dailyReport.toast.localFallbackFailed'), 'error');
  } finally {
    fallbackRunId.value = null;
  }
}

async function handleDelete(run: DailyReportRun): Promise<void> {
  const confirmed = await window.showConfirm({
    title: t('dailyReport.delete.title'),
    message: t('dailyReport.delete.message', { title: run.title || t('dailyReport.untitled') }),
    confirmText: t('common.confirm'),
    cancelText: t('common.cancel'),
    isDanger: true,
  });
  if (!confirmed) return;
  try {
    await deleteRun(run.id);
    mobileDetail.value = false;
    if (history.value[0]) await selectReport(history.value[0].id, false);
    window.showToast(t('dailyReport.toast.deleted'), 'success');
  } catch (error) {
    console.error('Failed to delete daily report:', error);
    window.showToast(t('dailyReport.toast.deleteFailed'), 'error');
  }
}

async function copyMarkdown(): Promise<void> {
  const markdown = selectedDetail.value?.run.markdown;
  if (!markdown) return;
  try {
    await Clipboard.SetText(markdown);
    window.showToast(t('common.toast.copiedToClipboard'), 'success');
  } catch (error) {
    console.error('Failed to copy daily report:', error);
    window.showToast(t('common.errors.failedToCopy'), 'error');
  }
}

function downloadMarkdown(): void {
  const run = selectedDetail.value?.run;
  if (!run?.markdown) return;
  const blob = new Blob([run.markdown], { type: 'text/markdown;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = `${sanitizeFilename(run.title || `daily-report-${run.id}`)}.md`;
  anchor.click();
  URL.revokeObjectURL(url);
}

function sanitizeFilename(value: string): string {
  return Array.from(value)
    .map((character) => (character.charCodeAt(0) < 32 ? '-' : character))
    .join('')
    .replace(/[\\/:*?"<>|]/g, '-')
    .slice(0, 100);
}

async function openSource(source: DailyReportSource): Promise<void> {
  if (source.article_id && source.feed_id) {
    store.setTopLevelView('articles');
    store.selectFeedInArticleList(source.feed_id, source.article_id);
    return;
  }
  if (source.url) await openInBrowser(source.url);
}
</script>

<template>
  <main class="daily-report-view min-w-0 flex-1 bg-bg-primary" data-testid="daily-report-view">
    <header
      class="flex min-h-16 flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3 sm:px-6"
    >
      <div class="flex min-w-0 items-center gap-3">
        <PhNewspaperClipping :size="26" class="shrink-0 text-accent" weight="duotone" />
        <div class="min-w-0">
          <h1 class="truncate text-lg font-semibold">{{ t('dailyReport.title') }}</h1>
          <p class="truncate text-xs text-text-secondary">
            <template v-if="status.is_running">
              {{ t('dailyReport.header.generating', { progress: Math.round(status.progress) }) }}
            </template>
            <template v-else-if="status.next_scheduled_at">
              {{ t('dailyReport.header.nextRun', { time: formatDate(status.next_scheduled_at) }) }}
            </template>
            <template v-else>{{ t('dailyReport.header.notScheduled') }}</template>
          </p>
        </div>
      </div>
      <div class="flex items-center gap-2">
        <button
          class="report-action secondary"
          :title="t('dailyReport.config.title')"
          @click="showConfig = true"
        >
          <PhGear :size="19" /><span class="hidden sm:inline">{{
            t('dailyReport.action.settings')
          }}</span>
        </button>
        <button
          class="report-action primary"
          :disabled="previewing || status.is_running"
          @click="openGeneratePreview"
        >
          <PhCircleNotch v-if="previewing" :size="19" class="animate-spin" />
          <PhPlay v-else :size="19" weight="fill" />
          <span>{{ t('dailyReport.action.generate') }}</span>
        </button>
      </div>
    </header>

    <button
      v-if="status.requires_feed_selection"
      class="mx-4 mt-3 flex items-center gap-3 rounded-xl border border-amber-400/40 bg-amber-500/10 px-4 py-3 text-left text-sm text-amber-700 dark:text-amber-300 sm:mx-6"
      data-testid="daily-report-feed-selection-warning"
      @click="showConfig = true"
    >
      <PhWarningCircle :size="21" class="shrink-0" />
      <span class="min-w-0 flex-1">{{ t('dailyReport.config.feedSelectionExpired') }}</span>
      <span class="font-semibold">{{ t('dailyReport.action.settings') }}</span>
    </button>

    <div class="report-workspace">
      <aside :class="['report-list-pane', { 'mobile-hidden': mobileDetail }]">
        <div class="flex items-center gap-2 border-b border-border p-3">
          <select
            v-model="historyStatus"
            class="report-select"
            :aria-label="t('dailyReport.filter.label')"
          >
            <option v-for="item in statuses" :key="item || 'all'" :value="item">
              {{ item ? statusLabel(item) : t('dailyReport.filter.all') }}
            </option>
          </select>
          <span class="ml-auto text-xs text-text-secondary">
            {{ t('dailyReport.history.total', { count: historyTotal }) }}
          </span>
        </div>

        <div v-if="loadingHistory" class="p-4 space-y-3">
          <div
            v-for="item in 5"
            :key="item"
            class="h-24 animate-pulse rounded-xl bg-bg-tertiary"
          ></div>
        </div>
        <div
          v-else-if="history.length === 0"
          class="flex h-full flex-col items-center justify-center p-8 text-center"
        >
          <PhArticle :size="42" class="mb-3 text-text-secondary" />
          <h2 class="font-semibold">{{ t('dailyReport.empty.title') }}</h2>
          <p class="mt-1 max-w-xs text-sm leading-6 text-text-secondary">
            {{ t('dailyReport.empty.description') }}
          </p>
        </div>
        <div v-else class="min-h-0 flex-1 overflow-y-auto p-2">
          <button
            v-for="run in history"
            :key="run.id"
            :class="['report-list-item', { active: run.id === selectedRunId }]"
            @click="selectReport(run.id)"
          >
            <div class="flex items-start justify-between gap-3">
              <strong :class="['line-clamp-2 text-left text-sm', { 'font-bold': !run.is_read }]">
                {{ run.title || t('dailyReport.untitled') }}
              </strong>
              <span :class="['report-status', statusClass(run.status)]">{{
                statusLabel(run.status)
              }}</span>
            </div>
            <p class="mt-2 flex items-center gap-1 text-xs text-text-secondary">
              <PhClock :size="14" />{{ formatDate(run.period_end) }}
              <span v-if="run.generation_mode === 'local'" class="report-mode-local">
                {{ t('dailyReport.mode.local') }}
              </span>
            </p>
            <div
              v-if="['queued', 'refreshing', 'generating'].includes(run.status)"
              class="mt-2 h-1 overflow-hidden rounded bg-bg-tertiary"
            >
              <div
                class="h-full bg-accent transition-all"
                :style="{ width: `${Math.max(2, runProgress(run))}%` }"
              ></div>
            </div>
          </button>
        </div>

        <div class="flex items-center justify-between border-t border-border p-3 text-sm">
          <button
            class="pager-button"
            :disabled="historyPage <= 1"
            @click="changePage(historyPage - 1)"
          >
            <PhCaretLeft :size="17" />
          </button>
          <span>{{ historyPage }} / {{ totalPages }}</span>
          <button
            class="pager-button"
            :disabled="historyPage >= totalPages"
            @click="changePage(historyPage + 1)"
          >
            <PhCaretRight :size="17" />
          </button>
        </div>
      </aside>

      <section :class="['report-detail-pane', { 'mobile-hidden': !mobileDetail }]">
        <div
          v-if="loadingDetail"
          class="flex h-full items-center justify-center text-text-secondary"
        >
          <PhCircleNotch :size="28" class="animate-spin" />
        </div>
        <div
          v-else-if="!selectedDetail"
          class="flex h-full flex-col items-center justify-center p-8 text-center text-text-secondary"
        >
          <PhNewspaperClipping :size="48" class="mb-3" />
          <p>{{ t('dailyReport.detail.select') }}</p>
        </div>
        <div v-else class="flex h-full min-h-0 flex-col">
          <div class="flex flex-wrap items-center gap-2 border-b border-border px-4 py-3 sm:px-6">
            <button
              class="mr-1 rounded-lg p-2 hover:bg-bg-tertiary lg:hidden"
              @click="mobileDetail = false"
            >
              <PhArrowLeft :size="19" />
            </button>
            <span :class="['report-status', statusClass(selectedDetail.run.status)]">
              {{ statusLabel(selectedDetail.run.status) }}
            </span>
            <span class="text-xs text-text-secondary">
              {{
                t('dailyReport.detail.articleCount', { count: selectedDetail.run.article_count })
              }}
            </span>
            <div class="ml-auto flex items-center gap-1">
              <button
                class="detail-icon"
                :title="
                  selectedDetail.run.is_read
                    ? t('dailyReport.action.markUnread')
                    : t('dailyReport.action.markRead')
                "
                @click="toggleSelectedRead"
              >
                <PhEnvelopeOpen v-if="selectedDetail.run.is_read" :size="19" />
                <PhEnvelopeSimple v-else :size="19" />
              </button>
              <button
                class="detail-icon"
                :title="t('dailyReport.action.copy')"
                :disabled="!selectedDetail.run.markdown"
                @click="copyMarkdown"
              >
                <PhCopy :size="19" />
              </button>
              <button
                class="detail-icon"
                :title="t('dailyReport.action.download')"
                :disabled="!selectedDetail.run.markdown"
                @click="downloadMarkdown"
              >
                <PhDownloadSimple :size="19" />
              </button>
              <button
                v-if="
                  !canRecoverAI &&
                  ['failed', 'partial', 'interrupted'].includes(selectedDetail.run.status)
                "
                class="detail-icon"
                :title="t('common.retry')"
                :disabled="retryingRunId === selectedDetail.run.id"
                @click="handleRetry(selectedDetail.run)"
              >
                <PhCircleNotch
                  v-if="retryingRunId === selectedDetail.run.id"
                  :size="19"
                  class="animate-spin"
                />
                <PhRepeat v-else :size="19" />
              </button>
              <button
                class="detail-icon danger"
                :title="t('common.action.remove')"
                @click="handleDelete(selectedDetail.run)"
              >
                <PhTrash :size="19" />
              </button>
            </div>
          </div>

          <article class="min-h-0 flex-1 overflow-y-auto px-5 py-6 sm:px-8 lg:px-12">
            <div class="mx-auto max-w-4xl">
              <h2 class="text-2xl font-bold leading-tight sm:text-3xl">
                {{ selectedDetail.run.title || t('dailyReport.untitled') }}
              </h2>
              <p class="mt-2 text-sm text-text-secondary">
                {{ formatDate(selectedDetail.run.period_start) }} —
                {{ formatDate(selectedDetail.run.period_end) }}
                <span
                  v-if="selectedDetail.run.generation_mode === 'local'"
                  class="report-mode-local ml-2"
                >
                  {{ t('dailyReport.mode.local') }}
                </span>
              </p>

              <div
                v-if="isSelectedRunActive"
                class="report-progress-card"
                data-testid="daily-report-detail-progress"
              >
                <div class="flex items-start gap-3">
                  <PhCircleNotch :size="24" class="mt-0.5 shrink-0 animate-spin text-accent" />
                  <div class="min-w-0 flex-1">
                    <div class="flex items-center justify-between gap-4">
                      <h3 class="font-semibold">{{ t('dailyReport.progress.title') }}</h3>
                      <strong data-testid="daily-report-detail-progress-value">
                        {{ selectedProgress }}%
                      </strong>
                    </div>
                    <p
                      class="mt-1 text-sm text-text-secondary"
                      data-testid="daily-report-progress-step"
                    >
                      {{ selectedStepLabel }}
                    </p>
                    <div class="mt-4 h-2 overflow-hidden rounded-full bg-bg-tertiary">
                      <div
                        class="h-full rounded-full bg-accent transition-all duration-300"
                        :style="{ width: `${Math.max(2, selectedProgress)}%` }"
                      ></div>
                    </div>
                  </div>
                </div>
                <dl class="mt-5 grid grid-cols-1 gap-3 text-sm sm:grid-cols-3">
                  <div class="progress-metric">
                    <dt>{{ t('dailyReport.progress.articles') }}</dt>
                    <dd>{{ formatNumber(selectedDetail.run.article_count) }}</dd>
                  </div>
                  <div class="progress-metric">
                    <dt>{{ t('dailyReport.progress.inputTokens') }}</dt>
                    <dd>{{ formatNumber(selectedDetail.run.input_tokens) }}</dd>
                  </div>
                  <div class="progress-metric">
                    <dt>{{ t('dailyReport.progress.outputTokens') }}</dt>
                    <dd>{{ formatNumber(selectedDetail.run.output_tokens) }}</dd>
                  </div>
                </dl>
              </div>

              <div
                v-if="!isSelectedRunActive && selectedDetail.run.error"
                class="mt-5 rounded-xl border border-red-400/30 bg-red-500/10 p-4 text-sm text-red-600 dark:text-red-300"
              >
                <div class="flex gap-3">
                  <PhWarningCircle :size="20" class="shrink-0" />
                  <div class="min-w-0">
                    <p>{{ displayError }}</p>
                    <p v-if="selectedDetail.run.failure_code" class="mt-1 text-xs opacity-80">
                      {{
                        t('dailyReport.detail.failureCode', {
                          code: selectedDetail.run.failure_code,
                        })
                      }}
                    </p>
                  </div>
                </div>
                <div v-if="canRecoverAI" class="mt-4 flex flex-wrap gap-2 pl-8">
                  <button
                    class="report-action primary"
                    :disabled="retryingRunId !== null || fallbackRunId !== null"
                    @click="
                      handleRetry(
                        selectedDetail.run,
                        retryAction === 'restart' ? 'restart' : 'resume'
                      )
                    "
                  >
                    <PhCircleNotch
                      v-if="retryingRunId === selectedDetail.run.id"
                      :size="17"
                      class="animate-spin"
                    />
                    <PhRepeat v-else :size="17" />
                    {{
                      retryAction === 'restart'
                        ? t('dailyReport.action.restartAI')
                        : t('dailyReport.action.resumeAI')
                    }}
                  </button>
                  <button
                    class="report-action secondary"
                    :disabled="retryingRunId !== null || fallbackRunId !== null"
                    @click="handleLocalFallback(selectedDetail.run)"
                  >
                    <PhCircleNotch
                      v-if="fallbackRunId === selectedDetail.run.id"
                      :size="17"
                      class="animate-spin"
                    />
                    <PhArticle v-else :size="17" />
                    {{ t('dailyReport.action.useLocalFallback') }}
                  </button>
                </div>
              </div>

              <div
                v-if="!isSelectedRunActive && selectedDetail.run.status === 'no_content'"
                class="mt-10 text-center text-text-secondary"
              >
                <PhCheckCircle :size="42" class="mx-auto mb-3" />
                <p>{{ t('dailyReport.detail.noContent') }}</p>
              </div>

              <div v-else-if="!isSelectedRunActive && sections.length" class="mt-8 space-y-10">
                <section v-for="section in sections" :key="section.id" class="report-section">
                  <h3>{{ section.title }}</h3>
                  <p>{{ section.summary }}</p>
                  <div v-if="section.source_ids?.length" class="mt-4 flex flex-wrap gap-2">
                    <button
                      v-for="sourceId in section.source_ids"
                      :key="sourceId"
                      class="source-chip"
                      :disabled="!sourcesById.get(sourceId)"
                      :title="sourcesById.get(sourceId)?.title"
                      @click="sourcesById.get(sourceId) && openSource(sourcesById.get(sourceId)!)"
                    >
                      [{{ sourceId }}]
                      {{
                        sourcesById.get(sourceId)?.title ||
                        t('dailyReport.detail.sourceUnavailable')
                      }}
                    </button>
                  </div>
                </section>
              </div>
              <p v-else-if="!isSelectedRunActive" class="mt-10 text-center text-text-secondary">
                {{ t('dailyReport.detail.contentUnavailable') }}
              </p>
            </div>
          </article>
        </div>
      </section>
    </div>

    <DailyReportConfigModal v-if="showConfig" @close="showConfig = false" @saved="fetchStatus" />
    <BaseModal
      v-if="preview"
      size="sm"
      :title="t('dailyReport.preview.title')"
      :loading="starting"
      body-class="p-5"
      @close="preview = null"
    >
      <div class="space-y-4 text-sm">
        <div class="rounded-xl bg-bg-secondary p-4">
          <div class="text-text-secondary">{{ t('dailyReport.preview.period') }}</div>
          <div class="mt-1 font-medium">
            {{ formatDate(preview.period_start) }} — {{ formatDate(preview.period_end) }}
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div class="rounded-xl bg-bg-secondary p-4">
            <div class="text-text-secondary">{{ t('dailyReport.preview.articles') }}</div>
            <div class="mt-1 text-xl font-semibold">{{ preview.article_count }}</div>
          </div>
          <div class="rounded-xl bg-bg-secondary p-4">
            <div class="text-text-secondary">{{ t('dailyReport.preview.batches') }}</div>
            <div class="mt-1 text-xl font-semibold">{{ preview.estimated_batches }}</div>
          </div>
        </div>
        <p class="text-xs leading-5 text-text-secondary">{{ t('dailyReport.preview.costHint') }}</p>
        <div class="flex justify-end gap-2">
          <button class="report-action secondary" :disabled="starting" @click="preview = null">
            {{ t('common.cancel') }}
          </button>
          <button class="report-action primary" :disabled="starting" @click="confirmGenerate">
            <PhCircleNotch v-if="starting" :size="18" class="animate-spin" />
            {{ t('dailyReport.preview.confirm') }}
          </button>
        </div>
      </div>
    </BaseModal>
  </main>
</template>

<style scoped>
@reference "../../style.css";
.daily-report-view {
  display: flex;
  height: 100%;
  min-width: 0;
  flex-direction: column;
}
.report-workspace {
  display: grid;
  min-height: 0;
  flex: 1;
  grid-template-columns: minmax(280px, 360px) minmax(0, 1fr);
}
.report-list-pane,
.report-detail-pane {
  min-width: 0;
  min-height: 0;
}
.report-list-pane {
  @apply flex flex-col border-r border-border bg-bg-secondary;
}
.report-detail-pane {
  @apply bg-bg-primary;
}
.report-action {
  @apply inline-flex items-center justify-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50;
}
.report-action.primary {
  @apply bg-accent text-white hover:bg-accent-hover;
}
.report-action.secondary {
  @apply border border-border bg-bg-secondary text-text-primary hover:bg-bg-tertiary;
}
.report-select {
  @apply min-w-0 rounded-lg border border-border bg-bg-primary px-3 py-2 text-sm outline-none focus:border-accent;
}
.report-list-item {
  @apply mb-2 block w-full rounded-xl border border-transparent bg-bg-primary p-3 text-left transition-colors hover:bg-bg-tertiary;
}
.report-list-item.active {
  @apply border-accent/40 bg-accent/10;
}
.report-status {
  @apply inline-flex shrink-0 items-center rounded-full px-2 py-1 text-[10px] font-semibold;
}
.report-mode-local {
  @apply inline-flex shrink-0 rounded-full bg-amber-500/10 px-2 py-0.5 text-[10px] font-semibold text-amber-700 dark:text-amber-300;
}
.report-progress-card {
  @apply mt-6 rounded-2xl border border-accent/25 bg-accent/5 p-5;
}
.progress-metric {
  @apply rounded-xl bg-bg-secondary px-4 py-3;
}
.progress-metric dt {
  @apply text-xs text-text-secondary;
}
.progress-metric dd {
  @apply mt-1 font-semibold tabular-nums text-text-primary;
}
.status-success {
  @apply bg-green-500/10 text-green-600 dark:text-green-300;
}
.status-warning {
  @apply bg-amber-500/10 text-amber-600 dark:text-amber-300;
}
.status-error {
  @apply bg-red-500/10 text-red-600 dark:text-red-300;
}
.status-neutral {
  @apply bg-bg-tertiary text-text-secondary;
}
.status-running {
  @apply bg-accent/10 text-accent;
}
.pager-button,
.detail-icon {
  @apply inline-flex h-9 w-9 items-center justify-center rounded-lg text-text-secondary transition-colors hover:bg-bg-tertiary hover:text-text-primary disabled:opacity-40;
}
.detail-icon.danger {
  @apply hover:bg-red-500/10 hover:text-red-500;
}
.report-section h3 {
  @apply text-xl font-semibold leading-7;
}
.report-section p {
  @apply mt-3 whitespace-pre-wrap text-[15px] leading-8 text-text-primary;
}
.source-chip {
  @apply max-w-full truncate rounded-full border border-border bg-bg-secondary px-3 py-1.5 text-xs text-text-secondary hover:border-accent hover:text-accent disabled:opacity-40;
}
@media (max-width: 900px) {
  .report-workspace {
    display: block;
  }
  .report-list-pane,
  .report-detail-pane {
    height: 100%;
  }
  .report-list-pane.mobile-hidden,
  .report-detail-pane.mobile-hidden {
    display: none;
  }
}
</style>
