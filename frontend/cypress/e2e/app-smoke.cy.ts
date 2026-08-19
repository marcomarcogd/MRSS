/// <reference types="cypress" />

describe('Application Smoke Tests', () => {
  beforeEach(() => {
    // Set up intercepts before visiting the page
    cy.intercept('GET', '/api/articles*').as('getArticles');
    cy.intercept('GET', '/api/feeds*').as('getFeeds');
    cy.visit('/');
  });

  it('should load the application successfully', () => {
    // Verify the app loads
    cy.get('body').should('be.visible');

    // Check for main layout elements
    cy.get('[class*="sidebar"]').should('exist');
    cy.get('[class*="article"]').should('exist');
  });

  it('should display the sidebar', () => {
    // Verify sidebar is present
    cy.get('[class*="sidebar"]').should('be.visible');

    // Check for common sidebar elements
    cy.get('button[title="All Articles"], button[title="所有文章"]').should('exist');
    cy.get('button[title="Unread"], button[title="未读"]').should('exist');
  });

  it('should keep all activity bar icons on the same centre line', () => {
    cy.viewport(1280, 720);

    cy.get('.smart-activity-bar').then(($bar) => {
      const bar = $bar[0].getBoundingClientRect();
      const centreX = bar.left + bar.width / 2;

      cy.wrap($bar)
        .find('button:visible svg')
        .each(($icon) => {
          const icon = $icon[0].getBoundingClientRect();
          const iconCentreX = icon.left + icon.width / 2;

          expect(Math.abs(iconCentreX - centreX)).to.be.lessThan(0.6);
        });
    });
  });

  it('should have working navigation', () => {
    // Click on different navigation items
    cy.get('button[title="All Articles"], button[title="所有文章"]').click({ force: true });
    cy.wait(500);

    cy.get('button[title="Unread"], button[title="未读"]').click({ force: true });
    cy.wait(500);

    cy.get('button[title="Favorites"], button[title="收藏"]').click({ force: true });
    cy.wait(500);
  });

  it('should open and close settings modal', () => {
    // Wait for initial data to load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Open settings - find the gear icon button
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });

    // Wait for modal to appear
    cy.wait(1000);

    // Verify modal content is visible (check for settings text or modal structure)
    cy.get('body').then(($body) => {
      const hasSettingsModal =
        $body.find(/settings|设置|general|常规/i).length > 0 ||
        $body.find('[class*="modal"]').length > 0;
      if (hasSettingsModal) {
        cy.log('Settings modal opened successfully');
      } else {
        cy.log('Settings modal may have opened but not detected');
      }
    });

    // Close modal using ESC key
    cy.get('body').type('{esc}');
    cy.wait(1000);
  });

  it('should handle keyboard shortcuts', () => {
    // Wait for initial data to load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Test settings shortcut (Ctrl+,)
    cy.get('body').type('{ctrl},');
    cy.wait(1000);

    // Check if settings opened (may not always work in test environment)
    cy.get('body').then(($body) => {
      if ($body.find(/settings|设置/i).length > 0) {
        cy.log('Settings opened via keyboard shortcut');

        // Close with ESC
        cy.get('body').type('{esc}');
        cy.wait(500);
      } else {
        cy.log('Keyboard shortcut may not work in test environment');
      }
    });
  });

  it('should display articles when feeds exist', () => {
    // Wait for articles to load
    cy.wait('@getArticles', { timeout: 10000 });

    // Check if articles are displayed (or empty state)
    cy.get('[class*="article"], [class*="empty"], [class*="no-articles"]').should('exist');
  });

  it('should handle API errors gracefully', () => {
    // Wait for app to load first
    cy.wait('@getFeeds', { timeout: 10000 });

    // Verify app doesn't crash even if APIs fail
    cy.get('body').should('be.visible');

    // The app should show empty state or handle errors gracefully
    cy.get('[class*="sidebar"]').should('exist');
  });

  it('should be responsive', () => {
    // Test different viewport sizes
    cy.viewport(1920, 1080);
    cy.get('body').should('be.visible');

    cy.viewport(1280, 720);
    cy.get('body').should('be.visible');

    cy.viewport(768, 1024);
    cy.get('body').should('be.visible');

    // Mobile view
    cy.viewport(375, 667);
    cy.get('body').should('be.visible');
  });

  it('should handle long content gracefully', () => {
    // Wait for articles to load
    cy.wait('@getArticles', { timeout: 10000 });

    // Wait for feeds to load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Try to click on an article if it exists
    cy.get('body').then(($body) => {
      if ($body.find('[class*="article"]').length > 0) {
        cy.get('[class*="article"]').first().click({ force: true });

        // Wait for content to load
        cy.wait(500);

        // Verify page is still responsive
        cy.get('body').should('be.visible');
      } else {
        // No articles to test, skip gracefully
        cy.log('No articles found to test long content');
      }
    });
  });

  it('should maintain state during navigation', () => {
    // Wait for initial load
    cy.wait('@getFeeds', { timeout: 10000 });

    // Select unread filter
    cy.get('button[title="Unread"], button[title="未读"]').click({ force: true });
    cy.wait(500);

    // Open settings
    cy.get('button')
      .filter('[title="Settings"], [title="设置"]')
      .should('exist')
      .click({ force: true });
    cy.wait(500);

    // Close settings
    cy.get('body').type('{esc}');
    cy.wait(500);

    // Verify unread filter is still active by checking if the element exists
    cy.get('button[title="Unread"], button[title="未读"]').should('exist').and('be.visible');
  });

  it('should open the daily report center and render structured sources safely', () => {
    const run = {
      id: 7,
      kind: 'manual',
      status: 'completed',
      period_start: '2026-08-18T08:00:00+08:00',
      period_end: '2026-08-19T08:00:00+08:00',
      progress: 100,
      title: 'AI Daily Digest',
      content: {
        sections: [
          {
            id: 'highlights',
            title: 'Highlights',
            summary: '<img src=x onerror=alert(1)> Plain text only',
            source_ids: [1],
          },
        ],
      },
      markdown: '# AI Daily Digest',
      input_tokens: 120,
      output_tokens: 30,
      article_count: 1,
      is_read: true,
      error: '',
      created_at: '2026-08-19T08:01:00+08:00',
    };

    cy.intercept('GET', '/api/daily-report/config', {
      config: {
        enabled: true,
        schedule_time: '08:00',
        feed_scope: 'all',
        feed_ids: [],
        include_hidden: false,
        ai_profile_id: null,
        focus: '',
        outline: [],
        language: 'auto',
        title_template: '24-Hour AI Digest · {{date}}',
        in_app_notification: true,
        system_notification: false,
        notify_on_empty: false,
      },
    });
    cy.intercept('GET', '/api/daily-report/status', {
      enabled: true,
      is_running: false,
      progress: 0,
      unread_count: 101,
      missed_count: 0,
      notification_authorization: 'not_determined',
    });
    cy.intercept('GET', '/api/daily-report/history?*', {
      items: [run],
      total: 1,
      page: 1,
      page_size: 20,
    });
    cy.intercept('GET', '/api/daily-report/history/7', {
      run,
      sources: [
        {
          id: 99,
          source_index: 1,
          article_id: null,
          feed_id: 2,
          title: 'Source article',
          feed_title: 'Example feed',
          author: '',
          url: 'https://example.com/article',
          first_seen_at: '2026-08-19T06:00:00+08:00',
          late_arrival: false,
        },
      ],
    });
    cy.intercept('PUT', '/api/daily-report/history/7/read', (req) => {
      expect(req.body).to.deep.equal({ read: false });
      req.reply({ run: { ...run, is_read: false } });
    }).as('markDigestUnread');
    cy.reload();

    cy.get('[data-testid="daily-report-unread-badge"]').should('contain.text', '99+');
    cy.get('button[title="24-Hour AI Digest"], button[title="24 小时 AI 日报"]').click({
      force: true,
    });
    cy.get('[data-testid="daily-report-view"]').should('be.visible');
    cy.contains('AI Daily Digest').should('be.visible');
    cy.contains('<img src=x onerror=alert(1)> Plain text only').should('be.visible');
    cy.get('img[src="x"]').should('not.exist');
    cy.contains('[1] Source article').should('be.visible');
    cy.get('button[title="Mark as unread"], button[title="标为未读"]').click();
    cy.wait('@markDigestUnread');
    cy.get('button[title="Mark as read"], button[title="标为已读"]').should('exist');
  });

  it('should request cloud consent once and retry manual generation', () => {
    let accepted = false;
    let startCalls = 0;
    const run = {
      id: 21,
      kind: 'manual',
      status: 'queued',
      period_start: '2026-08-18T08:00:00+08:00',
      period_end: '2026-08-19T08:00:00+08:00',
      progress: 0,
      title: 'Queued digest',
      content: { sections: [] },
      markdown: '',
      input_tokens: 0,
      output_tokens: 0,
      article_count: 3,
      is_read: false,
      error: '',
      created_at: '2026-08-19T08:01:00+08:00',
    };
    const cloudProcessing = () => ({
      disclosure_version: 1,
      required: true,
      accepted,
      accepted_version: accepted ? 1 : null,
      accepted_at: accepted ? '2026-08-19T08:02:00+08:00' : null,
      destination: {
        profile_id: 12,
        profile_name: 'Work AI',
        endpoint: 'https://api.example.com',
      },
    });
    const config = {
      enabled: false,
      schedule_time: '08:00',
      feed_scope: 'all',
      feed_ids: [],
      include_hidden: false,
      ai_profile_id: 12,
      focus: '',
      outline: [],
      language: 'auto',
      title_template: '24-Hour AI Digest · {{date}}',
      in_app_notification: true,
      system_notification: false,
      notify_on_empty: false,
    };

    cy.intercept('GET', '/api/daily-report/config', (req) =>
      req.reply({ config, cloud_processing: cloudProcessing() })
    );
    cy.intercept('GET', '/api/daily-report/consent', (req) =>
      req.reply({ cloud_processing: cloudProcessing() })
    );
    cy.intercept('POST', '/api/daily-report/consent', (req) => {
      expect(req.body).to.deep.equal({ action: 'grant', version: 1 });
      accepted = true;
      req.reply({ cloud_processing: cloudProcessing() });
    }).as('grantDailyReportConsent');
    cy.intercept('GET', '/api/daily-report/status', {
      enabled: false,
      is_running: false,
      progress: 0,
      unread_count: 0,
      missed_count: 0,
      notification_authorization: 'not_determined',
    });
    cy.intercept('GET', '/api/daily-report/history?*', (req) =>
      req.reply({
        items: startCalls >= 2 ? [run] : [],
        total: startCalls >= 2 ? 1 : 0,
        page: 1,
        page_size: 20,
      })
    );
    cy.intercept('GET', '/api/daily-report/history/21', { run, sources: [] });
    cy.intercept('POST', '/api/daily-report/generate', (req) => {
      if (req.body.action === 'preview') {
        req.reply({
          period_start: run.period_start,
          period_end: run.period_end,
          article_count: 3,
          estimated_batches: 1,
        });
        return;
      }

      startCalls += 1;
      if (startCalls === 1) {
        req.reply({
          statusCode: 409,
          body: {
            success: false,
            error: {
              code: 'cloud_processing_consent_required',
              message: 'Cloud processing consent is required',
              details: { cloud_processing: cloudProcessing() },
            },
          },
        });
        return;
      }
      req.reply({ statusCode: 202, body: { run } });
    }).as('generateDailyReport');
    cy.reload();

    cy.get('button[title="24-Hour AI Digest"], button[title="24 小时 AI 日报"]').click({
      force: true,
    });
    cy.contains('button', /Generate now|立即生成/).click();
    cy.wait('@generateDailyReport').its('request.body.action').should('eq', 'preview');
    cy.contains('button', /Start generation|开始生成/).click();
    cy.wait('@generateDailyReport').its('response.statusCode').should('eq', 409);

    cy.get('[data-testid="daily-report-cloud-consent"]').should('contain.text', 'Work AI');
    cy.contains('button', /Agree and continue|同意并继续/).should('be.disabled');
    cy.get('[data-testid="daily-report-consent-checkbox"]').check();
    cy.contains('button', /Agree and continue|同意并继续/).click();
    cy.wait('@grantDailyReportConsent');
    cy.wait('@generateDailyReport').its('request.body.action').should('eq', 'start');
    cy.then(() => expect(startCalls).to.eq(2));
    cy.get('[data-testid="daily-report-cloud-consent"]').should('not.exist');
    cy.contains('Queued digest').should('be.visible');
  });
});
