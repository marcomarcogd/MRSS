<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import {
  PhArrowsDownUp,
  PhBell,
  PhCheck,
  PhClock,
  PhMagicWand,
  PhMagnifyingGlass,
  PhPlus,
  PhTrash,
} from '@phosphor-icons/vue';
import BaseModal from '@/components/common/BaseModal.vue';
import ModalFooter from '@/components/common/ModalFooter.vue';
import ToggleControl from '@/components/settings/base/SettingControl/ToggleControl.vue';
import { useAppStore } from '@/stores/app';
import { useAIProfiles } from '@/composables/ai/useAIProfiles';
import { DailyReportAPIError, useDailyReports } from '@/composables/dailyReport/useDailyReports';
import type { DailyReportConfig, DailyReportOutlineItem } from '@/types/dailyReport';
import { DEFAULT_DAILY_REPORT_CONFIG } from '@/types/dailyReport';

const emit = defineEmits<{ close: []; saved: [] }>();
const { t } = useI18n();
const store = useAppStore();
const { profiles, fetchProfiles } = useAIProfiles();
const {
  config,
  cloudProcessing,
  loadingConfig,
  savingConfig,
  fetchConfig,
  fetchStatus,
  saveConfig,
  optimizeOutline,
  authorizeNotifications,
  fetchCloudProcessing,
  updateCloudProcessingConsent,
  promptCloudConsent,
  openCloudConsentPrompt,
  consentDismissalSequence,
} = useDailyReports();

const form = ref<DailyReportConfig>({ ...DEFAULT_DAILY_REPORT_CONFIG });
const search = ref('');
const optimizing = ref(false);
const outlineDraft = ref<DailyReportOutlineItem[] | null>(null);
const draggedIndex = ref<number | null>(null);
const pendingConsentSave = ref(false);
const titleTemplateVariables = ['{{date}}', '{{start_time}}', '{{end_time}}', '{{article_count}}'];

const groupedFeeds = computed(() => {
  const query = search.value.trim().toLocaleLowerCase();
  const groups = new Map<string, typeof store.feeds>();
  store.feeds.forEach((feed) => {
    const haystack = `${feed.title} ${feed.url} ${feed.category || ''}`.toLocaleLowerCase();
    if (query && !haystack.includes(query)) return;
    const category = feed.category || t('dailyReport.config.uncategorized');
    const existing = groups.get(category) || [];
    existing.push(feed);
    groups.set(category, existing);
  });
  return [...groups.entries()];
});

const visibleFeedIds = computed(() =>
  groupedFeeds.value.flatMap(([, feeds]) => feeds.map((f) => f.id))
);
const allVisibleSelected = computed(
  () =>
    visibleFeedIds.value.length > 0 &&
    visibleFeedIds.value.every((id) => form.value.feed_ids.includes(id))
);
const aiProfileChanged = computed(() => form.value.ai_profile_id !== config.value.ai_profile_id);
const selectedAIProfileName = computed(
  () =>
    profiles.value.find((profile) => profile.id === form.value.ai_profile_id)?.name ||
    t('dailyReport.config.defaultProfile')
);

onMounted(async () => {
  await Promise.allSettled([
    fetchConfig(),
    fetchCloudProcessing(),
    fetchProfiles(),
    store.fetchFeeds(),
  ]);
  form.value = cloneConfig(config.value);
  if (form.value.outline.length === 0) {
    form.value.outline = [
      {
        id: 'overview',
        title: t('dailyReport.config.defaultSectionTitle'),
        instruction: t('dailyReport.config.defaultSectionInstruction'),
      },
    ];
  }
});

watch(consentDismissalSequence, async () => {
  if (!pendingConsentSave.value) return;
  pendingConsentSave.value = false;
  try {
    await Promise.all([fetchConfig(), fetchStatus()]);
    form.value = cloneConfig(config.value);
    window.showToast(t('dailyReport.toast.configPausedForConsent'), 'warning');
  } catch (error) {
    console.error('Failed to refresh paused daily report config:', error);
  }
});

