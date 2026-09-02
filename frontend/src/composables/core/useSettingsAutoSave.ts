/**
 * Auto-save settings after the settings modal has finished loading.
 *
 * A single instance is mounted with the modal so switching tabs cannot create
 * duplicate watchers. Only fields that differ from the last successful save
 * are sent to the backend.
 */
import { computed, isRef, onUnmounted, ref, watch, type Ref } from 'vue';
import { useI18n } from 'vue-i18n';
import { getAutoRefreshInterval, useAppStore } from '@/stores/app';
import type { SettingsData } from '@/types/settings';
import { buildAutoSavePayload } from './useSettings.generated';
import { mergeSharedSettings } from './useSettings';

type SettingsPayload = Record<string, string>;
const immediateTypographyKeys = [
  'ui_font_family',
  'ui_font_size',
  'content_font_family',
  'content_font_size',
  'content_line_height',
] as const;

function diffPayload(current: SettingsPayload, baseline: SettingsPayload): SettingsPayload {
  return Object.fromEntries(
    Object.entries(current).filter(([key, value]) => baseline[key] !== value)
  );
}

export function useSettingsAutoSave(
  settings: Ref<SettingsData> | (() => SettingsData),
  ready: Readonly<Ref<boolean>> = ref(true)
) {
  const { locale, t } = useI18n();
  const store = useAppStore();
  const settingsRef = isRef(settings) ? settings : computed(settings);

  let baseline: SettingsPayload | null = null;
  let baselineSettings: SettingsData | null = null;
  let saveTimeout: ReturnType<typeof setTimeout> | null = null;
  let saveInFlight: Promise<void> | null = null;
  let saveQueued = false;

  function snapshot(): SettingsPayload {
    return buildAutoSavePayload(settingsRef);
  }

  function changed(changes: SettingsPayload, ...keys: string[]): boolean {
    return keys.some((key) => Object.prototype.hasOwnProperty.call(changes, key));
  }

  async function applySavedSideEffects(
    changes: SettingsPayload,
    savedSettings: SettingsData
  ): Promise<void> {
    if (changed(changes, 'language')) {
      locale.value = savedSettings.language;
    }
    if (changed(changes, 'theme')) {
      store.setTheme(savedSettings.theme as 'light' | 'dark' | 'auto');
    }
    if (changed(changes, 'update_interval', 'refresh_mode')) {
      store.startAutoRefresh(
        getAutoRefreshInterval(savedSettings.refresh_mode, savedSettings.update_interval)
      );
    }

    if (changed(changes, 'default_view_mode')) {
      window.dispatchEvent(
        new CustomEvent('default-view-mode-changed', {
          detail: { mode: savedSettings.default_view_mode },
        })
      );
    }

    const translationSettingsChanged = changed(
      changes,
      'translation_mode',
      'translation_provider',
      'target_language',
      'translation_only_mode'
    );
    const translationCacheScopeChanged = changed(
      changes,
      'translation_provider',
      'target_language'
    );

    if (translationCacheScopeChanged) {
      try {
        const response = await fetch('/api/articles/clear-translations', { method: 'POST' });
        if (!response.ok) {
          throw new Error(`Failed to clear translated titles: HTTP ${response.status}`);
        }
        await store.fetchArticles();
      } catch (error) {
        console.error('Error clearing translated titles after settings change:', error);
        window.showToast(t('setting.content.clearTranslationCacheFailed'), 'error');
      }
    }

    if (translationSettingsChanged) {
      window.dispatchEvent(
        new CustomEvent('translation-settings-changed', {
          detail: {
            mode: savedSettings.translation_mode,
            targetLang: savedSettings.target_language,
          },
        })
      );
    }

    if (changed(changes, 'show_hidden_articles')) {
      await store.fetchArticles();
    }
    if (changed(changes, 'show_article_preview_images')) {
      window.dispatchEvent(
        new CustomEvent('show-preview-images-changed', {
          detail: { value: savedSettings.show_article_preview_images },
        })
      );
    }
    if (changed(changes, 'image_gallery_enabled')) {
      window.dispatchEvent(
        new CustomEvent('image-gallery-setting-changed', {
          detail: { enabled: savedSettings.image_gallery_enabled },
        })
      );
    }
    if (changed(changes, 'auto_show_all_content')) {
      window.dispatchEvent(
        new CustomEvent('auto-show-all-content-changed', {
          detail: { value: savedSettings.auto_show_all_content },
        })
      );
    }
    if (changed(changes, 'layout_mode')) {
      window.dispatchEvent(
        new CustomEvent('layout-mode-changed', {
          detail: { mode: savedSettings.layout_mode },
        })
      );
    }
    if (
      changed(
        changes,
        'summary_enabled',
        'summary_provider',
        'summary_trigger_mode',
        'summary_length'
      )
    ) {
      window.dispatchEvent(
        new CustomEvent('summary-settings-changed', {
          detail: {
            enabled: savedSettings.summary_enabled,
            provider: savedSettings.summary_provider,
            triggerMode: savedSettings.summary_trigger_mode,
            length: savedSettings.summary_length,
          },
        })
      );
    }

    window.dispatchEvent(new CustomEvent('settings-updated', { detail: { autoSave: true } }));
  }

  async function persistCurrentChanges(): Promise<void> {
    if (!ready.value || baseline === null) {
      return;
    }
    if (saveInFlight) {
      saveQueued = true;
      return saveInFlight;
    }

    const changes = diffPayload(snapshot(), baseline);
    if (Object.keys(changes).length === 0) {
      return;
    }
    const settingsAtRequest = { ...settingsRef.value };

    const request = (async () => {
      try {
        const response = await fetch('/api/settings', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(changes),
        });
        if (!response.ok) {
          throw new Error(`Failed to save settings: HTTP ${response.status}`);
        }

        Object.assign(baseline as SettingsPayload, changes);
        const savedSettings = Object.fromEntries(
          Object.keys(changes).map((key) => [key, settingsAtRequest[key as keyof SettingsData]])
        ) as Partial<SettingsData>;
        if (baselineSettings) {
          Object.assign(baselineSettings, savedSettings);
        }
        mergeSharedSettings(savedSettings);
        await applySavedSideEffects(changes, settingsAtRequest);
      } catch (error) {
        console.error('Error auto-saving settings:', error);
        if (baselineSettings) {
          mergeSharedSettings(
            Object.fromEntries(
              immediateTypographyKeys.map((key) => [key, baselineSettings?.[key]])
            ) as Partial<SettingsData>
          );
        }
        window.showToast(t('common.errors.savingSettings'), 'error');
      }
    })();

    saveInFlight = request;
    try {
      await request;
    } finally {
      saveInFlight = null;
      if (saveQueued) {
        saveQueued = false;
        queueMicrotask(() => void persistCurrentChanges());
      }
    }
  }

  function debouncedAutoSave(): void {
    if (!ready.value || baseline === null) {
      return;
    }
    if (saveTimeout) {
      clearTimeout(saveTimeout);
    }
    saveTimeout = setTimeout(() => {
      saveTimeout = null;
      void persistCurrentChanges();
    }, 500);
  }

  watch(
    [() => ready.value, () => settingsRef.value],
    ([isReady]) => {
      if (!isReady) {
        baseline = null;
        baselineSettings = null;
        if (saveTimeout) {
          clearTimeout(saveTimeout);
          saveTimeout = null;
        }
        return;
      }

      if (baseline === null) {
        baseline = snapshot();
        baselineSettings = { ...settingsRef.value };
        return;
      }
      // Typography is a visual preference and must preview immediately. The
      // backend save remains debounced; a failed save restores the last
      // confirmed values in the shared application state.
      mergeSharedSettings(
        Object.fromEntries(
          immediateTypographyKeys.map((key) => [key, settingsRef.value[key]])
        ) as Partial<SettingsData>
      );
      debouncedAutoSave();
    },
    { deep: true, immediate: true }
  );

  onUnmounted(() => {
    if (saveTimeout) {
      clearTimeout(saveTimeout);
      saveTimeout = null;
    }
    void persistCurrentChanges();
  });

  return {
    autoSave: persistCurrentChanges,
    debouncedAutoSave,
  };
}
