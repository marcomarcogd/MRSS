// src/test/setup.ts
import { vi } from 'vitest';

const localStorageValues = new Map<string, string>();
Object.defineProperty(window, 'localStorage', {
  configurable: true,
  value: {
    getItem: (key: string) => localStorageValues.get(key) ?? null,
    setItem: (key: string, value: string) => localStorageValues.set(key, String(value)),
    removeItem: (key: string) => localStorageValues.delete(key),
    clear: () => localStorageValues.clear(),
    key: (index: number) => [...localStorageValues.keys()][index] ?? null,
    get length() {
      return localStorageValues.size;
    },
  },
});

// Mock window.matchMedia
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => {},
  }),
});

// Mock fetch only in test environment
const originalFetch = global.fetch;
global.fetch = vi.fn((input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
  // Convert input to string for URL matching
  const url = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;

  // Helper to create mock Response
  const createMockResponse = (data: any): Response => ({
    ok: true,
    status: 200,
    statusText: 'OK',
    headers: new Headers({ 'content-type': 'application/json' }),
    json: () => Promise.resolve(data),
    text: () => Promise.resolve(JSON.stringify(data)),
    bytes: () => Promise.resolve(new Uint8Array(new TextEncoder().encode(JSON.stringify(data)))),
    clone: () => createMockResponse(data),
    body: null,
    bodyUsed: false,
    arrayBuffer: () => Promise.resolve(new ArrayBuffer(0)),
    blob: () => Promise.resolve(new Blob()),
    formData: () => Promise.resolve(new FormData()),
    redirected: false,
    type: 'basic' as ResponseType,
    url: url,
  });

  // Mock successful responses for common API calls
  if (url === '/api/settings') {
    return Promise.resolve(
      createMockResponse({
        theme: 'light',
        update_interval: '30',
        last_global_refresh: '',
        auto_update: false,
        shortcuts: '{}',
        image_gallery_enabled: 'false',
        translation_mode: 'manual',
        target_language: 'en',
        show_article_preview_images: 'false',
        default_view_mode: 'original',
      })
    );
  }

  if (url === '/api/progress') {
    return Promise.resolve(
      createMockResponse({
        is_running: false,
        current: 0,
        total: 0,
        message: '',
      })
    );
  }

  if (url === '/api/window/state') {
    return Promise.resolve(
      createMockResponse({
        width: 1200,
        height: 800,
        x: 100,
        y: 100,
        maximized: false,
      })
    );
  }

  if (url === '/api/daily-report/config') {
    return Promise.resolve(
      createMockResponse({
        config: {
          enabled: false,
          schedule_time: '08:00',
          feed_scope: 'all',
          feed_ids: [],
          include_hidden: false,
          ai_profile_id: null,
          focus: '',
          outline: [{ id: 'overview', title: 'Highlights', instruction: 'Summarize.' }],
          language: 'auto',
          title_template: '24-Hour AI Digest · {{date}}',
          in_app_notification: true,
          system_notification: true,
          notify_on_empty: false,
        },
        cloud_processing: {
          disclosure_version: 1,
          required: false,
          accepted: true,
          accepted_version: null,
          accepted_at: null,
          destination: null,
        },
      })
    );
  }

  if (url === '/api/daily-report/consent') {
    return Promise.resolve(
      createMockResponse({
        cloud_processing: {
          disclosure_version: 1,
          required: false,
          accepted: true,
          accepted_version: null,
          accepted_at: null,
          destination: null,
        },
      })
    );
  }

  if (url === '/api/daily-report/status') {
    return Promise.resolve(
      createMockResponse({
        enabled: false,
        is_running: false,
        progress: 0,
        unread_count: 0,
        missed_count: 0,
        notification_authorization: 'not_determined',
      })
    );
  }

  if (url.startsWith('/api/daily-report/history')) {
    return Promise.resolve(createMockResponse({ items: [], total: 0, page: 1, page_size: 20 }));
  }

  // For any other URLs, fall back to original fetch if available
  // This ensures tests can still make real HTTP calls if needed
  if (originalFetch) {
    return originalFetch(input, init);
  }

  // Default mock response for unknown URLs
  return Promise.resolve(createMockResponse({}));
});

// Mock window.showToast, window.showConfirm, etc.
Object.defineProperty(window, 'showToast', {
  writable: true,
  value: () => {},
});

Object.defineProperty(window, 'showConfirm', {
  writable: true,
  value: () => Promise.resolve(true),
});

// Mock ResizeObserver
global.ResizeObserver = class ResizeObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
};
