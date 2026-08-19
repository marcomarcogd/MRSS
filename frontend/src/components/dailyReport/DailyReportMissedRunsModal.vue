<script setup lang="ts">
import { ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { PhClockCounterClockwise } from '@phosphor-icons/vue';
import BaseModal from '@/components/common/BaseModal.vue';
import { useDailyReports } from '@/composables/dailyReport/useDailyReports';

const emit = defineEmits<{ close: [] }>();
const { t } = useI18n();
const { status, handleMissedRuns, promptCloudConsent } = useDailyReports();
const workingAction = ref<string | null>(null);

async function handle(action: 'latest' | 'all' | 'skip_all'): Promise<void> {
  workingAction.value = action;
  try {
    await handleMissedRuns(action);
    window.showToast(
      action === 'skip_all'
        ? t('dailyReport.toast.missedSkipped')
        : t('dailyReport.toast.backfillStarted'),
      'success'
    );
    emit('close');
  } catch (error) {
    if (action !== 'skip_all' && (await promptCloudConsent(error, () => handle(action)))) return;
    console.error('Failed to handle missed daily reports:', error);
    window.showToast(t('dailyReport.toast.missedFailed'), 'error');
  } finally {
    workingAction.value = null;
  }
}
</script>

<template>
  <BaseModal
    size="md"
    :title="t('dailyReport.missed.title')"
    :loading="!!workingAction"
    body-class="p-5"
    @close="emit('close')"
  >
    <div class="flex gap-4">
      <div
        class="flex h-11 w-11 shrink-0 items-center justify-center rounded-full bg-accent/10 text-accent"
      >
        <PhClockCounterClockwise :size="24" />
      </div>
      <div class="min-w-0">
        <p class="text-sm leading-6 text-text-primary">
          {{ t('dailyReport.missed.description', { count: status.missed_count }) }}
        </p>
        <p class="mt-1 text-xs leading-5 text-text-secondary">
          {{ t('dailyReport.missed.closeHint') }}
        </p>
      </div>
    </div>
    <div class="mt-5 grid gap-2 sm:grid-cols-3">
      <button class="missed-button primary" :disabled="!!workingAction" @click="handle('latest')">
        {{ t('dailyReport.missed.latest') }}
      </button>
      <button class="missed-button" :disabled="!!workingAction" @click="handle('all')">
        {{ t('dailyReport.missed.all') }}
      </button>
      <button class="missed-button" :disabled="!!workingAction" @click="handle('skip_all')">
        {{ t('dailyReport.missed.skipAll') }}
      </button>
    </div>
  </BaseModal>
</template>

<style scoped>
@reference "../../style.css";
.missed-button {
  @apply rounded-lg border border-border bg-bg-secondary px-3 py-2 text-sm font-medium text-text-primary transition-colors hover:bg-bg-tertiary disabled:opacity-50;
}
.missed-button.primary {
  @apply border-accent bg-accent text-white hover:bg-accent-hover;
}
</style>
