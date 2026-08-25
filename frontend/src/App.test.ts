import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { nextTick } from 'vue';
import { mount } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createI18n } from 'vue-i18n';
import en from './i18n/locales/en';
import zh from './i18n/locales/zh';
import App from './App.vue';
import ActivityBar from './components/sidebar/ActivityBar.vue';
import DailyReportCloudConsentModal from './components/dailyReport/DailyReportCloudConsentModal.vue';
import { useAppStore } from './stores/app';
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

describe('App', () => {
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
    expect(toast).toContain('calc(100vw-2rem)');
    expect(chat).not.toContain('v-html="msg.html || msg.content"');
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