function cloneConfig(value: DailyReportConfig): DailyReportConfig {
  return {
    ...value,
    feed_ids: [...value.feed_ids],
    outline: value.outline.map((item) => ({ ...item })),
  };
}

function toggleFeed(id: number, checked: boolean): void {
  const next = new Set(form.value.feed_ids);
  checked ? next.add(id) : next.delete(id);
  form.value.feed_ids = [...next];
}

function toggleVisibleFeeds(): void {
  const next = new Set(form.value.feed_ids);
  if (allVisibleSelected.value) visibleFeedIds.value.forEach((id) => next.delete(id));
  else visibleFeedIds.value.forEach((id) => next.add(id));
  form.value.feed_ids = [...next];
}

function addOutlineItem(): void {
  if (form.value.outline.length >= 12) return;
  form.value.outline.push({
    id: crypto.randomUUID?.() || `section-${Date.now()}`,
    title: '',
    instruction: '',
  });
}

function removeOutlineItem(index: number): void {
  if (form.value.outline.length <= 1) return;
  form.value.outline.splice(index, 1);
}

function handleDragStart(index: number): void {
  draggedIndex.value = index;
}

function handleDrop(index: number): void {
  if (draggedIndex.value === null || draggedIndex.value === index) return;
  const [item] = form.value.outline.splice(draggedIndex.value, 1);
  form.value.outline.splice(index, 0, item);
  draggedIndex.value = null;
}

async function requestOutlineDraft(): Promise<void> {
  if (optimizing.value) return;
  if (form.value.ai_profile_id !== config.value.ai_profile_id) {
    window.showToast(t('dailyReport.config.saveProfileBeforeOptimize'), 'warning');
    return;
  }
  optimizing.value = true;
  try {
    outlineDraft.value = await optimizeOutline({
      focus: form.value.focus,
      language: form.value.language,
      ai_profile_id: form.value.ai_profile_id,
    });
    if (!outlineDraft.value.length) throw new Error(t('dailyReport.config.emptyOutline'));
  } catch (error) {
    if (await promptCloudConsent(error, requestOutlineDraft)) return;
    console.error('Failed to optimize daily report outline:', error);
    const supportedCodes = new Set([
      'timeout',
      'rate_limited',
      'provider_unavailable',
      'authentication_failed',
      'provider_rejected_request',
      'empty_response',
      'invalid_json',
      'schema_invalid',
      'network_error',
      'request_failed',
    ]);
    const code = error instanceof DailyReportAPIError ? error.code : undefined;
    window.showToast(
      code && supportedCodes.has(code)
        ? t(`dailyReport.config.outlineErrors.${code}`)
        : t('dailyReport.toast.optimizeFailed'),
      'error'
    );
  } finally {
    optimizing.value = false;
  }
}

function confirmOutlineDraft(): void {
  if (!outlineDraft.value) return;
  form.value.outline = outlineDraft.value.map((item) => ({ ...item }));
  outlineDraft.value = null;
}

async function toggleSystemNotification(enabled: boolean): Promise<void> {
  if (!enabled) {
    form.value.system_notification = false;
    return;
  }
  try {
    const result = await authorizeNotifications();
    form.value.system_notification = result === 'authorized';
    if (result !== 'authorized')
      window.showToast(t(`dailyReport.notification.${result}`), 'warning');
  } catch (error) {
    console.error('Failed to authorize notifications:', error);
    form.value.system_notification = false;
    window.showToast(t('dailyReport.toast.notificationFailed'), 'error');
  }
}

function validate(): boolean {
  if (form.value.feed_scope === 'selected' && form.value.feed_ids.length === 0) {
    window.showToast(t('dailyReport.config.selectFeedRequired'), 'warning');
    return false;
  }
  if (form.value.outline.length < 1 || form.value.outline.length > 12) {
    window.showToast(t('dailyReport.config.outlineCountError'), 'warning');
    return false;
  }
  if (form.value.outline.some((item) => !item.title.trim() || item.instruction.length > 500)) {
    window.showToast(t('dailyReport.config.outlineInvalid'), 'warning');
    return false;
  }
  const allowedVariables = new Set(titleTemplateVariables);
  const variables = form.value.title_template.match(/{{[^{}]*}}/g) || [];
  const remainingBraces = form.value.title_template.replace(/{{[^{}]*}}/g, '');
  if (
    variables.some((variable) => !allowedVariables.has(variable)) ||
    remainingBraces.includes('{') ||
    remainingBraces.includes('}')
  ) {
    window.showToast(t('dailyReport.config.titleTemplateInvalid'), 'warning');
    return false;
  }
  return true;
}

