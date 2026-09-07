import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { nextTick } from 'vue';
import { mount, shallowMount, flushPromises } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createI18n } from 'vue-i18n';
import en from './i18n/locales/en';
import zh from './i18n/locales/zh';
import RuleLogicConnector from './components/modals/rules/RuleLogicConnector.vue';
import ArticleList from './components/article/ArticleList.vue';
import type { Feed, Article } from './types/models';
import { useArticleActions } from './composables/article/useArticleActions';
import App from './App.vue';
import ArticleChatPanel from './components/article/ArticleChatPanel.vue';
import AIFeatureSettings from './components/modals/settings/ai/AIFeatureSettings.vue';
import type { SettingsData } from './types/settings';
import ActivityBar from './components/sidebar/ActivityBar.vue';
import DailyReportCloudConsentModal from './components/dailyReport/DailyReportCloudConsentModal.vue';
import {
  createAutoRefreshScheduler,
  getAutoRefreshInterval,
  preserveSelectedArticle,
  useAppStore,
} from './stores/app';
import { DailyReportAPIError, useDailyReports } from './composables/dailyReport/useDailyReports';
import { setSettingsFromRawData } from './composables/core/useSettings';
import { getAIErrorMessage } from './utils/aiError';
import {
  getRecommendedFonts,
  resolveFontFamily,
  SYSTEM_FONT_STACK,
  WINDOWS_SYSTEM_FONT_STACK,
} from './utils/fontDetector';

// Create stub components for complex child components
const createStub = (name: string) => ({
  name,
  template: '<div class="stub-component"><slot /></div>',
});

const collectStrings = (value: unknown): string[] => {
  if (typeof value === 'string') return [value];
  if (Array.isArray(value)) return value.flatMap(collectStrings);
  if (value && typeof value === 'object') {
    return Object.values(value as Record<string, unknown>).flatMap(collectStrings);
  }
  return [];
};

