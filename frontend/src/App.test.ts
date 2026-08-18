import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { nextTick } from 'vue';
import { mount } from '@vue/test-utils';
import { createPinia } from 'pinia';
import { createI18n } from 'vue-i18n';
import en from './i18n/locales/en';
import App from './App.vue';
import { setSettingsFromRawData } from './composables/core/useSettings';
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