async function submit(): Promise<void> {
  if (!validate()) return;
  try {
    if (form.value.enabled && !config.value.enabled && form.value.system_notification) {
      const authorization = await authorizeNotifications();
      form.value.system_notification = authorization === 'authorized';
      if (authorization !== 'authorized') {
        window.showToast(t(`dailyReport.notification.${authorization}`), 'warning');
      }
    }
    await saveConfig(form.value);
    window.showToast(t('dailyReport.toast.configSaved'), 'success');
    emit('saved');
    emit('close');
  } catch (error) {
    if (await promptCloudConsent(error, submit)) {
      pendingConsentSave.value = true;
      return;
    }
    console.error('Failed to save daily report config:', error);
    window.showToast(t('dailyReport.toast.configSaveFailed'), 'error');
  }
}

async function revokeCloudProcessing(): Promise<void> {
  const confirmed = await window.showConfirm({
    title: t('dailyReport.consent.revokeTitle'),
    message: t('dailyReport.consent.revokeConfirm'),
    confirmText: t('dailyReport.consent.revoke'),
    cancelText: t('common.cancel'),
    isDanger: true,
  });
  if (!confirmed) return;
  try {
    await updateCloudProcessingConsent('revoke', { refreshConfig: false });
    // Revoking pauses scheduled reports but must not replace unsaved profile,
    // outline, feed, or notification edits in this modal.
    form.value.enabled = false;
    window.showToast(t('dailyReport.consent.revokedToast'), 'success');
  } catch (error) {
    console.error('Failed to revoke cloud processing consent:', error);
    window.showToast(t('dailyReport.consent.revokeFailed'), 'error');
  }
}
</script>