describe('App', () => {
  it('preserves the selected article while replacing a refreshed first page', () => {
    const selected = { id: 75, title: 'Selected article' };
    const fresh = [{ id: 1, title: 'Fresh article' }];

    expect(preserveSelectedArticle(fresh, [selected], 75)).toEqual([fresh[0], selected]);
    expect(preserveSelectedArticle([selected], [selected], 75)).toEqual([selected]);
    expect(preserveSelectedArticle(fresh, [selected], null)).toEqual(fresh);
  });

  it('schedules normal and very long automatic refresh intervals without overflowing', async () => {
    vi.useFakeTimers();
    const refresh = vi.fn();
    const scheduler = createAutoRefreshScheduler(refresh);

    scheduler.start(30);
    await vi.advanceTimersByTimeAsync(30 * 60 * 1000 - 1);
    expect(refresh).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(refresh).toHaveBeenCalledTimes(1);

    refresh.mockClear();
    scheduler.start(46_080);
    await vi.advanceTimersByTimeAsync(46_080 * 60 * 1000 - 1);
    expect(refresh).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    expect(refresh).toHaveBeenCalledTimes(1);

    scheduler.stop();
    vi.useRealTimers();
  });

  it('replaces or disables an existing automatic refresh schedule', async () => {
    vi.useFakeTimers();
    const refresh = vi.fn();
    const scheduler = createAutoRefreshScheduler(refresh);

    scheduler.start(30);
    await vi.advanceTimersByTimeAsync(15 * 60 * 1000);
    scheduler.start(60);
    await vi.advanceTimersByTimeAsync(45 * 60 * 1000);
    expect(refresh).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(15 * 60 * 1000);
    expect(refresh).toHaveBeenCalledTimes(1);

    refresh.mockClear();
    scheduler.start(30);
    scheduler.start(0);
    await vi.advanceTimersByTimeAsync(60 * 60 * 1000);
    expect(refresh).not.toHaveBeenCalled();

    scheduler.stop();
    vi.useRealTimers();
  });

  it('disables the frontend timer outside fixed refresh mode', () => {
    expect(getAutoRefreshInterval('fixed', 30)).toBe(30);
    expect(getAutoRefreshInterval('intelligent', 30)).toBe(0);
    expect(getAutoRefreshInterval('never', 30)).toBe(0);
  });

  it('skips overlapping refreshes and catches up only once after sleep', async () => {
    vi.useFakeTimers();
    const refresh = vi.fn();
    let refreshing = true;
    let currentTime = 0;
    const scheduler = createAutoRefreshScheduler(
      refresh,
      () => !refreshing,
      () => currentTime
    );

    scheduler.start(30);
    currentTime = 30 * 60 * 1000;
    await vi.advanceTimersByTimeAsync(30 * 60 * 1000);
    expect(refresh).not.toHaveBeenCalled();

    refreshing = false;
    currentTime = 4 * 30 * 60 * 1000;
    await vi.advanceTimersByTimeAsync(30 * 60 * 1000);
    expect(refresh).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(refresh).toHaveBeenCalledTimes(1);

    currentTime = 5 * 30 * 60 * 1000;
    await vi.advanceTimersByTimeAsync(30 * 60 * 1000);
    expect(refresh).toHaveBeenCalledTimes(2);

    scheduler.stop();
    vi.useRealTimers();
  });

  it('uses the MRSS brand and fork attribution', () => {
    expect(en.appName).toBe('MRSS');
    expect(en.setting.about.forkNotice).toContain('DevXDojo/MrRSS');
    expect(en.setting.about.licenseNotice).toContain('GPL-3.0');
    expect(en.setting.about.forkNotice).not.toContain('2026');
    expect(zh.setting.about.forkNotice).not.toContain('2026');
    expect(en.setting.about).not.toHaveProperty('noWarranty');
    expect(zh.setting.about).not.toHaveProperty('noWarranty');
  });

  it('maps provider failures to short messages and keeps toast content inside the viewport', () => {
    const rawProviderError =
      'OpenRouter 429 {"error":{"message":"VERY_LONG_PROVIDER_RESPONSE_WITH_SECRET_TOKEN"}}';
    expect(getAIErrorMessage(rawProviderError)).toBe(en.aiErrors.rate_limited);
    expect(
      getAIErrorMessage({ error_code: 'authentication_failed', error: rawProviderError })
    ).toBe(en.aiErrors.authentication_failed);
    expect(getAIErrorMessage(undefined, 'unrecognized ' + 'x'.repeat(2000))).toBe(
      en.aiErrors.request_failed
    );

    const toast = readFileSync('src/components/common/Toast.vue', 'utf8');
    const chat = readFileSync('src/components/article/ArticleChatPanel.vue', 'utf8');
    expect(toast).toContain('overflow-wrap: anywhere');
    expect(toast).toMatch(/calc\(100vw\s*-\s*2rem\)/);
    expect(chat).not.toContain('v-html="msg.html || msg.content"');
  });

  it('does not pass raw service errors directly to user notifications', () => {
    const notificationSources = [
      'src/components/article/ArticleDetailModal.vue',
      'src/components/modals/feed/FeedFormModal.vue',
      'src/components/modals/settings/plugins/FreshRSSSettings.vue',
      'src/components/modals/settings/plugins/RSSHubSettings.vue',
      'src/components/modals/settings/reading/CustomizationSettings.vue',
      'src/components/sidebar/FeedList.vue',
      'src/composables/article/useArticleActions.ts',
      'src/composables/article/useArticleDetail.ts',
      'src/composables/core/useSidebar.ts',
    ];
    const unsafeToastValue =
      /showToast\(\s*(?:error\.message|result\.(?:error|message)|response\.text\(\)|errorText|responseText)/;

    for (const path of notificationSources) {
      expect(readFileSync(path, 'utf8'), path).not.toMatch(unsafeToastValue);
    }
  });

  it('tests the saved AI key only when the form still contains its mask', () => {
    const profileModal = readFileSync(
      'src/components/modals/settings/ai/AIProfileModal.vue',
      'utf8'
    );

    expect(profileModal).toContain("formData.value.api_key.startsWith('****')");
    expect(profileModal).not.toContain("!formData.value.api_key.startsWith('****')");
    expect(profileModal.indexOf('result = await testProfile(props.editProfileId)')).toBeLessThan(
      profileModal.indexOf('result = await testConfig({')
    );
  });

  it('provides a safe, bilingual daily report interface', () => {
    expect(en.dailyReport.title).toBe('24-Hour AI Digest');
    expect(zh.dailyReport.title).toBe('24 小时 AI 日报');
    expect(en.dailyReport.config.aiPrivacyNotice).toContain('Token');
    expect(en.dailyReport.config.aiPrivacyNotice).toContain('API keys');

    const dailyReportView = readFileSync('src/components/dailyReport/DailyReportView.vue', 'utf8');
    expect(dailyReportView).not.toContain('v-html');
    expect(dailyReportView).toContain('source.source_index');
    expect(dailyReportView).toContain('downloadMarkdown');
    expect(dailyReportView).toContain('section.blocks?.length');
    expect(dailyReportView).toContain("block.type === 'ordered_list'");
    expect(dailyReportView).toContain('{{ item.text }}');
    expect(dailyReportView).not.toContain("t('dailyReport.detail.failureCode'");
    expect(dailyReportView).toContain('step.match(/^summarizing:');
    expect(dailyReportView).toContain('step.match(/^writing:');

    const dailyReportComposable = readFileSync(
      'src/composables/dailyReport/useDailyReports.ts',
      'utf8'
    );
    expect(dailyReportComposable).toContain('completedEventRunIds.has(runId)');
    expect(dailyReportComposable).toContain('openedNotificationRunIds.has(runId)');

    const desktopNotifier = readFileSync('../daily_report_notifications.go', 'utf8');
    expect(desktopNotifier).toContain('eventSent');
    expect(desktopNotifier).toContain('systemSent');
    expect(desktopNotifier).toContain('opened');
    expect(desktopNotifier).toContain('SendNotification(options)');
    expect(desktopNotifier).toContain('dailyReportNotificationPreview(run)');
    expect(desktopNotifier).not.toContain('SendNotificationWithActions');
    expect(desktopNotifier).not.toContain('ThreadID:');
    expect(desktopNotifier).not.toContain('CategoryID:');

    const desktopMain = readFileSync('../main.go', 'utf8');
    expect(desktopMain).toContain('dailyReportNotifier.claimOpen(runID)');

    const dailyReportConfig = readFileSync(
      'src/components/dailyReport/DailyReportConfigModal.vue',
      'utf8'
    );
    expect(dailyReportConfig).toContain('v-model="form.article_summary_mode"');
    expect(zh.dailyReport.config.articleSummaryModeAI).toContain('AI');
    expect(zh.dailyReport.config.articleSummaryModeLocal).toContain('TextRank');

    expect(zh.dailyReport.action.resumeAI).toBe('继续生成');
    expect(zh.dailyReport.action.restartAI).toBe('重新生成');
    expect(en.dailyReport.action.resumeAI).toBe('Continue generation');
    expect(en.dailyReport.action.restartAI).toBe('Regenerate');

    const visibleCopy = collectStrings([zh.dailyReport, en.dailyReport]).join('\n');
    expect(visibleCopy).not.toMatch(/未创建新记录|No new record|从断点|错误代码|Error code/i);
    expect(visibleCopy).not.toMatch(/checkpoint|fingerprint|polling/i);
  });

  it('switches to the daily report top-level view and caps its unread badge', async () => {
    const pinia = createPinia();
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } });
    const dailyReports = useDailyReports();
    dailyReports.status.value.unread_count = 120;

    const wrapper = mount(ActivityBar, {
      global: { plugins: [pinia, i18n] },
    });
    await nextTick();
    const store = useAppStore(pinia);

    expect(wrapper.get('[data-testid="daily-report-unread-badge"]').text()).toBe('99+');
    const button = wrapper
      .findAll('button')
      .find((candidate) => candidate.attributes('title') === en.dailyReport.title);
    expect(button).toBeDefined();
    await button!.trigger('click');
    expect(store.currentView).toBe('dailyReports');

    wrapper.unmount();
    dailyReports.status.value.unread_count = 0;
  });

  it('requires an explicit checkbox before cloud processing consent can be granted', async () => {
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } });
    const dailyReports = useDailyReports();
    dailyReports.cloudProcessing.value = {
      disclosure_version: 1,
      required: true,
      accepted: false,
      accepted_version: null,
      accepted_at: null,
      destination: {
        profile_id: 8,
        profile_name: 'Private AI Profile',
        endpoint: 'https://api.example.com',
      },
    };

    const wrapper = mount(DailyReportCloudConsentModal, {
      global: { plugins: [i18n] },
    });
    const grantButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes(en.dailyReport.consent.grantAndContinue));

    expect(wrapper.text()).toContain('Private AI Profile');
    expect(wrapper.text()).toContain('https://api.example.com');
    expect(grantButton?.attributes('disabled')).toBeDefined();
    await wrapper.get('[data-testid="daily-report-consent-checkbox"]').setValue(true);
    expect(grantButton?.attributes('disabled')).toBeUndefined();
    expect(wrapper.html()).not.toContain('v-html');

    wrapper.unmount();
    dailyReports.closeCloudConsentPrompt();
  });

  it('preserves consent error metadata and schedules the original action once', async () => {
    const dailyReports = useDailyReports();
    const retry = vi.fn();
    const disclosure = {
      disclosure_version: 1,
      required: true,
      accepted: false,
      accepted_version: null,
      accepted_at: null,
      destination: {
        profile_id: 9,
        profile_name: 'Changed Profile',
        endpoint: 'https://changed.example.com',
      },
    };
    const error = new DailyReportAPIError(
      'Consent required',
      409,
      'cloud_processing_consent_required',
      { cloud_processing: disclosure }
    );
    const acceptedDisclosure = {
      ...disclosure,
      accepted: true,
      accepted_version: 1,
      accepted_at: '2026-08-19T08:00:00Z',
    };
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === '/api/daily-report/consent' && init?.method === 'POST') {
        expect(JSON.parse(String(init.body))).toEqual({ action: 'grant', version: 1 });
        return new Response(JSON.stringify({ cloud_processing: acceptedDisclosure }), {
          status: 200,
        });
      }
      if (url === '/api/daily-report/config') {
        return new Response(
          JSON.stringify({
            config: dailyReports.config.value,
            cloud_processing: acceptedDisclosure,
          }),
          { status: 200 }
        );
      }
      if (url === '/api/daily-report/status') {
        return new Response(JSON.stringify(dailyReports.status.value), { status: 200 });
      }
      throw new Error(`Unexpected request: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    try {
      expect(await dailyReports.promptCloudConsent(error, retry)).toBe(true);
      expect(dailyReports.consentModalVisible.value).toBe(true);
      expect(dailyReports.cloudProcessing.value.destination?.profile_name).toBe('Changed Profile');
      expect(retry).not.toHaveBeenCalled();

      await dailyReports.grantCloudConsentAndRetry();
      expect(retry).toHaveBeenCalledTimes(1);
      expect(dailyReports.consentModalVisible.value).toBe(false);
      expect(dailyReports.cloudProcessing.value.accepted).toBe(true);
    } finally {
      dailyReports.closeCloudConsentPrompt();
      vi.unstubAllGlobals();
    }
  });

  it('revokes cloud consent without reloading and replacing an unsaved AI profile draft', async () => {
    const dailyReports = useDailyReports();
    dailyReports.config.value.ai_profile_id = 13;
    dailyReports.config.value.enabled = true;
    const revokedDisclosure = {
      disclosure_version: 1,
      required: true,
      accepted: false,
      accepted_version: null,
      accepted_at: null,
      destination: {
        profile_id: 12,
        profile_name: 'Saved Profile',
        endpoint: 'https://saved.example.com',
      },
    };
    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url === '/api/daily-report/consent' && init?.method === 'POST') {
        expect(JSON.parse(String(init.body))).toEqual({ action: 'revoke' });
        return new Response(JSON.stringify({ cloud_processing: revokedDisclosure }), {
          status: 200,
        });
      }
      if (url === '/api/daily-report/status') {
        return new Response(JSON.stringify(dailyReports.status.value), { status: 200 });
      }
      if (url === '/api/daily-report/config') {
        throw new Error('Revocation must not reload the saved config');
      }
      throw new Error(`Unexpected request: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    try {
      await dailyReports.updateCloudProcessingConsent('revoke', { refreshConfig: false });
      expect(dailyReports.config.value.ai_profile_id).toBe(13);
      expect(dailyReports.config.value.enabled).toBe(false);
      expect(fetchMock).not.toHaveBeenCalledWith('/api/daily-report/config', expect.anything());
    } finally {
      vi.unstubAllGlobals();
    }
  });

  it('keeps the newest daily report detail when requests resolve out of order', async () => {
    const dailyReports = useDailyReports();
    let resolveFirst!: (response: Response) => void;
    let resolveSecond!: (response: Response) => void;
    const firstResponse = new Promise<Response>((resolve) => {
      resolveFirst = resolve;
    });
    const secondResponse = new Promise<Response>((resolve) => {
      resolveSecond = resolve;
    });
    vi.stubGlobal(
      'fetch',
      vi.fn((input: RequestInfo | URL) => {
        const url = String(input);
        if (url.endsWith('/1')) return firstResponse;
        if (url.endsWith('/2')) return secondResponse;
        throw new Error(`Unexpected request: ${url}`);
      })
    );
    const detail = (id: number, title: string) => ({
      run: {
        id,
        kind: 'manual' as const,
        status: 'completed' as const,
        period_start: '2026-08-18T00:00:00Z',
        period_end: '2026-08-19T00:00:00Z',
        progress: 100,
        title,
        content: { sections: [] },
        markdown: '',
        input_tokens: 0,
        output_tokens: 0,
        article_count: 0,
        is_read: false,
        error: '',
        created_at: '2026-08-19T00:00:00Z',
      },
      sources: [],
    });

    try {
      const firstRequest = dailyReports.fetchDetail(1);
      const secondRequest = dailyReports.fetchDetail(2);
      resolveSecond(new Response(JSON.stringify(detail(2, 'Newest')), { status: 200 }));
      await secondRequest;
      resolveFirst(new Response(JSON.stringify(detail(1, 'Stale')), { status: 200 }));
      await firstRequest;

      expect(dailyReports.selectedRunId.value).toBe(2);
      expect(dailyReports.selectedDetail.value?.run.title).toBe('Newest');
      expect(dailyReports.loadingDetail.value).toBe(false);
    } finally {
      dailyReports.selectRun(null);
      vi.unstubAllGlobals();
    }
  });

  it('silently refreshes an active daily report detail into its terminal state', async () => {
    const dailyReports = useDailyReports();
    const failedRun = {
      id: 9,
      kind: 'manual' as const,
      status: 'failed' as const,
      period_start: '2026-08-24T00:00:00Z',
      period_end: '2026-08-25T00:00:00Z',
      progress: 100,
      current_step: 'failed',
      title: 'Failed digest',
      content: { sections: [] },
      markdown: '',
      input_tokens: 120,
      output_tokens: 20,
      article_count: 4,
      is_read: true,
      error: 'generation failed',
      failure_code: 'timeout',
      generation_mode: 'ai' as const,
      created_at: '2026-08-25T00:00:00Z',
      completed_at: '2026-08-25T00:01:00Z',
    };
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url === '/api/daily-report/history/9') {
        return new Response(JSON.stringify({ run: failedRun, sources: [] }), { status: 200 });
      }
      if (url.startsWith('/api/daily-report/history?')) {
        return new Response(
          JSON.stringify({ items: [failedRun], total: 1, page: 1, page_size: 20 }),
          { status: 200 }
        );
      }
      if (url === '/api/daily-report/status') {
        return new Response(
          JSON.stringify({
            enabled: true,
            is_running: false,
            progress: 0,
            unread_count: 0,
            missed_count: 0,
            notification_authorization: 'not_determined',
          }),
          { status: 200 }
        );
      }
      throw new Error(`Unexpected request: ${url}`);
    });
    vi.stubGlobal('fetch', fetchMock);

    try {
      const refresh = dailyReports.refreshSelectedRun(9);
      expect(dailyReports.loadingDetail.value).toBe(false);
      const detail = await refresh;

      expect(detail?.run.status).toBe('failed');
      expect(dailyReports.selectedDetail.value?.run.failure_code).toBe('timeout');
      expect(dailyReports.history.value[0]?.status).toBe('failed');
      expect(dailyReports.loadingHistory.value).toBe(false);
    } finally {
      dailyReports.selectRun(null);
      dailyReports.history.value = [];
      vi.unstubAllGlobals();
    }
  });

  it('uses bundled fonts only for the Windows system default', () => {
    expect(resolveFontFamily('system', 'windows')).toBe(WINDOWS_SYSTEM_FONT_STACK);
    expect(resolveFontFamily('system', 'darwin')).toBe(SYSTEM_FONT_STACK);
    expect(resolveFontFamily('system', 'linux')).toBe(SYSTEM_FONT_STACK);
    expect(resolveFontFamily('system', 'other')).toBe(SYSTEM_FONT_STACK);

    const windowsCustomFont = resolveFontFamily('Noto Serif SC', 'windows');
    expect(windowsCustomFont).toMatch(/^"Noto Serif SC",/);
    expect(windowsCustomFont).toContain('"Inter Variable"');
    expect(windowsCustomFont).toContain('"Noto Sans SC Variable"');
    expect(windowsCustomFont.indexOf('"Inter Variable"')).toBeLessThan(
      windowsCustomFont.indexOf('"Noto Sans SC Variable"')
    );

    expect(resolveFontFamily('serif', 'windows')).toBe('Georgia, "Times New Roman", Times, serif');
  });

  it('does not load fonts from remote services', () => {
    const indexHtml = readFileSync('index.html', 'utf8');
    expect(indexHtml).not.toContain('fonts.googleapis.com');
    expect(indexHtml).not.toContain('fonts.gstatic.com');
  });

  it('preserves the selected article during a background refresh', () => {
    const selected = { id: 75, title: 'Selected article' };
    const fresh = [{ id: 1, title: 'Fresh article' }];

    expect(preserveSelectedArticle(fresh, [selected], 75)).toEqual([fresh[0], selected]);
    expect(preserveSelectedArticle([selected], [selected], 75)).toEqual([selected]);
    expect(preserveSelectedArticle(fresh, [selected], null)).toEqual(fresh);
  });

  it('keeps long toast messages inside narrow viewports', () => {
    const toast = readFileSync('src/components/common/Toast.vue', 'utf8');
    expect(toast).toMatch(/calc\(100vw\s*-\s*2rem\)/);
    expect(toast).toContain('overflow-wrap: anywhere');
    expect(toast).toContain('min-w-0 flex-1');
    expect(toast).toContain('shrink-0');
  });

  it('renders and reacts to interface typography settings', async () => {
    setSettingsFromRawData({});
    const pinia = createPinia();
    const i18n = createI18n({
      legacy: false,
      locale: 'en',
      messages: { en },
    });

    // Mock store methods
    const mockFetchFeeds = vi.fn();
    const mockFetchArticles = vi.fn();
    const mockInitTheme = vi.fn();

    const wrapper = mount(App, {
      global: {
        plugins: [pinia, i18n],
        stubs: {
          Sidebar: createStub('Sidebar'),
          ArticleList: createStub('ArticleList'),
          ArticleDetail: createStub('ArticleDetail'),
          ImageGalleryView: createStub('ImageGalleryView'),
          DailyReportView: createStub('DailyReportView'),
          DailyReportMissedRunsModal: createStub('DailyReportMissedRunsModal'),
          DailyReportCloudConsentModal: createStub('DailyReportCloudConsentModal'),
          AddFeedModal: createStub('AddFeedModal'),
          EditFeedModal: createStub('EditFeedModal'),
          SettingsModal: createStub('SettingsModal'),
          DiscoverFeedsModal: createStub('DiscoverFeedsModal'),
          UpdateAvailableDialog: createStub('UpdateAvailableDialog'),
          ContextMenu: createStub('ContextMenu'),
          ConfirmDialog: createStub('ConfirmDialog'),
          InputDialog: createStub('InputDialog'),
          MultiSelectDialog: createStub('MultiSelectDialog'),
          Toast: createStub('Toast'),
        },
        mocks: {
          $window: {
            showToast: vi.fn(),
            showConfirm: vi.fn(() => Promise.resolve(true)),
          },
        },
      },
    });

    // Check that the app container is rendered
    expect(wrapper.find('.app-container').exists()).toBe(true);
    expect(document.documentElement.style.getPropertyValue('--ui-font-family')).toContain('Inter');
    expect(document.documentElement.style.getPropertyValue('--ui-font-size')).toBe('16px');
    expect(document.documentElement.style.getPropertyValue('--ui-font-scale')).toBe('1');

    setSettingsFromRawData({
      ui_font_family: 'serif',
      ui_font_size: '8',
    });
    await nextTick();

    expect(document.documentElement.style.getPropertyValue('--ui-font-family')).toContain(
      'Georgia'
    );
    expect(document.documentElement.style.getPropertyValue('--ui-font-size')).toBe('12px');
    expect(document.documentElement.style.getPropertyValue('--ui-font-scale')).toBe('0.75');

    setSettingsFromRawData({
      ui_font_family: 'serif',
      ui_font_size: '24',
    });
    await nextTick();

    expect(document.documentElement.style.getPropertyValue('--ui-font-size')).toBe('20px');
    expect(document.documentElement.style.getPropertyValue('--ui-font-scale')).toBe('1.25');

    setSettingsFromRawData({
      ui_font_family: 'Noto Sans SC',
      ui_font_size: '16',
    });
    await nextTick();

    expect(document.documentElement.style.getPropertyValue('--ui-font-family')).toContain(
      '"Noto Sans SC"'
    );
    expect(document.documentElement.style.getPropertyValue('--ui-font-family')).toContain(
      'system-ui'
    );

    wrapper.unmount();
    expect(document.documentElement.style.getPropertyValue('--ui-font-family')).toBe('');
  });

  it('detects the expanded Chinese font catalog in the expected groups', () => {
    let currentFont = '';
    const context = {
      get font() {
        return currentFont;
      },
      set font(value: string) {
        currentFont = value;
      },
      measureText: () => ({ width: currentFont === '100px sans-serif' ? 100 : 200 }),
    };
    const getContextSpy = vi
      .spyOn(HTMLCanvasElement.prototype, 'getContext')
      .mockReturnValue(context as unknown as CanvasRenderingContext2D);

    const fonts = getRecommendedFonts();

    expect(fonts.sansSerif).toEqual(
      expect.arrayContaining([
        'Noto Sans SC',
        'Source Han Sans CN',
        'Sarasa Gothic SC',
        'Sarasa UI SC',
      ])
    );
    expect(fonts.serif).toEqual(
      expect.arrayContaining([
        'Noto Serif SC',
        'Source Han Serif CN',
        'LXGW WenKai',
        'LXGW WenKai GB',
        'LXGW WenKai Lite',
        'LXGW WenKai Screen',
      ])
    );

    getContextSpy.mockRestore();
  });
});

