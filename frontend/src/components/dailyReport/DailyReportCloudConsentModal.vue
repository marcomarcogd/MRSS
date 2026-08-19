<script setup lang="ts">
import { ref } from 'vue';
import { useI18n } from 'vue-i18n';
import {
  PhArrowSquareOut,
  PhClockCountdown,
  PhCoins,
  PhDatabase,
  PhShieldWarning,
} from '@phosphor-icons/vue';
import BaseModal from '@/components/common/BaseModal.vue';
import ModalFooter from '@/components/common/ModalFooter.vue';
import { useDailyReports } from '@/composables/dailyReport/useDailyReports';

const emit = defineEmits<{ close: [] }>();
const { t } = useI18n();
const {
  cloudProcessing,
  consentActionRunning,
  grantCloudConsentAndRetry,
  closeCloudConsentPrompt,
} = useDailyReports();
const acknowledged = ref(false);

function close(): void {
  if (consentActionRunning.value) return;
  closeCloudConsentPrompt();
  emit('close');
}

async function grant(): Promise<void> {
  if (!acknowledged.value || consentActionRunning.value) return;
  try {
    await grantCloudConsentAndRetry();
    window.showToast(t('dailyReport.consent.grantedToast'), 'success');
  } catch (error) {
    console.error('Failed to grant cloud processing consent:', error);
    window.showToast(t('dailyReport.consent.grantFailed'), 'error');
  }
}
</script>

<template>
  <BaseModal
    size="lg"
    :title="t('dailyReport.consent.title')"
    :loading="consentActionRunning"
    show-footer
    body-class="p-5 sm:p-6"
    :z-index="80"
    @close="close"
  >
    <div data-testid="daily-report-cloud-consent" class="space-y-5">
      <div class="rounded-xl border border-amber-400/40 bg-amber-500/10 p-4">
        <div class="flex items-start gap-3">
          <PhShieldWarning :size="24" class="mt-0.5 shrink-0 text-amber-600 dark:text-amber-300" />
          <div>
            <h4 class="font-semibold text-text-primary">
              {{ t('dailyReport.consent.explicitTitle') }}
            </h4>
            <p class="mt-1 text-sm leading-6 text-text-secondary">
              {{ t('dailyReport.consent.explicitDescription') }}
            </p>
          </div>
        </div>
      </div>

      <dl class="grid gap-3 rounded-xl border border-border bg-bg-secondary p-4 sm:grid-cols-2">
        <div class="min-w-0">
          <dt class="text-xs font-medium text-text-secondary">
            {{ t('dailyReport.consent.profile') }}
          </dt>
          <dd
            class="mt-1 truncate text-sm font-semibold"
            :title="cloudProcessing.destination?.profile_name"
          >
            {{
              cloudProcessing.destination?.profile_name ||
              t('dailyReport.consent.unknownDestination')
            }}
          </dd>
        </div>
        <div class="min-w-0">
          <dt class="text-xs font-medium text-text-secondary">
            {{ t('dailyReport.consent.endpoint') }}
          </dt>
          <dd
            class="mt-1 truncate font-mono text-xs"
            :title="cloudProcessing.destination?.endpoint"
          >
            {{ cloudProcessing.destination?.endpoint || '—' }}
          </dd>
        </div>
      </dl>

      <div class="space-y-3">
        <div class="consent-disclosure-row">
          <PhArrowSquareOut :size="21" />
          <div>
            <strong>{{ t('dailyReport.consent.sentContentTitle') }}</strong>
            <p>{{ t('dailyReport.consent.sentContent') }}</p>
          </div>
        </div>
        <div class="consent-disclosure-row">
          <PhClockCountdown :size="21" />
          <div>
            <strong>{{ t('dailyReport.consent.automationTitle') }}</strong>
            <p>{{ t('dailyReport.consent.automation') }}</p>
          </div>
        </div>
        <div class="consent-disclosure-row">
          <PhCoins :size="21" />
          <div>
            <strong>{{ t('dailyReport.consent.costTitle') }}</strong>
            <p>{{ t('dailyReport.consent.cost') }}</p>
          </div>
        </div>
        <div class="consent-disclosure-row">
          <PhDatabase :size="21" />
          <div>
            <strong>{{ t('dailyReport.consent.retentionTitle') }}</strong>
            <p>{{ t('dailyReport.consent.retention') }}</p>
          </div>
        </div>
      </div>

      <label
        class="flex cursor-pointer items-start gap-3 rounded-xl border border-accent/40 bg-accent/5 p-4"
      >
        <input
          v-model="acknowledged"
          type="checkbox"
          class="mt-1 h-4 w-4 shrink-0 accent-accent"
          data-testid="daily-report-consent-checkbox"
        />
        <span class="text-sm font-medium leading-6">{{
          t('dailyReport.consent.acknowledge')
        }}</span>
      </label>
    </div>

    <template #footer>
      <ModalFooter
        :secondary-button="{ label: t('common.cancel'), disabled: consentActionRunning }"
        :primary-button="{
          label: t('dailyReport.consent.grantAndContinue'),
          loading: consentActionRunning,
          disabled: !acknowledged,
        }"
        @secondary-click="close"
        @primary-click="grant"
      />
    </template>
  </BaseModal>
</template>

<style scoped>
@reference "../../style.css";
.consent-disclosure-row {
  @apply flex items-start gap-3 rounded-lg bg-bg-secondary p-3 text-text-secondary;
}
.consent-disclosure-row > svg {
  @apply mt-0.5 shrink-0 text-accent;
}
.consent-disclosure-row strong {
  @apply text-sm font-semibold text-text-primary;
}
.consent-disclosure-row p {
  @apply mt-1 text-xs leading-5;
}
</style>