<template>
  <BaseModal
    :title="t('dailyReport.config.title')"
    size="4xl"
    height="full"
    max-height="92vh"
    :loading="savingConfig"
    show-footer
    body-class="p-4 sm:p-6"
    @close="emit('close')"
  >
    <div v-if="loadingConfig" class="flex h-48 items-center justify-center text-text-secondary">
      {{ t('common.state.loading') }}
    </div>
    <div v-else class="space-y-6" data-testid="daily-report-config">
      <section class="report-setting-card">
        <div class="flex items-start justify-between gap-4">
          <div>
            <h4 class="report-setting-title">{{ t('dailyReport.config.enable') }}</h4>
            <p class="report-setting-help">{{ t('dailyReport.config.enableDescription') }}</p>
          </div>
          <ToggleControl v-model="form.enabled" />
        </div>
      </section>

      <section class="report-setting-card grid gap-4 sm:grid-cols-2">
        <label class="report-field">
          <span><PhClock :size="18" />{{ t('dailyReport.config.scheduleTime') }}</span>
          <input v-model="form.schedule_time" type="time" class="report-input" />
        </label>
        <label class="report-field">
          <span>{{ t('dailyReport.config.language') }}</span>
          <select v-model="form.language" class="report-input">
            <option value="auto">{{ t('dailyReport.config.followApp') }}</option>
            <option value="zh-CN">{{ t('dailyReport.config.chinese') }}</option>
            <option value="en">{{ t('dailyReport.config.english') }}</option>
          </select>
        </label>
        <label class="report-field">
          <span>{{ t('dailyReport.config.aiProfile') }}</span>
          <select
            v-model="form.ai_profile_id"
            class="report-input"
            data-testid="daily-report-profile-select"
          >
            <option :value="null">{{ t('dailyReport.config.defaultProfile') }}</option>
            <option v-for="profile in profiles" :key="profile.id" :value="profile.id">
              {{ profile.name }}
            </option>
          </select>
        </label>
        <label class="report-field">
          <span>{{ t('dailyReport.config.titleTemplate') }}</span>
          <input v-model="form.title_template" maxlength="80" class="report-input" />
          <small class="flex flex-wrap items-center gap-1">
            {{ t('dailyReport.config.titleVariables') }}
            <code v-for="variable in titleTemplateVariables" :key="variable">{{ variable }}</code>
          </small>
        </label>
        <div
          class="sm:col-span-2 rounded-lg border border-amber-400/30 bg-amber-500/10 p-3 text-xs leading-5 text-text-secondary"
        >
          {{ t('dailyReport.config.aiPrivacyNotice') }}
        </div>
        <div
          class="sm:col-span-2 rounded-xl border border-border bg-bg-primary p-4"
          data-testid="daily-report-consent-status"
        >
          <div class="flex flex-wrap items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="flex items-center gap-2 text-sm font-semibold">
                <span
                  :class="[
                    'h-2.5 w-2.5 rounded-full',
                    aiProfileChanged
                      ? 'bg-amber-500'
                      : !cloudProcessing.destination
                        ? 'bg-text-secondary'
                        : cloudProcessing.accepted
                          ? 'bg-green-500'
                          : 'bg-amber-500',
                  ]"
                ></span>
                <span v-if="aiProfileChanged">{{ t('dailyReport.consent.profileChanged') }}</span>
                <span v-else-if="!cloudProcessing.destination">{{
                  t('dailyReport.consent.localOnly')
                }}</span>
                <span v-else-if="cloudProcessing.accepted">{{
                  t('dailyReport.consent.authorized')
                }}</span>
                <span v-else>{{ t('dailyReport.consent.authorizationRequired') }}</span>
              </div>
              <template v-if="aiProfileChanged">
                <p class="mt-2 truncate text-xs text-text-secondary">
                  {{ selectedAIProfileName }}
                </p>
                <p class="mt-2 text-xs leading-5 text-amber-600 dark:text-amber-300">
                  {{ t('dailyReport.consent.profileChangedDescription') }}
                </p>
              </template>
              <template v-else-if="cloudProcessing.destination">
                <p class="mt-2 truncate text-xs text-text-secondary">
                  {{ cloudProcessing.destination.profile_name }} ·
                  <span class="font-mono">{{ cloudProcessing.destination.endpoint }}</span>
                </p>
                <p
                  v-if="!cloudProcessing.accepted"
                  class="mt-2 text-xs leading-5 text-amber-600 dark:text-amber-300"
                >
                  {{ t('dailyReport.consent.pausedUntilAuthorized') }}
                </p>
              </template>
              <p v-else class="mt-2 text-xs text-text-secondary">
                {{ t('dailyReport.consent.localOnlyDescription') }}
              </p>
            </div>
            <button
              v-if="!aiProfileChanged && cloudProcessing.destination && cloudProcessing.accepted"
              class="report-secondary-button text-red-600 dark:text-red-300"
              type="button"
              @click="revokeCloudProcessing"
            >
              {{ t('dailyReport.consent.revoke') }}
            </button>
            <button
              v-else-if="!aiProfileChanged && cloudProcessing.destination"
              class="report-primary-button"
              type="button"
              @click="openCloudConsentPrompt()"
            >
              {{ t('dailyReport.consent.reviewAndAuthorize') }}
            </button>
          </div>
        </div>
      </section>

      <section class="report-setting-card space-y-4">
        <div>
          <h4 class="report-setting-title">{{ t('dailyReport.config.feeds') }}</h4>
          <p class="report-setting-help">{{ t('dailyReport.config.feedsDescription') }}</p>
        </div>
        <div class="flex gap-4 text-sm">
          <label class="flex items-center gap-2">
            <input v-model="form.feed_scope" type="radio" value="all" />
            {{ t('dailyReport.config.allFeeds') }}
          </label>
          <label class="flex items-center gap-2">
            <input v-model="form.feed_scope" type="radio" value="selected" />
            {{ t('dailyReport.config.selectedFeeds') }}
          </label>
        </div>
        <div
          v-if="form.feed_scope === 'selected'"
          class="rounded-lg border border-border overflow-hidden"
        >
          <div class="flex items-center gap-2 border-b border-border bg-bg-tertiary p-3">
            <PhMagnifyingGlass :size="18" class="text-text-secondary" />
            <input
              v-model="search"
              class="min-w-0 flex-1 bg-transparent outline-none"
              :placeholder="t('dailyReport.config.searchFeeds')"
            />
            <button class="text-sm text-accent" type="button" @click="toggleVisibleFeeds">
              {{
                allVisibleSelected
                  ? t('common.action.deselectAll')
                  : t('dailyReport.config.selectVisible')
              }}
            </button>
          </div>
          <div class="max-h-60 overflow-y-auto p-2">
            <div v-for="[category, feeds] in groupedFeeds" :key="category" class="mb-3 last:mb-0">
              <div class="px-2 py-1 text-xs font-semibold text-text-secondary">{{ category }}</div>
              <label
                v-for="feed in feeds"
                :key="feed.id"
                class="flex cursor-pointer items-center gap-3 rounded-lg px-2 py-2 hover:bg-bg-tertiary"
              >
                <input
                  type="checkbox"
                  :checked="form.feed_ids.includes(feed.id)"
                  @change="toggleFeed(feed.id, ($event.target as HTMLInputElement).checked)"
                />
                <div class="min-w-0 flex-1">
                  <div class="truncate text-sm font-medium">{{ feed.title }}</div>
                  <div class="truncate text-xs text-text-secondary">{{ feed.url }}</div>
                </div>
              </label>
            </div>
          </div>
        </div>
        <label class="flex items-center justify-between gap-4 rounded-lg bg-bg-tertiary p-3">
          <div>
            <div class="text-sm font-medium">{{ t('dailyReport.config.includeHidden') }}</div>
            <div class="text-xs text-text-secondary">
              {{ t('dailyReport.config.includeHiddenDescription') }}
            </div>
          </div>
          <ToggleControl v-model="form.include_hidden" />
        </label>
      </section>

      <section class="report-setting-card space-y-4">
        <div class="flex flex-wrap items-start justify-between gap-3">
          <div>
            <h4 class="report-setting-title">{{ t('dailyReport.config.focusAndOutline') }}</h4>
            <p class="report-setting-help">{{ t('dailyReport.config.focusDescription') }}</p>
          </div>
          <button
            class="report-secondary-button"
            type="button"
            :disabled="optimizing"
            data-testid="daily-report-optimize-outline"
            @click="requestOutlineDraft"
          >
            <PhMagicWand :size="18" />
            {{
              optimizing
                ? t('dailyReport.config.optimizing')
                : t('dailyReport.config.optimizeOutline')
            }}
          </button>
        </div>
        <textarea
          v-model="form.focus"
          maxlength="2000"
          rows="4"
          class="report-input resize-y"
          :placeholder="t('dailyReport.config.focusPlaceholder')"
        ></textarea>

        <div v-if="outlineDraft" class="rounded-xl border border-accent/40 bg-accent/5 p-4">
          <div class="mb-3 flex items-center justify-between gap-3">
            <strong>{{ t('dailyReport.config.outlineDraft') }}</strong>
            <div class="flex gap-2">
              <button class="report-ghost-button" type="button" @click="outlineDraft = null">
                {{ t('common.cancel') }}
              </button>
              <button class="report-primary-button" type="button" @click="confirmOutlineDraft">
                <PhCheck :size="16" />{{ t('dailyReport.config.useDraft') }}
              </button>
            </div>
          </div>
          <ol class="list-decimal space-y-2 pl-5 text-sm">
            <li v-for="item in outlineDraft" :key="item.id">
              <span class="font-medium">{{ item.title }}</span>
              <span class="text-text-secondary"> — {{ item.instruction }}</span>
            </li>
          </ol>
        </div>

        <div class="space-y-2">
          <div
            v-for="(item, index) in form.outline"
            :key="item.id"
            class="grid grid-cols-[auto_minmax(0,1fr)_auto] items-start gap-2 rounded-lg border border-border p-3"
            draggable="true"
            @dragstart="handleDragStart(index)"
            @dragover.prevent
            @drop="handleDrop(index)"
          >
            <PhArrowsDownUp :size="18" class="mt-2 cursor-grab text-text-secondary" />
            <div class="grid min-w-0 gap-2 sm:grid-cols-3">
              <input
                v-model="item.title"
                maxlength="80"
                class="report-input"
                :placeholder="t('dailyReport.config.sectionTitle')"
              />
              <textarea
                v-model="item.instruction"
                maxlength="500"
                rows="2"
                class="report-input resize-y sm:col-span-2"
                :placeholder="t('dailyReport.config.sectionInstruction')"
              ></textarea>
            </div>
            <button
              class="mt-1 rounded-lg p-2 text-text-secondary hover:bg-red-500/10 hover:text-red-500 disabled:opacity-40"
              type="button"
              :disabled="form.outline.length <= 1"
              :title="t('common.action.remove')"
              @click="removeOutlineItem(index)"
            >
              <PhTrash :size="18" />
            </button>
          </div>
          <button
            class="report-secondary-button"
            type="button"
            :disabled="form.outline.length >= 12"
            @click="addOutlineItem"
          >
            <PhPlus :size="18" />{{ t('dailyReport.config.addSection') }}
          </button>
        </div>
      </section>

      <section class="report-setting-card space-y-3">
        <h4 class="report-setting-title flex items-center gap-2">
          <PhBell :size="20" />{{ t('dailyReport.config.notifications') }}
        </h4>
        <label class="report-toggle-row">
          <span>{{ t('dailyReport.config.inAppNotification') }}</span>
          <ToggleControl v-model="form.in_app_notification" />
        </label>
        <label class="report-toggle-row">
          <span>{{ t('dailyReport.config.systemNotification') }}</span>
          <ToggleControl
            :model-value="form.system_notification"
            @update:model-value="toggleSystemNotification"
          />
        </label>
        <label class="report-toggle-row">
          <span>{{ t('dailyReport.config.notifyOnEmpty') }}</span>
          <ToggleControl v-model="form.notify_on_empty" />
        </label>
      </section>
    </div>

    <template #footer>
      <ModalFooter
        :secondary-button="{ label: t('common.cancel'), disabled: savingConfig }"
        :primary-button="{
          label: t('common.save'),
          loading: savingConfig,
          disabled: !!outlineDraft,
        }"
        @secondary-click="emit('close')"
        @primary-click="submit"
      />
    </template>
  </BaseModal>