describe('AI chat panel size', () => {
  it('restores a saved size, fits a smaller viewport, and keeps the preferred size', async () => {
    const ChatPanel = (await import('./components/article/ArticleChatPanel.vue')).default;
    vi.stubGlobal(
      'fetch',
      vi.fn(async () => ({ ok: true, json: async () => [] }))
    );
    vi.stubGlobal('innerWidth', 1000);
    vi.stubGlobal('innerHeight', 800);
    localStorage.setItem('mrrssChatPanelSize', JSON.stringify({ width: 650, height: 680 }));
    const wrapper = mount(ChatPanel, {
      props: {
        article: { id: 1, title: 'Article', url: 'https://example.com' } as any,
        articleContent: 'Article body',
        settings: { ai_chat_enabled: true, ai_chat_profile_id: '', ai_chat_quick_prompts: '' },
      },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })],
        stubs: { Teleport: true },
      },
    });
    try {
      await nextTick();
      const panel = wrapper.get('.chat-panel').element as HTMLElement;
      expect(panel.style.width).toBe('650px');
      expect(panel.style.height).toBe('680px');
      vi.stubGlobal('innerWidth', 500);
      vi.stubGlobal('innerHeight', 400);
      window.dispatchEvent(new Event('resize'));
      expect(panel.style.width).toBe('468px');
      expect(panel.style.height).toBe('344px');
      vi.stubGlobal('innerWidth', 1000);
      vi.stubGlobal('innerHeight', 800);
      window.dispatchEvent(new Event('resize'));
      expect(panel.style.width).toBe('650px');
      expect(JSON.parse(localStorage.getItem('mrrssChatPanelSize')!).width).toBe(650);
      vi.spyOn(panel, 'getBoundingClientRect').mockReturnValue({ width: 650, height: 680 } as DOMRect);
      await wrapper.get('.cursor-nw-resize').trigger('mousedown', { clientX: 100, clientY: 100 });
      document.dispatchEvent(new MouseEvent('mousemove', { clientX: 50, clientY: 60 }));
      document.dispatchEvent(new MouseEvent('mouseup'));
      expect(JSON.parse(localStorage.getItem('mrrssChatPanelSize')!)).toEqual({
        width: 700,
        height: 720,
      });
    } finally {
      wrapper.unmount();
      localStorage.removeItem('mrrssChatPanelSize');
      vi.unstubAllGlobals();
    }
  });
});

