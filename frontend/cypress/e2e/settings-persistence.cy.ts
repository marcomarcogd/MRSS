/// <reference types="cypress" />

describe('Settings Persistence', () => {
  beforeEach(() => {
    cy.fixture('settings').then((fixtureSettings) => {
      let settingsState: Record<string, string> = {
        ...fixtureSettings,
        language: 'en-US',
        theme: 'light',
        translation_mode: 'manual',
        update_check_enabled: 'false',
      };

      // Keep these persistence tests independent from a running desktop backend.
      cy.intercept('/api/**', { statusCode: 200, body: {} });
      cy.intercept('GET', '/api/settings', (req) => {
        req.reply({ statusCode: 200, body: settingsState });
      }).as('getSettings');
      cy.intercept('POST', '/api/settings', (req) => {
        settingsState = { ...settingsState, ...req.body };
        req.reply({ statusCode: 200, body: settingsState });
      }).as('saveSettings');
      cy.intercept('GET', '/api/feeds', { statusCode: 200, body: [] }).as('getFeeds');
      cy.intercept('GET', '/api/tags', { statusCode: 200, body: [] });
      cy.intercept('GET', '/api/saved-filters', { statusCode: 200, body: [] });
      cy.intercept({ method: 'GET', pathname: '/api/articles' }, { statusCode: 200, body: [] });
      cy.intercept('GET', '/api/articles/unread-counts', { statusCode: 200, body: {} });
      cy.intercept('GET', '/api/articles/filter-counts', { statusCode: 200, body: {} });
      cy.intercept('GET', '/api/progress', { statusCode: 200, body: { is_running: false } });

      cy.visit('/');
      cy.get('body').should('be.visible');
    });
  });

  it('should persist theme changes after closing and reopening settings', () => {
    // Wait for initial load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Open settings modal - find the gear icon button
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });

    // Wait for settings modal to be visible
    cy.contains(/settings|设置/i).should('be.visible');

    // Ensure we're on the general tab (or navigate to it)
    cy.contains(/general|常规/i).click({ force: true });

    cy.contains('.setting-item', /theme|主题/i)
      .find('button.select-trigger')
      .click();
    cy.contains('.select-option', /dark|暗色|深色/i).click({ force: true });
    cy.wait('@saveSettings', { timeout: 5000 }).its('request.body.theme').should('eq', 'dark');

    // Close the settings modal
    cy.get('body').type('{esc}');

    // Wait a bit for modal to close
    cy.wait(500);

    // Reopen settings to verify the change persisted
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });

    // Wait for settings to load again
    cy.wait('@getSettings');

    cy.contains('.setting-item', /theme|主题/i)
      .find('button.select-trigger')
      .should(($button) => {
        expect($button.text()).to.match(/dark|暗色|深色/i);
      });
  });

  it('should persist language changes', () => {
    // Wait for initial load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Open settings
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });

    // Navigate to general tab if not already there
    cy.contains(/general|常规/i).click({ force: true });

    // Wait for settings to load
    cy.wait('@getSettings');

    let expectedLanguage = '';
    cy.contains('.setting-item', /language|语言/i)
      .find('button.select-trigger')
      .invoke('text')
      .then((currentLanguage) => {
        const chooseChinese = /english|英语/i.test(currentLanguage);
        expectedLanguage = chooseChinese ? 'zh-CN' : 'en-US';
        cy.contains('.setting-item', /language|语言/i)
          .find('button.select-trigger')
          .click();
        cy.contains('.select-option', chooseChinese ? /chinese|中文/i : /english|英语/i).click({
          force: true,
        });
        cy.wait('@saveSettings', { timeout: 5000 })
          .its('request.body.language')
          .should('eq', expectedLanguage);
      });

    // Close settings
    cy.get('body').type('{esc}');
    cy.wait(500);

    // Reopen settings to verify
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });
    cy.wait('@getSettings');

    cy.contains('.setting-item', /language|语言/i)
      .find('button.select-trigger')
      .should(($button) => {
        const expectedLabel = expectedLanguage === 'zh-CN' ? /chinese|中文/i : /english|英语/i;
        expect($button.text()).to.match(expectedLabel);
      });
  });

  it('should persist daily report configuration through its dedicated API', () => {
    let dailyConfig = {
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
      system_notification: false,
      notify_on_empty: false,
    };
    cy.intercept('GET', '/api/daily-report/config', (req) => req.reply({ config: dailyConfig })).as(
      'getDailyReportConfig'
    );
    cy.intercept('PUT', '/api/daily-report/config', (req) => {
      dailyConfig = { ...dailyConfig, ...req.body };
      expect(req.body).not.to.have.property('created_at');
      expect(req.body).not.to.have.property('last_handled_boundary');
      req.reply({ config: dailyConfig });
    }).as('saveDailyReportConfig');
    cy.intercept('GET', '/api/daily-report/status', {
      enabled: false,
      is_running: false,
      progress: 0,
      unread_count: 0,
      missed_count: 0,
      notification_authorization: 'not_determined',
    });
    cy.intercept('GET', '/api/daily-report/history?*', {
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
    });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.reload();

    cy.get('button[title="24-Hour AI Digest"], button[title="24 小时 AI 日报"]').click({
      force: true,
    });
    cy.get('[data-testid="daily-report-view"]').should('be.visible');
    cy.get('button[title="Digest settings"], button[title="日报设置"]').click({ force: true });
    cy.wait('@getDailyReportConfig');
    cy.get('[data-testid="daily-report-config"] input[type="time"]').clear().type('09:30');
    cy.get('button')
      .contains(/^Save$|^保存$/)
      .click({ force: true });
    cy.wait('@saveDailyReportConfig').its('request.body.schedule_time').should('eq', '09:30');

    cy.get('button[title="Digest settings"], button[title="日报设置"]').click({ force: true });
    cy.wait('@getDailyReportConfig');
    cy.get('[data-testid="daily-report-config"] input[type="time"]').should('have.value', '09:30');
  });

  it('should require explicit cloud processing consent and support revocation', () => {
    let accepted = false;
    let optimizeCalls = 0;
    let optimizeShouldFail = false;
    const config = {
      enabled: false,
      schedule_time: '08:00',
      feed_scope: 'all',
      feed_ids: [],
      include_hidden: false,
      ai_profile_id: 12,
      focus: '',
      outline: [{ id: 'overview', title: 'Highlights', instruction: 'Summarize.' }],
      language: 'auto',
      title_template: '24-Hour AI Digest · {{date}}',
      in_app_notification: true,
      system_notification: false,
      notify_on_empty: false,
    };
    const cloudProcessing = () => ({
      disclosure_version: 1,
      required: true,
      accepted,
      accepted_version: accepted ? 1 : null,
      accepted_at: accepted ? '2026-08-19T08:00:00Z' : null,
      destination: {
        profile_id: 12,
        profile_name: 'Work AI',
        endpoint: 'https://api.example.com',
      },
    });
    cy.intercept('GET', '/api/daily-report/config', (req) =>
      req.reply({ config, cloud_processing: cloudProcessing() })
    );
    cy.intercept('GET', '/api/daily-report/consent', (req) =>
      req.reply({ cloud_processing: cloudProcessing() })
    );
    cy.intercept('POST', '/api/daily-report/consent', (req) => {
      if (req.body.action === 'grant') {
        expect(req.body).to.deep.equal({ action: 'grant', version: 1 });
        accepted = true;
      } else {
        expect(req.body).to.deep.equal({ action: 'revoke' });
        accepted = false;
      }
      req.reply({ cloud_processing: cloudProcessing() });
    }).as('updateCloudConsent');
    cy.intercept('POST', '/api/daily-report/outline/optimize', (req) => {
      optimizeCalls += 1;
      if (optimizeShouldFail) {
        req.reply({
          delay: 150,
          statusCode: 422,
          body: {
            error: {
              code: 'schema_invalid',
              message: 'outline repair still failed',
            },
          },
        });
        return;
      }
      req.reply({
        delay: 250,
        body: {
          outline: [
            { id: 'draft-news', title: 'Draft News', instruction: 'Prioritize key news.' },
          ],
        },
      });
    }).as('optimizeDailyReportOutline');
    cy.intercept('GET', '/api/daily-report/status', {
      enabled: false,
      is_running: false,
      progress: 0,
      unread_count: 0,
      missed_count: 0,
      notification_authorization: 'not_determined',
    });
    cy.intercept('GET', '/api/daily-report/history?*', {
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
    });
    cy.intercept('GET', '/api/ai/profiles', [
      {
        id: 12,
        name: 'Work AI',
        endpoint: 'https://api.example.com/v1/chat/completions',
        model: 'test-model',
        custom_headers: '',
        is_default: true,
        created_at: '',
        updated_at: '',
      },
      {
        id: 13,
        name: 'Changed AI',
        endpoint: 'https://changed.example.com/v1/chat/completions',
        model: 'changed-model',
        custom_headers: '',
        is_default: false,
        created_at: '',
        updated_at: '',
      },
    ]);
    cy.reload();

    cy.get('button[title="24-Hour AI Digest"], button[title="24 小时 AI 日报"]').click({
      force: true,
    });
    cy.get('button[title="Digest settings"], button[title="日报设置"]').click({ force: true });
    cy.get('[data-testid="daily-report-consent-status"]').should('contain.text', 'Work AI');

    // Optimizing with an unsaved profile must not call the backend or open the
    // consent prompt for the previously saved destination.
    cy.get('[data-testid="daily-report-profile-select"]').select('13');
    cy.get('[data-testid="daily-report-consent-status"]').should(
      'contain.text',
      'AI configuration changed'
    );
    cy.get('[data-testid="daily-report-consent-status"]')
      .contains('button', /Revoke consent|撤销授权/)
      .should('not.exist');
    cy.get('[data-testid="daily-report-optimize-outline"]').click();
    cy.contains(
      /The AI profile selection changed\. Save settings before optimizing the outline\.|AI 配置选择已更改，请先保存设置，再优化目录/
    ).should('be.visible');
    cy.then(() => expect(optimizeCalls).to.eq(0));
    cy.get('[data-testid="daily-report-cloud-consent"]').should('not.exist');
    cy.get('[data-testid="daily-report-profile-select"]').select('12');

    cy.contains('button', /Review and authorize|查看并授权/).click();
    cy.get('[data-testid="daily-report-cloud-consent"]').should('contain.text', 'Work AI');
    cy.get('[data-testid="daily-report-cloud-consent"]').should(
      'contain.text',
      'https://api.example.com'
    );
    cy.contains('button', /Agree and continue|同意并继续/).should('be.disabled');
    cy.get('[data-testid="daily-report-consent-checkbox"]').check();
    cy.contains('button', /Agree and continue|同意并继续/).click();
    cy.wait('@updateCloudConsent').its('request.body.action').should('eq', 'grant');
    cy.get('[data-testid="daily-report-consent-status"]').should(
      'contain.text',
      'Cloud processing authorized'
    );

    // The loading label must describe the actual action, prevent duplicate
    // requests, and keep the current outline unchanged until confirmation.
    cy.get('[data-testid="daily-report-config"] input[placeholder="Section title"]')
      .first()
      .should('have.value', 'Highlights');
    cy.get('[data-testid="daily-report-optimize-outline"]').click();
    cy.get('[data-testid="daily-report-optimize-outline"]')
      .should('be.disabled')
      .and('contain.text', 'Generating outline…')
      .click({ force: true });
    cy.wait('@optimizeDailyReportOutline');
    cy.then(() => expect(optimizeCalls).to.eq(1));
    cy.contains('AI outline draft (applied only after confirmation)').should('be.visible');
    cy.contains('Draft News').should('be.visible');
    cy.get('[data-testid="daily-report-config"] input[placeholder="Section title"]')
      .first()
      .should('have.value', 'Highlights');
    cy.contains('button', 'Use this draft').click();
    cy.get('[data-testid="daily-report-config"] input[placeholder="Section title"]')
      .first()
      .should('have.value', 'Draft News');

    // A failed optimization keeps the confirmed outline and shows the stable,
    // user-facing schema error instead of replacing it with an empty draft.
    cy.then(() => {
      optimizeShouldFail = true;
    });
    cy.get('[data-testid="daily-report-optimize-outline"]').click();
    cy.wait('@optimizeDailyReportOutline');
    cy.contains(
      'The AI returned an invalid outline format twice. Choose another model or edit the outline manually.'
    ).should('be.visible');
    cy.get('[data-testid="daily-report-config"] input[placeholder="Section title"]')
      .first()
      .should('have.value', 'Draft News');

    // Revoking consent must pause the schedule without replacing unrelated
    // unsaved modal edits. A changed profile is handled by save-time consent
    // instead of exposing the old destination's revoke action.
    cy.get('[data-testid="daily-report-config"] textarea[maxlength="2000"]')
      .last()
      .scrollIntoView()
      .clear()
      .type('Unsaved digest focus');
    cy.contains('button', /Revoke consent|撤销授权/).click();
    cy.get('[data-modal-open="true"]')
      .last()
      .contains('button', /Revoke consent|撤销授权/)
      .click();
    cy.wait('@updateCloudConsent').its('request.body.action').should('eq', 'revoke');
    cy.get('[data-testid="daily-report-consent-status"]').should(
      'contain.text',
      'Authorization required'
    );
    cy.get('[data-testid="daily-report-config"] textarea[maxlength="2000"]')
      .last()
      .scrollIntoView()
      .should('have.value', 'Unsaved digest focus');
    cy.get('[data-testid="daily-report-profile-select"]').select('13');
    cy.get('[data-testid="daily-report-consent-status"]').should(
      'contain.text',
      'AI configuration changed'
    );
    cy.get('[data-testid="daily-report-profile-select"]').should('have.value', '13');
  });

  it('should persist update interval changes', () => {
    // Wait for initial load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Open settings
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });

    // Navigate to general tab (update settings are here)
    cy.contains(/general|常规/i).click({ force: true });

    // Wait for settings to load
    cy.wait('@getSettings');

    cy.contains('.setting-item', /refresh mode|刷新模式/i)
      .scrollIntoView()
      .find('button.select-trigger')
      .click();
    cy.contains('.select-option', /fixed interval|固定间隔/i).click({ force: true });

    let expectedInterval = '';
    cy.contains('.sub-setting-item', /auto update interval|自动更新间隔/i)
      .scrollIntoView()
      .find('input[type="number"]')
      .invoke('val')
      .then((currentValue) => {
        expectedInterval = String(currentValue) === '31' ? '30' : '31';
        cy.contains('.sub-setting-item', /auto update interval|自动更新间隔/i)
          .find('input[type="number"]')
          .invoke('val', expectedInterval)
          .trigger('input')
          .trigger('change');
        cy.wait('@saveSettings', { timeout: 5000 })
          .its('request.body.update_interval')
          .should('eq', expectedInterval);
      });

    cy.get('body').type('{esc}');
    cy.wait(500);
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });
    cy.wait('@getSettings');
    cy.contains('.sub-setting-item', /auto update interval|自动更新间隔/i)
      .scrollIntoView()
      .find('input[type="number"]')
      .should(($input) => {
        expect($input.val()).to.equal(expectedInterval);
      });
  });

  it('should handle multiple setting changes in sequence', () => {
    // Wait for initial load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Open settings
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });
    cy.wait('@getSettings');

    // Change theme
    cy.contains(/general|常规/i).click({ force: true });
    cy.get('body').then(($body) => {
      if ($body.find(/light|亮色/i).length > 0) {
        cy.contains(/light|亮色/i).click({ force: true });
        cy.wait(1000);
      }
    });

    // Navigate to another tab
    cy.get('body').then(($body) => {
      if ($body.find(/feeds|订阅/i).length > 0) {
        cy.contains(/feeds|订阅/i).click({ force: true });
        cy.wait(500);

        // Just verify the tab is open (no number input on feeds tab)
        cy.contains(/feeds|订阅/i).should('exist');
      }
    });

    // Close and reopen
    cy.get('body').type('{esc}');
    cy.wait(500);

    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });
    cy.wait('@getSettings');

    // Verify settings modal is open
    cy.contains(/settings|设置/i).should('be.visible');
  });

  it('should save settings when switching between tabs', () => {
    // Wait for initial load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Open settings
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });
    cy.wait('@getSettings');

    let expectedTheme = '';
    cy.contains(/general|常规/i).click({ force: true });
    cy.contains('.setting-item', /theme|主题/i)
      .find('button.select-trigger')
      .invoke('text')
      .then((currentTheme) => {
        const chooseDark = !/dark|暗色|深色/i.test(currentTheme);
        expectedTheme = chooseDark ? 'dark' : 'light';
        cy.contains('.setting-item', /theme|主题/i)
          .find('button.select-trigger')
          .click();
        cy.contains('.select-option', chooseDark ? /dark|暗色|深色/i : /light|亮色/i).click({
          force: true,
        });
        cy.contains('button', /^(Feeds|订阅)$/i).click({ force: true });
        cy.wait('@saveSettings', { timeout: 5000 })
          .its('request.body.theme')
          .should('eq', expectedTheme);
      });

    // Close settings
    cy.get('body').type('{esc}');

    // Reopen and verify the change was saved
    cy.wait(500);
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });
    cy.wait('@getSettings');

    cy.contains(/general|常规/i).click({ force: true });
    cy.contains('.setting-item', /theme|主题/i)
      .find('button.select-trigger')
      .should(($button) => {
        const expectedLabel = expectedTheme === 'dark' ? /dark|暗色|深色/i : /light|亮色/i;
        expect($button.text()).to.match(expectedLabel);
      });
  });

  it('updates the AI usage limit immediately and refreshes usage while visible', () => {
    let usage = 120;
    let savedLimit = '200';
    let usageRequests = 0;

    cy.intercept('GET', '/api/ai/profiles', { statusCode: 200, body: [] });
    cy.intercept('GET', '/api/ai-usage', (req) => {
      usageRequests += 1;
      const limit = Number(savedLimit);
      req.reply({
        statusCode: 200,
        body: {
          usage,
          limit,
          limit_reached: limit > 0 && usage >= limit,
        },
      });
    }).as('getAIUsage');
    cy.intercept('POST', '/api/settings', (req) => {
      if (req.body.ai_usage_limit !== undefined) {
        savedLimit = String(req.body.ai_usage_limit);
      }
      req.reply({ statusCode: 200, body: req.body });
    }).as('saveAIUsageLimit');

    cy.clock();
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });
    cy.wait('@getSettings');
    cy.contains('button', /^AI$/i).click({ force: true });
    cy.wait('@getAIUsage');

    cy.get('[data-testid="ai-usage-status"]').should('contain.text', '120 / 200');
    cy.get('[data-testid="ai-usage-limit-input"]').clear().type('400');
    cy.get('[data-testid="ai-usage-status"]').should('contain.text', '120 / 400');

    cy.tick(500);
    cy.wait('@saveAIUsageLimit').its('request.body.ai_usage_limit').should('eq', '400');
    cy.wait('@getAIUsage');
    cy.get('[data-testid="ai-usage-status"]').should('contain.text', '120 / 400');

    cy.then(() => {
      usage = 300;
    });
    cy.tick(15_000);
    cy.wait('@getAIUsage');
    cy.get('[data-testid="ai-usage-status"]').should('contain.text', '300 / 400');

    let requestsBeforeUnmount = 0;
    cy.then(() => {
      requestsBeforeUnmount = usageRequests;
    });
    cy.contains('button', /^(General|常规)$/i).click({ force: true });
    cy.tick(30_000);
    cy.then(() => {
      expect(usageRequests).to.eq(requestsBeforeUnmount);
    });
  });

  it('should switch away from card layout without per-article settings requests', () => {
    let layoutMode = 'card';
    let settingsGetCount = 0;
    let settingsRequestsBeforeSwitch = 0;

    const settingsResponse = () => ({
      language: 'en-US',
      layout_mode: layoutMode,
      update_check_enabled: 'false',
      update_interval: '10',
      last_global_refresh: new Date().toISOString(),
    });

    const articles = Array.from({ length: 24 }, (_, index) => ({
      id: index + 1,
      feed_id: 1,
      feed_title: 'Test Feed',
      title: `Test Article ${index + 1}`,
      url: `https://example.com/articles/${index + 1}`,
      published_at: '2026-08-13T00:00:00Z',
      image_url: '',
      translated_title: '',
      is_read: false,
      is_favorite: false,
      is_hidden: false,
      is_read_later: false,
    }));

    // Reload with deterministic API state so the regression does not depend on local data.
    cy.intercept('/api/**', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/feeds', { statusCode: 200, body: [] }).as('layoutFeeds');
    cy.intercept('GET', '/api/tags', { statusCode: 200, body: [] });
    cy.intercept('GET', '/api/saved-filters', { statusCode: 200, body: [] });
    cy.intercept(
      { method: 'GET', pathname: '/api/articles' },
      {
        statusCode: 200,
        body: articles,
      }
    ).as('layoutArticles');
    cy.intercept('GET', '/api/articles/unread-counts', {
      statusCode: 200,
      body: { total: 0, feeds: {}, categories: {} },
    });
    cy.intercept('GET', '/api/articles/filter-counts', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/progress', { statusCode: 200, body: { is_running: false } });
    cy.intercept('GET', '/api/settings', (req) => {
      settingsGetCount += 1;
      req.reply({ statusCode: 200, body: settingsResponse() });
    }).as('layoutSettings');
    cy.intercept('POST', '/api/settings', (req) => {
      layoutMode = req.body.layout_mode;
      req.reply({ statusCode: 200, body: { success: true } });
    }).as('saveLayoutSettings');

    const layoutSelector = () =>
      cy.contains('.setting-item', 'Article List Layout').find('button.select-trigger');

    const selectLayout = (label: string) => {
      layoutSelector().click();
      cy.contains('.select-option', label).click({ force: true });
    };

    cy.reload();
    cy.wait('@layoutFeeds');
    cy.wait('@layoutArticles');

    cy.get('button[title="Settings"]').click();
    cy.contains('button', /^Reading$/).click();
    layoutSelector().should('contain.text', 'Card');
    cy.wait(150);

    cy.then(() => {
      settingsRequestsBeforeSwitch = settingsGetCount;
    });

    selectLayout('Normal');
    cy.wait('@saveLayoutSettings').its('request.body.layout_mode').should('eq', 'normal');
    layoutSelector().should('contain.text', 'Normal');
    cy.then(() => {
      expect(settingsGetCount - settingsRequestsBeforeSwitch).to.be.at.most(5);
    });

    selectLayout('Compact');
    cy.wait('@saveLayoutSettings').its('request.body.layout_mode').should('eq', 'compact');
    layoutSelector().should('contain.text', 'Compact');

    selectLayout('Card');
    cy.wait('@saveLayoutSettings').its('request.body.layout_mode').should('eq', 'card');
    layoutSelector().should('contain.text', 'Card');

    selectLayout('Normal');
    cy.wait('@saveLayoutSettings').its('request.body.layout_mode').should('eq', 'normal');
    layoutSelector().should('contain.text', 'Normal');

    cy.get('body').type('{esc}');
    cy.reload();
    cy.wait('@layoutFeeds');
    cy.wait('@layoutArticles');
    cy.get('button[title="Settings"]').click();
    cy.contains('button', /^Reading$/).click();
    layoutSelector().should('contain.text', 'Normal');
    cy.then(() => {
      expect(layoutMode).to.eq('normal');
    });
  });

  it('should apply and persist interface typography without changing article content typography', () => {
    let settingsState: Record<string, string> = {
      language: 'en-US',
      theme: 'light',
      layout_mode: 'normal',
      ui_font_family: 'system',
      ui_font_size: '16',
      content_font_family: 'system',
      content_font_size: '16',
      content_line_height: '1.6',
      default_view_mode: 'rendered',
      update_check_enabled: 'false',
      update_interval: '10',
      last_global_refresh: new Date().toISOString(),
    };

    const feed = {
      id: 1,
      url: 'https://example.com/feed.xml',
      title: 'Typography Test Feed',
      category: '',
      last_fetched_at: '2026-08-13T00:00:00Z',
    };
    const article = {
      id: 1,
      feed_id: 1,
      feed_title: feed.title,
      title: 'Typography Test Article',
      url: 'https://example.com/article',
      published_at: '2026-08-13T00:00:00Z',
      is_read: false,
      is_favorite: false,
      is_hidden: false,
      is_read_later: false,
    };

    cy.intercept('/api/**', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/settings', (req) => {
      req.reply({ statusCode: 200, body: settingsState });
    }).as('typographySettings');
    cy.intercept('POST', '/api/settings', (req) => {
      settingsState = { ...settingsState, ...req.body };
      req.reply({ statusCode: 200, body: { success: true } });
    }).as('saveTypographySettings');
    cy.intercept('GET', '/api/feeds', { statusCode: 200, body: [feed] }).as('typographyFeeds');
    cy.intercept(
      { method: 'GET', pathname: '/api/articles' },
      { statusCode: 200, body: [article] }
    ).as('typographyArticles');
    cy.intercept('GET', '/api/tags', { statusCode: 200, body: [] });
    cy.intercept('GET', '/api/articles/unread-counts', {
      statusCode: 200,
      body: { total: 1, feed_counts: { 1: 1 } },
    });
    cy.intercept('GET', '/api/articles/filter-counts', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/progress', { statusCode: 200, body: { is_running: false } });
    cy.intercept('GET', '/api/articles/content*', {
      statusCode: 200,
      body: { content: '<p>Independent article content</p>', cached: true },
    });

    cy.reload();
    cy.wait('@typographyFeeds');
    cy.wait('@typographyArticles');

    let initialArticleTitleSize = 0;
    cy.get('.article-card .article-title')
      .should('be.visible')
      .then(($title) => {
        initialArticleTitleSize = parseFloat(getComputedStyle($title[0]).fontSize);
      });

    cy.get('button[title="Settings"]').click();
    cy.contains('button', /^General$/).click();

    cy.get('.setting-item').then(($items) => {
      const labels = [...$items].map((item) => item.textContent || '');
      const languageIndex = labels.findIndex((label) => label.includes('Language'));
      const interfaceFontIndex = labels.findIndex((label) =>
        label.includes('Interface Font Family')
      );
      const interfaceSizeIndex = labels.findIndex((label) => label.includes('Interface Font Size'));

      expect(languageIndex).to.be.greaterThan(-1);
      expect(interfaceFontIndex).to.equal(languageIndex + 1);
      expect(interfaceSizeIndex).to.equal(interfaceFontIndex + 1);
    });

    let interfaceFontOptions: string[] = [];

    cy.contains('.setting-item', 'Interface Font Family').within(() => {
      cy.get('button.select-trigger').click();
    });
    cy.get('.select-option').then(($options) => {
      interfaceFontOptions = [...$options].map((option) => option.textContent?.trim() || '');
      expect(interfaceFontOptions).to.include('System Default');
    });
    cy.contains('.select-option', 'Default Serif').click({ force: true });
    cy.wait('@saveTypographySettings').its('request.body.ui_font_family').should('eq', 'serif');

    cy.contains('.setting-item', 'Interface Font Size').within(() => {
      cy.get('input[type="number"]').invoke('val', '20').trigger('input').trigger('change');
    });
    cy.wait('@saveTypographySettings').its('request.body.ui_font_size').should('eq', '20');

    cy.document().then((doc) => {
      expect(doc.documentElement.style.getPropertyValue('--ui-font-size')).to.equal('20px');
      expect(doc.documentElement.style.getPropertyValue('--ui-font-scale')).to.equal('1.25');
      expect(doc.documentElement.style.getPropertyValue('--ui-font-family')).to.contain('Georgia');
    });
    cy.get('.feed-item').should(($feedItem) => {
      expect(getComputedStyle($feedItem[0]).fontFamily).to.contain('Georgia');
    });
    cy.get('.article-card .article-title').should(($title) => {
      expect(parseFloat(getComputedStyle($title[0]).fontSize)).to.be.greaterThan(
        initialArticleTitleSize
      );
    });

    cy.get('body').type('{esc}');
    cy.window().then((win) => {
      win.localStorage.setItem('mrrssArticleScrollPositions', JSON.stringify({ 1: 42 }));
      win.localStorage.removeItem('mrssArticleScrollPositions');
    });
    cy.reload();
    cy.wait('@typographyFeeds');
    cy.wait('@typographyArticles');
    cy.window().then((win) => {
      expect(win.localStorage.getItem('mrssArticleScrollPositions')).to.equal(
        JSON.stringify({ 1: 42 })
      );
      expect(win.localStorage.getItem('mrrssArticleScrollPositions')).to.equal(null);
    });
    cy.get('.article-card').first().click();
    cy.get('.prose-content').should(($content) => {
      const style = getComputedStyle($content[0]);
      expect(style.fontSize).to.equal('16px');
      expect(style.fontFamily).to.contain('Inter');
      expect(style.fontFamily).not.to.contain('Georgia');
    });
    cy.get('button[title="Settings"]').click();
    cy.contains('button', /^General$/).click();
    cy.contains('.setting-item', 'Interface Font Family')
      .find('button.select-trigger')
      .should('contain.text', 'Default Serif');
    cy.contains('.setting-item', 'Interface Font Size')
      .find('input[type="number"]')
      .should('have.value', '20');

    cy.contains('button', /^Reading$/).click();
    cy.contains('.setting-item', 'Interface Font Family').should('not.exist');
    cy.contains('.setting-item', 'Interface Font Size').should('not.exist');
    cy.contains('.setting-item', 'Content Font Family').within(() => {
      cy.get('button.select-trigger').click();
    });
    cy.get('.select-option').then(($options) => {
      const contentFontOptions = [...$options].map((option) => option.textContent?.trim() || '');
      expect(contentFontOptions).to.deep.equal(interfaceFontOptions);
    });
    cy.contains('.select-option', 'System Default').click({ force: true });

    cy.get('body').type('{esc}');
    cy.reload();
    cy.wait('@typographyFeeds');
    cy.document().then((doc) => {
      expect(doc.documentElement.style.getPropertyValue('--ui-font-size')).to.equal('20px');
      expect(doc.documentElement.style.getPropertyValue('--ui-font-family')).to.contain('Georgia');
    });
  });

  it('should not save on open, tab switches, or clean close and should flush only dirty fields', () => {
    let settingsState: Record<string, string> = {
      language: 'en-US',
      theme: 'light',
      translation_mode: 'manual',
      update_check_enabled: 'false',
      update_interval: '30',
    };
    const savedBodies: Record<string, string>[] = [];

    cy.intercept('/api/**', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/settings', (req) => {
      req.reply({ statusCode: 200, body: settingsState });
    }).as('baselineSettings');
    cy.intercept('POST', '/api/settings', (req) => {
      savedBodies.push(req.body);
      settingsState = { ...settingsState, ...req.body };
      req.reply({ statusCode: 200, body: settingsState });
    }).as('saveDirtySettings');
    cy.intercept('GET', '/api/feeds', { statusCode: 200, body: [] }).as('baselineFeeds');
    cy.intercept('GET', '/api/tags', { statusCode: 200, body: [] });
    cy.intercept('GET', '/api/saved-filters', { statusCode: 200, body: [] });
    cy.intercept({ method: 'GET', pathname: '/api/articles' }, { statusCode: 200, body: [] });
    cy.intercept('GET', '/api/articles/unread-counts', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/articles/filter-counts', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/progress', { statusCode: 200, body: { is_running: false } });

    cy.reload();
    cy.wait('@baselineFeeds');
    cy.get('button[title="Settings"]').click();
    cy.wait('@baselineSettings');
    cy.wait(700);
    cy.contains('button', /^Feeds$/).click();
    cy.contains('button', /^Reading$/).click();
    cy.contains('button', /^General$/).click();
    cy.get('body').type('{esc}');
    cy.wait(700);
    cy.then(() => expect(savedBodies).to.have.length(0));

    cy.get('button[title="Settings"]').click();
    cy.wait('@baselineSettings');
    cy.contains('.setting-item', 'Theme').find('button.select-trigger').click();
    cy.contains('.select-option', 'Dark').click({ force: true });
    // Close before the debounce expires: unmount must flush the one dirty key.
    cy.get('body').type('{esc}');
    cy.wait('@saveDirtySettings').then(({ request }) => {
      expect(request.body).to.deep.equal({ theme: 'dark' });
    });
    cy.then(() => expect(savedBodies).to.have.length(1));
  });

  it('should persist manual, automatic, and off translation modes', () => {
    let settingsState: Record<string, string> = {
      language: 'en-US',
      theme: 'light',
      translation_mode: 'manual',
      translation_only_mode: 'false',
      translation_provider: 'google',
      target_language: 'zh',
      update_check_enabled: 'false',
      update_interval: '30',
    };

    cy.intercept('/api/**', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/settings', (req) => {
      req.reply({ statusCode: 200, body: settingsState });
    }).as('translationModeSettings');
    cy.intercept('POST', '/api/settings', (req) => {
      settingsState = { ...settingsState, ...req.body };
      req.reply({ statusCode: 200, body: settingsState });
    }).as('saveTranslationMode');
    cy.intercept('GET', '/api/feeds', { statusCode: 200, body: [] }).as('translationModeFeeds');
    cy.intercept('GET', '/api/tags', { statusCode: 200, body: [] });
    cy.intercept('GET', '/api/saved-filters', { statusCode: 200, body: [] });
    cy.intercept({ method: 'GET', pathname: '/api/articles' }, { statusCode: 200, body: [] });
    cy.intercept('GET', '/api/articles/unread-counts', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/articles/filter-counts', { statusCode: 200, body: {} });
    cy.intercept('GET', '/api/progress', { statusCode: 200, body: { is_running: false } });

    cy.reload();
    cy.wait('@translationModeFeeds');
    cy.get('button[title="Settings"]').click();
    cy.contains('button', /^Content$/).click();

    cy.contains('label', 'Manual translation').find('input[type="radio"]').should('be.checked');
    cy.contains('label', 'Automatic translation').click();
    cy.wait('@saveTranslationMode').its('request.body.translation_mode').should('eq', 'auto');

    cy.get('body').type('{esc}');
    cy.get('[data-settings-modal="true"]').should('not.exist');
    cy.get('button[title="Settings"]').click();
    cy.wait('@translationModeSettings');
    cy.contains('button', /^Content$/).click();
    cy.contains('label', 'Automatic translation').find('input[type="radio"]').should('be.checked');

    cy.contains('label', 'Translation off').click();
    cy.wait('@saveTranslationMode').its('request.body.translation_mode').should('eq', 'off');
    cy.contains('.sub-setting-item', 'Translation Provider').should('not.exist');
  });
});