</template>

<style scoped>
@reference "../../style.css";
.report-setting-card {
  @apply rounded-xl border border-border bg-bg-secondary p-4;
}
.report-setting-title {
  @apply text-base font-semibold text-text-primary;
}
.report-setting-help {
  @apply mt-1 text-xs leading-5 text-text-secondary;
}
.report-field {
  @apply flex min-w-0 flex-col gap-2 text-sm font-medium;
}
.report-field > span {
  @apply flex items-center gap-2;
}
.report-field small {
  @apply font-normal text-text-secondary;
}
.report-field small code {
  @apply rounded bg-bg-tertiary px-1 py-0.5 text-[10px];
}
.report-input {
  @apply w-full rounded-lg border border-border bg-bg-primary px-3 py-2 text-sm text-text-primary outline-none transition-colors focus:border-accent;
}
.report-primary-button,
.report-secondary-button,
.report-ghost-button {
  @apply inline-flex items-center justify-center gap-2 rounded-lg px-3 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-50;
}
.report-primary-button {
  @apply bg-accent text-white hover:bg-accent-hover;
}
.report-secondary-button {
  @apply border border-border bg-bg-primary text-text-primary hover:bg-bg-tertiary;
}
.report-ghost-button {
  @apply text-text-secondary hover:bg-bg-tertiary hover:text-text-primary;
}
.report-toggle-row {
  @apply flex items-center justify-between gap-4 rounded-lg bg-bg-tertiary p-3 text-sm;
}
</style>