describe('Rule logic localization', () => {
  it('localizes AND/OR labels while preserving emitted rule operators', async () => {
    const i18n = createI18n({ legacy: false, locale: 'zh', messages: { en, zh } });
    const wrapper = mount(RuleLogicConnector, {
      props: { logic: 'and' },
      global: { plugins: [i18n] },
    });
    const buttons = wrapper.findAll('button');
    expect(buttons.map((button) => button.text())).toEqual(['且', '或']);
    await buttons[1].trigger('click');
    await buttons[0].trigger('click');
    expect(wrapper.emitted('update')).toEqual([['or'], ['and']]);

    i18n.global.locale.value = 'en';
    await nextTick();
    expect(buttons.map((button) => button.text())).toEqual(['AND', 'OR']);
    wrapper.unmount();
  });
});

describe('Chat response preferences', () => {
  it('edits and clears the shared preference without changing model selection', async () => {
    const settings = {
      ai_chat_enabled: true,
      ai_chat_profile_id: '7',
      ai_chat_quick_prompts: '[]',
      ai_chat_response_preferences: '',
    } as SettingsData;
    const wrapper = mount(AIFeatureSettings, {
      props: { settings },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })],
        stubs: { AIProfileSelector: true, AIChatQuickPromptsSettings: true },
      },
    });
    const input = wrapper.get('textarea');
    await input.setValue('用中文回答，保持简洁。');
    const updated = wrapper.emitted('update:settings')?.[0]?.[0] as SettingsData;
    expect(updated.ai_chat_response_preferences).toBe('用中文回答，保持简洁。');
    expect(updated.ai_chat_profile_id).toBe('7');
    await wrapper.setProps({ settings: updated });
    await input.setValue('');
    const cleared = wrapper.emitted('update:settings')?.[1]?.[0] as SettingsData;
    expect(cleared.ai_chat_response_preferences).toBe('');
    wrapper.unmount();
  });
});

describe('Selected source titles', () => {
  it('omits only the all filter and localizes uncategorized selections', async () => {
    const pinia = createPinia();
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en, zh } });
    const wrapper = shallowMount(ArticleList, { global: { plugins: [pinia, i18n] } });
    const store = useAppStore(pinia);
    store.feeds = [{ id: 7, title: 'Example Feed' } as Feed];
    store.currentFilter = 'all';
    await nextTick();
    const title = () => wrapper.get('h3').text();
    expect(title()).toBe('All Articles');

    store.tempSelection = { feedId: 7, category: null };
    await nextTick();
    expect(title()).toBe('Example Feed');
    for (const [filter, label] of [
      ['unread', 'Unread Articles'],
      ['favorites', 'Favorites'],
      ['readLater', 'Read Later'],
    ] as const) {
      store.currentFilter = filter;
      await nextTick();
      expect(title()).toBe(`Example Feed - ${label}`);
    }
    store.currentFilter = 'all';
    store.tempSelection = { feedId: null, category: 'Technology/News' };
    await nextTick();
    expect(title()).toBe('Technology/News');

    store.tempSelection = { feedId: null, category: 'uncategorized' };
    await nextTick();
    expect(title()).toBe('Uncategorized');
    i18n.global.locale.value = 'zh';
    await nextTick();
    expect(title()).toBe('未分类');
    store.currentFilter = 'favorites';
    await nextTick();
    expect(title()).toBe(`未分类 - ${zh.sidebar.activity.favorites}`);
    wrapper.unmount();
  });
});

describe('Article context menu read status', () => {
  it('uses neutral read icons with distinct shapes while preserving status actions and colors', () => {
    const i18n = createI18n({ legacy: false, locale: 'en', messages: { en } });
    let actions: ReturnType<typeof useArticleActions>;
    const wrapper = mount({
      setup() {
        actions = useArticleActions(i18n.global.t, { value: 'rendered' });
        return {};
      },
      template: '<div />',
    }, { global: { plugins: [createPinia(), i18n] } });
    const openMenu = vi.fn();
    window.addEventListener('open-context-menu', openMenu);
    try {
      for (const isRead of [false, true]) {
        const article: Article = {
          id: 1, feed_id: 1, title: 'Article', url: 'https://example.com/article',
          published_at: '2026-01-01T00:00:00Z', is_read: isRead,
          is_favorite: true, is_read_later: true, is_hidden: false,
        };
        actions!.showArticleContextMenu(new MouseEvent('contextmenu'), article);
        const event = openMenu.mock.calls.at(-1)![0] as CustomEvent;
        expect(event.detail.data).toBe(article);
        expect(event.detail.items[0]).toMatchObject({
          action: 'toggleRead', icon: 'ph-circle', iconColor: 'text-text-secondary',
          iconWeight: isRead ? 'regular' : 'fill',
          label: i18n.global.t(isRead ? 'article.action.markAsUnread' : 'article.action.markAsRead'),
        });
        expect(event.detail.items).toEqual(expect.arrayContaining([
          expect.objectContaining({ action: 'toggleFavorite', iconColor: 'text-yellow-500' }),
          expect.objectContaining({ action: 'toggleReadLater', iconColor: 'text-blue-500' }),
        ]));
      }
    } finally {
      window.removeEventListener('open-context-menu', openMenu);
      wrapper.unmount();
    }
  });
});

describe('Chat generation cancellation', () => {
  function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((done) => {
      resolve = done;
    });
    return { promise, resolve };
  }

  function mountChat() {
    return mount(ArticleChatPanel, {
      props: {
        article: { id: 12, title: 'Article', url: 'https://example.com/article' } as Article,
        articleContent: 'Article content',
        settings: { ai_chat_enabled: true, ai_chat_profile_id: '', ai_chat_quick_prompts: '[]' },
      },
      global: {
        plugins: [createI18n({ legacy: false, locale: 'en', messages: { en } })],
        stubs: { teleport: true },
      },
    });
  }

  it.each([false, true])('reuses stopped creation (%s)', async (resolvedBeforeRetry) => {
    const creation = deferred<Response>();
    const generated = deferred<Response>();
    const toast = vi.fn();
    const previousToast = window.showToast;
    window.showToast = toast;
    const fetchMock = vi.fn((url: string, options?: RequestInit) => {
      if (url === '/api/ai/chat/session/create') return creation.promise;
      if (url === '/api/ai-chat') return generated.promise;
      return Promise.resolve(
        new Response(JSON.stringify(url === '/api/ai-chat/cancel' ? { success: true } : []))
      );
    });
    vi.stubGlobal('fetch', fetchMock);
    const wrapper = mountChat();
    try {
      await flushPromises();
      await wrapper.get('input').setValue('first');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      await wrapper.get('[data-testid="chat-stop-generation"]').trigger('click');
      expect((wrapper.get('input').element as HTMLInputElement).value).toBe('first');
      expect(wrapper.text()).not.toContain('first');
      if (resolvedBeforeRetry) {
        creation.resolve(new Response(JSON.stringify({ id: 55, article_id: 12, title: 'first' })));
        await flushPromises();
        expect((wrapper.get('input').element as HTMLInputElement).value).toBe('first');
        expect(fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat')).toHaveLength(0);
      }
      await wrapper.get('input').setValue('second');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      expect(
        fetchMock.mock.calls.filter(([url]) => url === '/api/ai/chat/session/create')
      ).toHaveLength(1);
      if (!resolvedBeforeRetry) {
        creation.resolve(new Response(JSON.stringify({ id: 55, article_id: 12, title: 'first' })));
      }
      await flushPromises();
      const sends = fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat');
      expect(sends).toHaveLength(1);
      const request = JSON.parse(sends[0][1]?.body as string);
      expect(request.session_id).toBe(55);
      expect(request.request_id).toBeTruthy();
      expect(request.messages).toHaveLength(1);
      expect(request.messages.at(-1).content).toBe('second');
      await wrapper.get('[data-testid="chat-stop-generation"]').trigger('click');
      await flushPromises();
      expect(sends[0][1]?.signal?.aborted).toBe(true);
      generated.resolve(new Response(JSON.stringify({ response: 'late answer', session_id: 55 })));
      await flushPromises();
      expect(wrapper.text()).not.toContain('late answer');
      expect(toast).not.toHaveBeenCalled();
    } finally {
      if (wrapper.exists()) wrapper.unmount();
      window.showToast = previousToast;
      vi.unstubAllGlobals();
    }
  });

  it('preserves the first question when creating a session fails and retries it once', async () => {
    let createCount = 0;
    const fetchMock = vi.fn((url: string, options?: RequestInit) => {
      if (url === '/api/ai/chat/session/create') {
        createCount++;
        return Promise.resolve(
          createCount === 1
            ? new Response('', { status: 503 })
            : new Response(JSON.stringify({ id: 55, article_id: 12, title: 'retry me' }))
        );
      }
      if (url === '/api/ai-chat') {
        return Promise.resolve(
          new Response(JSON.stringify({ response: 'answer', session_id: 55 }))
        );
      }
      return Promise.resolve(new Response('[]'));
    });
    const previousToast = window.showToast;
    const toast = vi.fn();
    window.showToast = toast;
    vi.stubGlobal('fetch', fetchMock);
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {});
    const wrapper = mountChat();
    try {
      await flushPromises();
      await wrapper.get('input').setValue('retry me');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      expect((wrapper.get('input').element as HTMLInputElement).value).toBe('retry me');
      expect(wrapper.text()).not.toContain('retry me');
      expect(fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat')).toHaveLength(0);
      expect(toast).toHaveBeenCalledTimes(1);
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      const sends = fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat');
      expect(createCount).toBe(2);
      expect(sends).toHaveLength(1);
      const request = JSON.parse(sends[0][1]?.body as string);
      expect(request.messages).toHaveLength(1);
      expect(request.messages[0].content).toBe('retry me');
      expect(request.is_first_message).toBe(true);
      expect((wrapper.get('input').element as HTMLInputElement).value).toBe('');
    } finally {
      wrapper.unmount();
      errorSpy.mockRestore();
      window.showToast = previousToast;
      vi.unstubAllGlobals();
    }
  });

  it.each([
    { action: 'close', hasAssistant: false },
    { action: 'unmount', hasAssistant: false },
    { action: 'close', hasAssistant: true },
  ])('isolates retries: $action, assistant $hasAssistant', async ({ action, hasAssistant }) => {
    const first = deferred<Response>();
    const second = deferred<Response>();
    let sendCount = 0;
    let created = false;
    const fetchMock = vi.fn((url: string, options?: RequestInit) => {
      if (url === '/api/ai/chat/session/create') {
        created = true;
        return Promise.resolve(
          new Response(JSON.stringify({ id: 55, article_id: 12, title: 'first' }))
        );
      }
      if (url.startsWith('/api/ai/chat/sessions?')) {
        return Promise.resolve(
          new Response(JSON.stringify(created ? [{ id: 55, article_id: 12, title: 'first' }] : []))
        );
      }
      if (url === '/api/ai-chat') return ++sendCount === 1 ? first.promise : second.promise;
      if (url.startsWith('/api/ai/chat/messages?')) {
        const history = [{ role: 'user', content: 'stored first question' }];
        if (hasAssistant) history.push({ role: 'assistant', content: 'stored first answer' });
        return Promise.resolve(new Response(JSON.stringify(history)));
      }
      return Promise.resolve(
        new Response(JSON.stringify(url === '/api/ai-chat/cancel' ? { success: true } : []))
      );
    });
    const toast = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    const previousToast = window.showToast;
    window.showToast = toast;
    const wrapper = mountChat();
    try {
      await flushPromises();
      await wrapper.get('input').setValue('first');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      await wrapper.get('[data-testid="chat-stop-generation"]').trigger('click');
      await flushPromises();
      await wrapper.get('input').setValue('second');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      first.resolve(new Response(JSON.stringify({ response: 'old result', session_id: 55 })));
      await flushPromises();
      expect(wrapper.find('[data-testid="chat-stop-generation"]').exists()).toBe(true);
      expect(wrapper.text()).not.toContain('old result');
      if (action === 'close') {
        await wrapper.get('[data-testid="chat-close"]').trigger('click');
        expect(wrapper.emitted('close')).toHaveLength(1);
      } else {
        wrapper.unmount();
      }
      await flushPromises();
      const sends = fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat');
      const cancels = fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat/cancel');
      const nextRequest = JSON.parse(sends[1][1]?.body as string);
      expect(nextRequest.is_first_message).toBe(!hasAssistant);
      expect(nextRequest.article_content).toBe('Article content');
      expect(cancels).toHaveLength(2);
      expect(JSON.parse(cancels[0][1]?.body as string).request_id).not.toBe(
        JSON.parse(cancels[1][1]?.body as string).request_id
      );
      expect(sends[1][1]?.signal?.aborted).toBe(true);
      second.resolve(new Response(JSON.stringify({ response: 'closed result', session_id: 55 })));
      await flushPromises();
      expect(wrapper.text()).not.toContain('closed result');
      expect(toast).not.toHaveBeenCalled();
    } finally {
      if (wrapper.exists()) wrapper.unmount();
      window.showToast = previousToast;
      vi.unstubAllGlobals();
    }
  });
  it('keeps an active conversation bound to A when the reader switches to B', async () => {
    const answer = deferred<Response>();
    let created = false;
    let sendCount = 0;
    const session = { id: 55, article_id: 12, title: 'Question for A' };
    const fetchMock = vi.fn((url: string, options?: RequestInit) => {
      if (url === '/api/ai/chat/session/create') {
        created = true;
        return Promise.resolve(new Response(JSON.stringify(session)));
      }
      if (url === '/api/ai-chat') {
        return ++sendCount === 1
          ? answer.promise
          : Promise.resolve(
              new Response(JSON.stringify({ response: 'Follow-up for A', session_id: 55 }))
            );
      }
      if (url.startsWith('/api/ai/chat/sessions'))
        return Promise.resolve(new Response(JSON.stringify(created ? [session] : [])));
      return Promise.resolve(new Response(JSON.stringify([])));
    });
    vi.stubGlobal('fetch', fetchMock);
    const wrapper = mountChat();
    try {
      await flushPromises();
      await wrapper.get('input').setValue('Question for A');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      await wrapper.setProps({
        article: { id: 13, title: 'Article B', url: 'https://example.com/b' } as Article,
        articleContent: 'Body B',
      });
      expect(
        wrapper.get('[data-testid="chat-context-article"]').attributes('data-context-article-id')
      ).toBe('12');
      expect(wrapper.get('[data-testid="chat-stop-generation"]').exists()).toBe(true);
      expect(wrapper.get('input').attributes('disabled')).toBeDefined();
      expect(fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat/cancel')).toHaveLength(0);
      answer.resolve(
        new Response(JSON.stringify({ response: 'Answer bound to A', session_id: 55 }))
      );
      await flushPromises();
      expect(wrapper.text()).toContain('Answer bound to A');
      expect(wrapper.find('[data-testid="chat-stop-generation"]').exists()).toBe(false);
      expect(wrapper.get('[data-testid="chat-send-message"]').attributes('disabled')).toBeDefined();
      await wrapper.setProps({
        article: { id: 12, title: 'Article', url: 'https://example.com/article' } as Article,
        articleContent: 'Article content',
      });
      await wrapper.get('input').setValue('Follow-up for A');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      const sends = fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat');
      expect(sends).toHaveLength(2);
      for (const [, options] of sends) {
        const body = JSON.parse(options?.body as string);
        expect(body.article_id).toBe(12);
        expect(body.article_content).toBe('Article content');
        expect(body.session_id).toBe(55);
      }
      expect(JSON.parse(sends[1][1]?.body as string).is_first_message).toBe(false);
    } finally {
      wrapper.unmount();
      vi.unstubAllGlobals();
    }
  });

  it.each([12, 13])(
    'isolates a stopped draft create from explicit New for article %s',
    async (articleId) => {
      const createA = deferred<Response>();
      const answerB = deferred<Response>();
      let creates = 0;
      const fetchMock = vi.fn((url: string, options?: RequestInit) => {
        if (url === '/api/ai/chat/session/create') {
          const body = JSON.parse(options?.body as string);
          return ++creates === 1
            ? createA.promise
            : Promise.resolve(
                new Response(JSON.stringify({ id: 66, article_id: articleId, title: body.title }))
              );
        }
        if (url === '/api/ai-chat') return answerB.promise;
        return Promise.resolve(new Response(JSON.stringify([])));
      });
      vi.stubGlobal('fetch', fetchMock);
      const wrapper = mountChat();
      try {
        await flushPromises();
        await wrapper.get('input').setValue('Stopped question for A');
        await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
        await flushPromises();
        await wrapper.get('[data-testid="chat-stop-generation"]').trigger('click');
        expect((wrapper.get('input').element as HTMLInputElement).value).toBe(
          'Stopped question for A'
        );
        await wrapper.setProps({
          article: { id: articleId, title: 'Article B', url: 'https://example.com/b' } as Article,
          articleContent: 'Body B',
        });
        await wrapper.get('[data-testid="chat-new-session"]').trigger('click');
        await wrapper.get('input').setValue('Question for B');
        await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
        await flushPromises();
        createA.resolve(
          new Response(JSON.stringify({ id: 55, article_id: 12, title: 'Old draft' }))
        );
        await flushPromises();
        const sends = fetchMock.mock.calls.filter(([url]) => url === '/api/ai-chat');
        expect(sends).toHaveLength(1);
        const body = JSON.parse(sends[0][1]?.body as string);
        expect(body.article_id).toBe(articleId);
        expect(body.session_id).toBe(66);
        expect(body.article_content).toBe('Body B');
        expect(body.messages).toHaveLength(1);
        expect(wrapper.get('[data-testid="chat-stop-generation"]').exists()).toBe(true);
        expect(wrapper.text()).not.toContain('Stopped question for A');
        answerB.resolve(new Response(JSON.stringify({ response: 'Answer for B', session_id: 66 })));
        await flushPromises();
        expect(wrapper.text()).toContain('Answer for B');
        expect(
          wrapper.get('[data-testid="chat-context-article"]').attributes('data-context-article-id')
        ).toBe(String(articleId));
      } finally {
        wrapper.unmount();
        vi.unstubAllGlobals();
      }
    }
  );

  it('does not let a stopped history refresh overwrite a newer bound conversation', async () => {
    const history = deferred<Response>();
    const answerA = deferred<Response>();
    const answerB = deferred<Response>();
    let cancelled = false;
    const fetchMock = vi.fn((url: string, options?: RequestInit) => {
      if (url === '/api/ai/chat/session/create') {
        const body = JSON.parse(options?.body as string);
        return Promise.resolve(
          new Response(
            JSON.stringify({
              id: body.article_id === 12 ? 55 : 66,
              article_id: body.article_id,
              title: body.title,
            })
          )
        );
      }
      if (url === '/api/ai-chat')
        return JSON.parse(options?.body as string).article_id === 12
          ? answerA.promise
          : answerB.promise;
      if (url === '/api/ai-chat/cancel') {
        cancelled = true;
        return Promise.resolve(new Response('{}'));
      }
      if (cancelled && url.includes('/api/ai/chat/sessions?article_id=12')) return history.promise;
      return Promise.resolve(new Response('[]'));
    });
    vi.stubGlobal('fetch', fetchMock);
    const wrapper = mountChat();
    try {
      await flushPromises();
      await wrapper.get('input').setValue('Question A');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      await wrapper.get('[data-testid="chat-stop-generation"]').trigger('click');
      await flushPromises();
      await wrapper.setProps({
        article: { id: 13, title: 'Article B', url: 'https://example.com/b' } as Article,
        articleContent: 'Body B',
      });
      await wrapper.get('[data-testid="chat-new-context"]').trigger('click');
      await wrapper.get('input').setValue('Question B');
      await wrapper.get('[data-testid="chat-send-message"]').trigger('click');
      await flushPromises();
      history.resolve(
        new Response(JSON.stringify([{ id: 55, article_id: 12, title: 'Stale history A' }]))
      );
      answerA.resolve(new Response(JSON.stringify({ response: 'Stale answer A', session_id: 55 })));
      await flushPromises();
      expect(wrapper.get('[data-testid="chat-stop-generation"]').exists()).toBe(true);
      expect(
        wrapper.get('[data-testid="chat-context-article"]').attributes('data-context-article-id')
      ).toBe('13');
      expect(wrapper.text()).not.toContain('Stale answer A');
      expect(wrapper.text()).not.toContain('Stale history A');
      answerB.resolve(
        new Response(JSON.stringify({ response: 'Current answer B', session_id: 66 }))
      );
      await flushPromises();
      expect(wrapper.text()).toContain('Current answer B');
    } finally {
      wrapper.unmount();
      vi.unstubAllGlobals();
    }
  });
});
