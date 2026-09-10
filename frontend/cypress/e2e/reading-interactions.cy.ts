/// <reference types="cypress" />

const image = 'data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///ywAAAAAAQABAAACAUwAOw==';
const article = {
  id: 1,
  feed_id: 1,
  feed_title: 'Reading Feed',
  title: 'English title',
  url: 'https://example.com/article',
  published_at: '2026-09-01T00:00:00Z',
  translated_title: '',
  is_read: false,
  is_favorite: false,
  is_hidden: false,
  is_read_later: false,
  image_url: image,
};

function setup(
  overrides: Record<string, string> = {},
  feedMode = 'global',
  empty = false,
  savedState: Record<string, string> = {}
) {
  const settings: Record<string, string> = {
    language: 'en-US',
    theme: 'light',
    layout_mode: 'normal',
    default_view_mode: 'rendered',
    translation_mode: 'manual',
    translation_provider: 'ai',
    translation_only_mode: 'false',
    target_language: 'zh-CN',
    summary_enabled: 'false',
    full_text_fetch_enabled: 'false',
    update_check_enabled: 'false',
    image_gallery_enabled: 'true',
    shortcuts_enabled: 'true',
    ai_chat_save_history: 'true',
    ...overrides,
  };
  cy.intercept('/api/**', { statusCode: 200, body: {} });
  cy.intercept('GET', '/api/settings', (req) => req.reply(settings));
  cy.intercept('POST', '/api/settings', (req) => {
    Object.entries(req.body).forEach(([key, value]) => {
      settings[key] = String(value);
    });
    req.reply({ success: true });
  });
  cy.intercept('GET', '/api/feeds', [
    {
      id: 1,
      title: article.feed_title,
      url: 'https://example.com/feed',
      category: '',
      article_view_mode: feedMode,
    },
  ]).as('feeds');
  cy.intercept('GET', '/api/tags', []);
  cy.intercept('GET', '/api/saved-filters', []);
  cy.intercept({ method: 'GET', pathname: '/api/articles' }, empty ? [] : [article]).as('articles');
  cy.intercept('GET', '/api/articles/images*', empty ? [] : [article]).as('images');
  cy.intercept('GET', '/api/articles/extract-images*', { images: [image] });
  cy.intercept('GET', '/api/articles/unread-counts', {});
  cy.intercept('GET', '/api/articles/filter-counts', {});
  cy.intercept('GET', '/api/progress', { is_running: false });
  cy.intercept('GET', '/api/articles/content*', {
    content:
      '<p>First paragraph with enough English words to translate.</p><p>Second paragraph stays unchanged.</p>',
    cached: true,
  }).as('content');
  cy.intercept('POST', '/api/browser/open', { statusCode: 200, body: {} }).as('openBrowser');
  cy.visit('/', {
    onBeforeLoad(win) {
      Object.entries(savedState).forEach(([key, value]) => win.localStorage.setItem(key, value));
    },
  });
  cy.wait(['@feeds', '@articles']);
  return settings;
}

function openArticle() {
  cy.get('[data-article-id="1"]').click();
  cy.wait('@content');
  cy.get('.prose-content p').should('have.length', 2);
}

describe('Reading interactions', () => {
  it('keeps Stop reachable in a small chat with a long title and cancels the active request', () => {
    const requests: Array<Record<string, unknown>> = [];
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', [
      { id: 55, article_id: 1, title: 'Long conversation title '.repeat(60), message_count: 1 },
    ]);
    cy.intercept('GET', '/api/ai/chat/messages*', [
      { id: 1, role: 'user', content: 'Earlier cancelled question', created_at: '' },
    ]).as('stoppedMessages');
    cy.intercept('POST', '/api/ai-chat', (req) => {
      requests.push(req.body);
      expect(req.body.session_id).to.equal(55);
      expect(req.body.is_first_message).to.equal(true);
      expect(req.body.article_content).to.contain('First paragraph');
      req.reply({ delay: 1200, body: { response: 'Late cancelled answer', session_id: 55 } });
    }).as('cancelledChat');
    cy.intercept('POST', '/api/ai-chat/cancel', (req) => {
      expect(req.body.session_id).to.equal(55);
      expect(req.body.request_id).to.equal(requests[0].request_id);
      req.reply({ success: true });
    }).as('cancelChat');
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.wait('@stoppedMessages');
    cy.get('input[placeholder="Type a message..."]').type('Retry with article context{enter}');
    cy.wrap(requests).should('have.length', 1);
    cy.get('.chat-panel').invoke('css', 'height', '174px');
    cy.get('[data-testid="chat-stop-generation"]').then(($button) => {
      const panel = $button[0].closest('.chat-panel')!.getBoundingClientRect();
      const button = $button[0].getBoundingClientRect();
      expect(button.top).to.be.at.least(panel.top);
      expect(button.bottom).to.be.at.most(panel.bottom);
      expect(button.right).to.be.at.most(panel.right);
    });
    cy.get('[data-testid="chat-close"]').then(($button) => {
      expect($button[0].getBoundingClientRect().right).to.be.at.most(
        $button[0].closest('.chat-panel')!.getBoundingClientRect().right
      );
    });
    cy.get('[data-testid="chat-stop-generation"]').click();
    cy.wait('@cancelChat');
    cy.wait('@cancelledChat');
    cy.get('[data-testid="chat-stop-generation"]').should('not.exist');
    cy.get('input[placeholder="Type a message..."]').should('be.enabled');
    cy.contains('.chat-panel', 'Late cancelled answer').should('not.exist');
  });

  it('keeps the bound article until explicitly starting a new chat for another reader article', () => {
    let sends = 0;
    let creates = 0;
    const firstArticle = {
      ...article,
      title: 'LongUnbrokenArticleTitle'.repeat(80),
      feed_title: 'LongUnbrokenFeedSource'.repeat(80),
    };
    const secondArticle = {
      ...article,
      id: 2,
      title: 'Second article',
      feed_title: 'Second source',
      url: 'https://example.com/second',
    };
    const sessions = [{ id: 11, article_id: 1, title: 'First article session', message_count: 1 }];
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept({ method: 'GET', pathname: '/api/articles' }, [firstArticle, secondArticle]).as(
      'contextArticles'
    );
    cy.intercept('GET', '/api/articles/content*', (req) => {
      req.reply({
        content:
          Number(req.query.id) === 2
            ? '<p>Second article body.</p><p>Second context.</p>'
            : '<p>First article body.</p><p>First context.</p>',
        cached: true,
      });
    }).as('contextContent');
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', (req) => {
      req.reply(sessions.filter((session) => session.article_id === Number(req.query.article_id)));
    });
    cy.intercept('GET', '/api/ai/chat/messages*', (req) => {
      req.reply(
        Number(req.query.session_id) === 11
          ? [{ id: 1, role: 'user', content: 'Question about first article', created_at: '' }]
          : { statusCode: 500, body: {} }
      );
    }).as('contextMessages');
    cy.intercept('POST', '/api/ai/chat/session/create', (req) => {
      creates++;
      expect(req.body.article_id).to.equal(2);
      const session = { id: 22, article_id: 2, title: req.body.title, message_count: 0 };
      sessions.push(session);
      req.reply({ delay: 300, body: session });
    }).as('newContext');
    cy.intercept('POST', '/api/ai-chat', (req) => {
      sends++;
      expect(req.body.session_id).to.equal(22);
      expect(req.body.article_id).to.equal(2);
      expect(req.body.article_title).to.equal(secondArticle.title);
      expect(req.body.article_url).to.equal(secondArticle.url);
      expect(req.body.article_content).to.contain('Second article body');
      expect(req.body.article_content).not.to.contain('First article body');
      expect(req.body.messages).to.have.length(1);
      req.reply({ response: 'Answer for second article', session_id: 22 });
    }).as('contextChat');
    cy.reload();
    cy.wait('@contextArticles');
    cy.get('[data-article-id="1"]').click();
    cy.wait('@contextContent');
    cy.get('button[title="AI Chat"]').click();
    cy.wait('@contextMessages');
    cy.get('[data-testid="chat-context-article"]')
      .should('contain', firstArticle.title)
      .and('contain', firstArticle.feed_title);
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.get('[data-testid="chat-context-article"]').should('be.visible');
    cy.get('[data-session-id="11"]').should('be.visible').click();
    cy.wait('@contextMessages');
    cy.get('input[placeholder="Type a message..."]').type('Draft for first article');
    cy.get('[data-article-id="2"]').click();
    cy.wait('@contextContent');
    cy.get('[data-testid="chat-context-article"]').should(
      'have.attr',
      'data-context-article-id',
      '1'
    );
    cy.contains('.chat-panel', 'Question about first article').should('be.visible');
    cy.get('input[placeholder="Type a message..."]')
      .should('be.disabled')
      .trigger('keydown', { key: 'Enter', force: true });
    cy.then(() => expect(sends).to.equal(0));
    cy.get('.chat-panel').invoke('css', 'width', '420px');
    cy.get('.chat-panel').should(($panel) => {
      const panel = $panel[0];
      const panelRect = panel.getBoundingClientRect();
      const card = panel.querySelector('[data-testid="chat-context-article"]')!;
      const input = panel.querySelector('input[placeholder="Type a message..."]')!;
      const inputRow = input.parentElement!.parentElement!;
      expect(panelRect.width).to.equal(420);
      for (const element of [card, inputRow, input]) {
        const rect = element.getBoundingClientRect();
        expect(rect.width).to.be.greaterThan(0);
        expect(rect.left).to.be.at.least(panelRect.left);
        expect(rect.right).to.be.at.most(panelRect.right);
      }
    });
    for (const height of [200, 174]) {
      cy.get('.chat-panel').invoke('css', 'height', `${height}px`);
      cy.get('input[placeholder="Type a message..."]').then(($input) => {
        const panelRect = $input[0].closest('.chat-panel')!.getBoundingClientRect();
        const inputRect = $input[0].getBoundingClientRect();
        expect(inputRect.top).to.be.at.least(panelRect.top);
        expect(inputRect.bottom).to.be.at.most(panelRect.bottom);
      });
    }
    cy.get('.chat-panel').invoke('css', 'height', '600px');
    cy.get('[data-article-id="1"]').click();
    cy.wait('@contextContent');
    cy.get('input[placeholder="Type a message..."]')
      .should('be.enabled')
      .and('have.value', 'Draft for first article');
    cy.get('[data-article-id="2"]').click();
    cy.wait('@contextContent');
    cy.get('[data-testid="chat-new-context"]').click();
    cy.then(() => expect(creates).to.equal(0));
    cy.get('[data-testid="chat-context-article"]')
      .should('have.attr', 'data-context-article-id', '2')
      .and('contain', secondArticle.title)
      .and('contain', secondArticle.feed_title);
    cy.get('input[placeholder="Type a message..."]')
      .should('be.enabled')
      .and('have.value', '')
      .type('Question about second article{enter}');
    cy.wait('@newContext');
    cy.wait('@contextChat');
    cy.contains('.chat-panel', 'Answer for second article').should('be.visible');
  });

  it('explicitly rebinds an existing chat to the current article while retaining its history', () => {
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    const second = {
      ...article,
      id: 2,
      title: 'Second article',
      feed_title: 'Second source',
      url: 'https://example.com/second',
    };
    const session = { id: 11, article_id: 1, title: 'Existing conversation', message_count: 2 };
    cy.intercept({ method: 'GET', pathname: '/api/articles' }, [article, second]).as('rebindArticles');
    cy.intercept('GET', '/api/articles/content*', (req) => {
      req.reply({
        content: Number(req.query.id) === 2 ? '<p>Second article body.</p>' : '<p>First article body.</p>',
        cached: true,
      });
    }).as('rebindContent');
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', (req) => {
      req.reply(session.article_id === Number(req.query.article_id) ? [session] : []);
    });
    cy.intercept('GET', '/api/ai/chat/messages*', [
      { id: 1, role: 'user', content: 'Earlier question', created_at: '' },
      { id: 2, role: 'assistant', content: 'Earlier answer', created_at: '' },
    ]).as('rebindMessages');
    cy.intercept('POST', '/api/ai/chat/session/create', () => {
      throw new Error('Continuing an existing conversation must not create a session');
    });
    let sends = 0;
    cy.intercept('POST', '/api/ai-chat', (req) => {
      sends++;
      expect(req.body.session_id).to.equal(11);
      expect(req.body.article_id).to.equal(2);
      expect(req.body.article_title).to.equal(second.title);
      expect(req.body.article_url).to.equal(second.url);
      expect(req.body.article_content).to.contain('Second article body');
      expect(req.body.article_content).not.to.contain('First article body');
      expect(req.body.messages[0].content).to.equal('Earlier question');
      expect(req.body.messages[1].content).to.equal('Earlier answer');
      expect(req.body.rebind_session).to.equal(sends === 1);
      expect(req.body.is_first_message).to.equal(sends === 1);
      session.article_id = 2;
      req.reply({ response: `Answer ${sends}`, session_id: 11 });
    }).as('rebindChat');
    cy.reload();
    cy.wait('@rebindArticles');
    cy.get('[data-article-id="1"]').click();
    cy.wait('@rebindContent');
    cy.get('button[title="AI Chat"]').click();
    cy.wait('@rebindMessages');
    cy.get('[data-article-id="2"]').click();
    cy.wait('@rebindContent');
    cy.get('input[placeholder="Type a message..."]').should('be.disabled');
    cy.get('[data-testid="chat-context-article"]').should('have.attr', 'data-context-article-id', '1');
    cy.get('[data-testid="chat-continue-current-article"]').click();
    cy.get('[data-testid="chat-context-article"]')
      .should('have.attr', 'data-context-article-id', '2')
      .and('contain', second.title)
      .and('contain', second.feed_title);
    cy.contains('.chat-panel', 'Earlier answer').should('be.visible');
    cy.get('input[placeholder="Type a message..."]').type('Compare this article{enter}');
    cy.wait('@rebindChat');
    cy.contains('.chat-panel', 'Answer 1').should('be.visible');
    cy.get('input[placeholder="Type a message..."]').type('Follow up{enter}');
    cy.wait('@rebindChat');
    cy.contains('.chat-panel', 'Answer 2').should('be.visible');
  });

  it('keeps temporary chats out of history and aborts Stop and Close without accepting late replies', () => {
    setup({ ai_chat_enabled: 'true', ai_chat_save_history: 'false', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('/api/ai/chat/**', () => {
      throw new Error('Temporary chat must not access persisted sessions or messages');
    });
    cy.intercept('POST', '/api/ai-chat/cancel', () => {
      throw new Error('Temporary chat uses HTTP abort, not a persisted request ID');
    });
    const requests: Array<Record<string, unknown>> = [];
    const aborted: string[] = [];
    cy.intercept('POST', '/api/ai-chat', (req) => {
      expect(req.body).not.to.have.property('session_id');
      expect(req.body).not.to.have.property('request_id');
      requests.push(req.body);
      const prompt = req.body.messages.at(-1).content;
      req.reply({ delay: prompt === 'Second question' ? 0 : 1000, body: { response: `Reply to ${prompt}` } });
    }).as('temporaryChat');
    openArticle();
    cy.window().then((win) => {
      const originalFetch = win.fetch.bind(win);
      cy.stub(win, 'fetch').callsFake((input: RequestInfo | URL, init?: RequestInit) => {
        if (input === '/api/ai-chat') {
          const prompt = JSON.parse(String(init?.body)).messages.at(-1).content;
          init?.signal?.addEventListener('abort', () => aborted.push(prompt), { once: true });
        }
        return originalFetch(input, init);
      });
    });
    cy.get('button[title="AI Chat"]').click();
    cy.get('[data-testid="chat-session-switcher"]').should('not.exist');
    cy.get('input[placeholder="Type a message..."]').type('First question{enter}');
    cy.wrap(requests).should('have.length', 1);
    cy.get('[data-testid="chat-stop-generation"]').click();
    cy.wrap(aborted).should('deep.equal', ['First question']);
    cy.contains('.chat-panel', 'First question').should('be.visible');
    cy.get('input[placeholder="Type a message..."]').type('Second question{enter}');
    cy.contains('.chat-panel', 'Reply to Second question').should('be.visible');
    cy.wait('@temporaryChat');
    cy.contains('.chat-panel', 'Reply to First question').should('not.exist');
    cy.get('input[placeholder="Type a message..."]').type('Third question{enter}');
    cy.wrap(requests).should('have.length', 3);
    cy.get('[data-testid="chat-close"]').click();
    cy.wrap(aborted).should('deep.equal', ['First question', 'Third question']);
    cy.get('button[title="AI Chat"]').click();
    cy.get('input[placeholder="Type a message..."]').should('have.value', '');
    cy.contains('.chat-panel', 'Third question').should('not.exist');
    cy.wait('@temporaryChat');
    cy.wait('@temporaryChat');
    cy.contains('.chat-panel', 'Reply to Third question').should('not.exist');
  });

  it('keeps a persistent request mode fixed when history settings change during session creation', () => {
    const settings = setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', []);
    let creates = 0;
    const requests: Array<Record<string, unknown>> = [];
    cy.intercept('POST', '/api/ai/chat/session/create', (req) => {
      creates++;
      req.reply({ delay: 1200, body: { id: 91, article_id: 1, title: req.body.title, message_count: 0 } });
    }).as('snapshotCreate');
    cy.intercept('POST', '/api/ai-chat', (req) => {
      expect(req.body.session_id).to.equal(91);
      expect(req.body.request_id).to.be.a('string').and.not.be.empty;
      expect(req.body.article_content).to.contain('First paragraph');
      requests.push(req.body);
      req.reply({ delay: 1200, body: { response: 'Late persistent answer', session_id: 91 } });
    }).as('snapshotChat');
    cy.intercept('POST', '/api/ai-chat/cancel', (req) => {
      expect(req.body.session_id).to.equal(91);
      expect(req.body.request_id).to.equal(requests[0].request_id);
      req.reply({ success: true });
    }).as('snapshotCancel');
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.get('[data-testid="chat-session-switcher"]').should('be.visible');
    cy.get('input[placeholder="Type a message..."]').type('Keep this request persistent{enter}');
    cy.wrap(null).should(() => expect(creates).to.equal(1));
    cy.window().then((win) => {
      settings.ai_chat_save_history = 'false';
      win.dispatchEvent(new CustomEvent('settings-updated'));
    });
    cy.get('[data-testid="chat-session-switcher"]').should('not.exist');
    cy.wait('@snapshotCreate');
    cy.wrap(requests).should('have.length', 1);
    cy.get('[data-testid="chat-stop-generation"]').click();
    cy.wait('@snapshotCancel');
    cy.wait('@snapshotChat');
    cy.contains('.chat-panel', 'Late persistent answer').should('not.exist');
  });

  it('resends article context when switching between persistent and temporary chat modes', () => {
    const settings = setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    const sessions = [{ id: 51, article_id: 1, title: 'Saved conversation', message_count: 2 }];
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', (req) => req.reply(sessions));
    cy.intercept('GET', '/api/ai/chat/messages*', [
      { id: 1, role: 'user', content: 'Saved question', created_at: '' },
      { id: 2, role: 'assistant', content: 'Saved answer', created_at: '' },
    ]).as('savedModeMessages');
    let creates = 0;
    let sends = 0;
    cy.intercept('POST', '/api/ai/chat/session/create', (req) => {
      creates++;
      expect(sends).to.equal(1);
      expect(req.body.article_id).to.equal(article.id);
      expect(req.body.title).to.equal('Save the next answer');
      const session = { id: 52, article_id: 1, title: req.body.title, message_count: 0 };
      sessions.unshift(session);
      req.reply(session);
    }).as('modeCreate');
    cy.intercept('POST', '/api/ai-chat', (req) => {
      sends++;
      expect(req.body.is_first_message).to.equal(true);
      expect(req.body.article_id).to.equal(article.id);
      expect(req.body.article_title).to.equal(article.title);
      expect(req.body.article_url).to.equal(article.url);
      expect(req.body.article_content).to.contain('First paragraph');
      expect(req.body.messages[0].content).to.equal('Saved question');
      expect(req.body.messages[1].content).to.equal('Saved answer');
      if (sends === 1) {
        expect(creates).to.equal(0);
        expect(req.body).not.to.have.property('session_id');
        expect(req.body).not.to.have.property('request_id');
        req.reply({ response: 'Temporary answer' });
      } else {
        expect(req.body.session_id).to.equal(52);
        expect(req.body.request_id).to.be.a('string').and.not.be.empty;
        expect(req.body.messages[3].content).to.equal('Temporary answer');
        req.reply({ response: 'New saved answer', session_id: 52 });
      }
    }).as('modeChat');
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.wait('@savedModeMessages');
    cy.window().then((win) => {
      settings.ai_chat_save_history = 'false';
      win.dispatchEvent(new CustomEvent('settings-updated'));
    });
    cy.get('[data-testid="chat-session-switcher"]').should('not.exist');
    cy.get('input[placeholder="Type a message..."]').type('Keep this answer temporary{enter}');
    cy.wait('@modeChat');
    cy.contains('.chat-panel', 'Temporary answer').should('be.visible');
    cy.window().then((win) => {
      settings.ai_chat_save_history = 'true';
      win.dispatchEvent(new CustomEvent('settings-updated'));
    });
    cy.get('[data-testid="chat-session-switcher"]').should('be.visible');
    cy.get('input[placeholder="Type a message..."]').type('Save the next answer{enter}');
    cy.wait('@modeCreate');
    cy.wait('@modeChat');
    cy.contains('.chat-panel', 'New saved answer').should('be.visible');
    cy.then(() => {
      expect(creates).to.equal(1);
      expect(sends).to.equal(2);
    });
  });

  for (const action of ['stop', 'close']) {
    it(`does not generate after ${action} while session creation is pending`, () => {
      setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
      cy.intercept('GET', '/api/ai/profiles', []);
      cy.intercept('GET', '/api/ai/chat/sessions*', []);
      let creates = 0;
      let sends = 0;
      cy.intercept('POST', '/api/ai/chat/session/create', (req) => {
        creates++;
        req.reply({ delay: 800, body: { id: 73, article_id: 1, title: req.body.title, message_count: 0 } });
      }).as('pendingCreate');
      cy.intercept('POST', '/api/ai-chat', (req) => {
        sends++;
        expect(req.body.session_id).to.equal(73);
        expect(req.body.messages).to.have.length(1);
        expect(req.body.messages[0].content).to.equal('Question before creation finishes');
        req.reply({ response: 'Answer after retry', session_id: 73 });
      }).as('retryCreateChat');
      openArticle();
      cy.get('button[title="AI Chat"]').click();
      cy.get('input[placeholder="Type a message..."]').type('Question before creation finishes{enter}');
      cy.wrap(null).should(() => expect(creates).to.equal(1));
      cy.get(`[data-testid="${action === 'stop' ? 'chat-stop-generation' : 'chat-close'}"]`).click();
      cy.wait('@pendingCreate');
      cy.then(() => expect(sends).to.equal(0));
      if (action === 'stop') {
        cy.get('input[placeholder="Type a message..."]')
          .should('have.value', 'Question before creation finishes')
          .type('{enter}');
        cy.wait('@retryCreateChat');
        cy.contains('.chat-panel', 'Answer after retry').should('be.visible');
        cy.then(() => {
          expect(creates).to.equal(1);
          expect(sends).to.equal(1);
        });
      } else {
        cy.get('.chat-panel').should('not.exist');
        cy.get('button[title="AI Chat"]').click();
        cy.get('input[placeholder="Type a message..."]').should('have.value', '');
        cy.contains('.chat-panel', 'Question before creation finishes').should('not.exist');
      }
    });
  }

  it('keeps new chats local until sending and creates only one session for the first question', () => {
    let creates = 0;
    let sends = 0;
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', []);
    cy.intercept('POST', '/api/ai/chat/session/create', (req) => {
      creates++;
      expect(req.body.article_id).to.equal(1);
      expect(req.body.title).to.equal('First real question');
      req.reply({ id: 10, article_id: 1, title: req.body.title, message_count: 0 });
    }).as('createChat');
    cy.intercept('POST', '/api/ai-chat', (req) => {
      sends++;
      expect(creates).to.equal(1);
      expect(req.body.session_id).to.equal(10);
      expect(req.body.messages.at(-1).content).to.equal('First real question');
      req.reply({ response: 'First answer', session_id: 10 });
    }).as('sendChat');
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.get('[data-testid="chat-new-session"]').click().click();
    cy.get('input[placeholder="Type a message..."]').type('Discard this draft');
    cy.get('[data-testid="chat-new-session"]').click();
    cy.get('input[placeholder="Type a message..."]').should('have.value', '');
    cy.then(() => expect(creates).to.equal(0));
    cy.get('input[placeholder="Type a message..."]').type('First real question{enter}');
    cy.wait('@createChat');
    cy.wait('@sendChat');
    cy.contains('.chat-panel', 'First answer').should('be.visible');
    cy.get('[data-testid="chat-new-session"]').click().click();
    cy.then(() => {
      expect(creates).to.equal(1);
      expect(sends).to.equal(1);
    });
  });

  it('disables composing only while the history list is open and allows continuing a selected session', () => {
    let sends = 0;
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', [
      { id: 1, article_id: 1, title: 'Saved session', message_count: 1 },
    ]);
    cy.intercept('GET', '/api/ai/chat/messages*', [
      { id: 1, role: 'user', content: 'Previous question', created_at: '' },
    ]).as('chatMessages');
    cy.intercept('POST', '/api/ai-chat', (req) => {
      sends++;
      expect(req.body.session_id).to.equal(1);
      expect(req.body.messages.at(-1).content).to.equal('Continue this conversation');
      req.reply({ response: 'Continued answer', session_id: 1 });
    }).as('continueChat');
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.wait('@chatMessages');
    cy.get('input[placeholder="Type a message..."]').type('Continue this conversation');
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.get('input[placeholder="Type a message..."]')
      .should('be.disabled')
      .trigger('keydown', { key: 'Enter', force: true });
    cy.get('input[placeholder="Type a message..."]').parent().find('button').should('be.disabled');
    cy.then(() => expect(sends).to.equal(0));
    cy.get('[data-session-id="1"]').click();
    cy.wait('@chatMessages');
    cy.get('input[placeholder="Type a message..."]')
      .should('be.enabled')
      .should('have.value', 'Continue this conversation')
      .type('{enter}');
    cy.wait('@continueChat');
    cy.contains('.chat-panel', 'Continued answer').should('be.visible');
    cy.then(() => expect(sends).to.equal(1));
  });

  it('keeps a failed title edit available for retry and shows the failure', () => {
    let saves = 0;
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', [
      { id: 1, article_id: 1, title: 'Original title', message_count: 0 },
    ]);
    cy.intercept('GET', '/api/ai/chat/messages*', []).as('chatMessages');
    cy.intercept('PUT', '/api/ai/chat/session*', (req) => {
      expect(req.body.title).to.equal('Title to retry');
      saves++;
      req.reply(saves === 1 ? { statusCode: 500, body: {} } : { success: true });
    }).as('saveTitle');
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.wait('@chatMessages');
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.get('[data-session-id="1"] button').first().click();
    cy.get('[data-session-id="1"] input').clear().type('Title to retry');
    cy.get('[data-session-id="1"] button').first().click();
    cy.wait('@saveTitle');
    cy.contains('Failed to save conversation title. Please try again.').should('be.visible');
    cy.get('[data-session-id="1"] input').should('be.visible').and('have.value', 'Title to retry');
    cy.get('[data-testid="chat-session-switcher"]').should('contain', 'Original title');
    cy.get('[data-session-id="1"] button').first().click();
    cy.wait('@saveTitle');
    cy.get('[data-session-id="1"] input').should('not.exist');
    cy.get('[data-session-id="1"]').should('contain', 'Title to retry');
  });

  it('discards unsaved title edits when leaving the session list or selecting another session', () => {
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', [
      { id: 1, article_id: 1, title: 'First session', message_count: 0 },
      { id: 2, article_id: 1, title: 'Second session', message_count: 0 },
    ]);
    cy.intercept('GET', '/api/ai/chat/messages*', []).as('chatMessages');
    cy.intercept('PUT', '/api/ai/chat/session*', () => {
      throw new Error('Leaving an edit must not save the draft');
    });
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.wait('@chatMessages');
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.get('[data-session-id="1"] button').first().click();
    cy.get('[data-session-id="1"] input').clear().type('Unsaved title');
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.get('[data-session-id="1"] input').should('not.exist');
    cy.get('[data-session-id="1"]').should('contain', 'First session');
    cy.get('[data-session-id="1"] button').first().click();
    cy.get('[data-session-id="1"] input').clear().type('Another unsaved title');
    cy.get('[data-session-id="2"]').click();
    cy.wait('@chatMessages');
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.get('[data-session-id="1"] input').should('not.exist');
    cy.get('[data-session-id="1"]').should('contain', 'First session');
  });

  for (const [language, unreadTitle, galleryTitle, unreadToggle, emptyText, completedText] of [
    [
      'en-US',
      'Unread',
      'Multimedia Gallery',
      'Show only unread articles',
      'No articles found.',
      "You're all caught up",
    ],
    ['zh-CN', '未读', '多媒体模式', '仅显示未读文章', '未找到文章', '已读完全部文章'],
  ]) {
    it(`centers unread completion and distinguishes ordinary empty galleries in ${language}`, () => {
      setup({ language, translation_mode: 'off' }, 'global', true);
      cy.get('[data-testid="article-list-empty"]').should('contain', emptyText);
      cy.get(`.smart-activity-bar button[title^="${unreadTitle}"]`).click();
      cy.get('[data-testid="article-list-empty"]')
        .should('contain', completedText)
        .then(($empty) => {
          const element = $empty[0];
          const viewport = element.parentElement!.getBoundingClientRect();
          const first = element.firstElementChild!.getBoundingClientRect();
          const last = element.lastElementChild!.getBoundingClientRect();
          expect(
            Math.abs((first.top + last.bottom) / 2 - (viewport.top + viewport.bottom) / 2)
          ).to.be.lessThan(3);
        });
      cy.get(`.smart-activity-bar button[title="${galleryTitle}"]`).click();
      cy.wait('@images');
      cy.get('[data-testid="gallery-empty"]')
        .should('contain', emptyText)
        .and('not.contain', completedText);
      cy.get(`button[title="${unreadToggle}"]`).click();
      cy.wait('@images').its('request.url').should('contain', 'only_unread=true');
      cy.get('[data-testid="gallery-empty"]')
        .should('contain', completedText)
        .then(($empty) => {
          const element = $empty[0];
          const viewport = element.parentElement!.getBoundingClientRect();
          const first = element.firstElementChild!.getBoundingClientRect();
          const last = element.lastElementChild!.getBoundingClientRect();
          expect(
            Math.abs((first.top + last.bottom) / 2 - (viewport.top + viewport.bottom) / 2)
          ).to.be.lessThan(3);
        });
    });
  }

  it('prevents selecting model labels while keeping model changes and message selection available', () => {
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', [
      { id: 1, name: 'Model One', is_default: true },
      { id: 2, name: 'Model Two', is_default: false },
    ]);
    cy.intercept('GET', '/api/ai/chat/sessions*', []);
    cy.intercept('POST', '/api/ai/chat/session/create', {
      id: 1,
      article_id: 1,
      title: 'Question',
      message_count: 0,
    }).as('createModelChat');
    cy.intercept('POST', '/api/ai-chat', (req) => {
      expect(req.body.profile_id).to.equal(2);
      req.reply({ response: 'Selectable answer', session_id: 1 });
    }).as('modelChat');
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.get('.chat-profile-selector .select-trigger .select-text').should(
      'have.css',
      'user-select',
      'none'
    );
    cy.get('.chat-profile-selector .select-trigger')
      .should('have.css', 'user-select', 'none')
      .click();
    cy.contains('.chat-profile-selector .select-option', 'Model Two')
      .should('have.css', 'user-select', 'none')
      .click();
    cy.get('.chat-profile-selector .select-trigger').should('contain', 'Model Two');
    cy.get('input[placeholder="Type a message..."]').type('Question{enter}');
    cy.wait('@createModelChat').its('request.body.article_id').should('equal', article.id);
    cy.wait('@modelChat');
    cy.contains('.chat-panel .select-text', 'Selectable answer')
      .should('be.visible')
      .and('have.css', 'user-select', 'text');
  });

  it('keeps title editing above message actions without selecting the session on save', () => {
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', [
      { id: 1, article_id: 1, title: 'Saved session', message_count: 1 },
    ]);
    cy.intercept('GET', '/api/ai/chat/messages*', [
      { id: 1, role: 'user', content: 'Previous question', created_at: '' },
    ]).as('chatMessages');
    cy.intercept('PUT', '/api/ai/chat/session*', (req) => {
      expect(req.body.title).to.equal('Renamed session');
      req.reply({ success: true });
    }).as('renameChat');
    openArticle();
    cy.get('button[title="AI Chat"]').click();
    cy.wait('@chatMessages');
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.get('[data-session-id="1"] button').first().click();
    cy.get('[data-session-id="1"] input').clear().type('Renamed session');
    cy.get('.chat-panel button[title="Copy message"]').should('not.be.visible');
    cy.get('[data-session-id="1"] button').first().click();
    cy.wait('@renameChat');
    cy.get('[data-session-id="1"]').should('be.visible').and('contain', 'Renamed session');
    cy.get('@chatMessages.all').should('have.length', 1);
    cy.get('[data-testid="chat-session-switcher"]').click();
    cy.contains('.chat-panel', 'Previous question').should('be.visible');
  });

  it('translates only the requested title or paragraph in manual mode, and retries failures', () => {
    let calls = 0;
    let paragraphCalls = 0;
    setup();
    cy.intercept('POST', '/api/articles/translate', (req) => {
      calls++;
      expect(req.body.article_id).to.equal(article.id);
      expect(req.body.title).to.equal(article.title);
      req.reply({ translated_title: '翻译后的标题', skipped: false });
    }).as('titleTranslation');
    cy.intercept('POST', '/api/articles/translate-text', (req) => {
      calls++;
      paragraphCalls++;
      expect(req.body.text).to.contain('First paragraph');
      expect(req.body.text).not.to.contain('Second paragraph');
      req.alias = 'paragraphTranslation';
      req.reply(
        paragraphCalls === 1
          ? { statusCode: 500, body: { error: 'temporary failure' } }
          : { translated_text: '第一段已翻译', skipped: false }
      );
    });
    openArticle();
    cy.contains('Translate on demand')
      .should('be.visible')
      .then(() => expect(calls).to.equal(0));
    cy.get('button[title="Translate title"]').click();
    cy.wait('@titleTranslation');
    cy.contains('翻译后的标题').should('be.visible');
    cy.get('.prose-content p').first().click({ ctrlKey: true });
    cy.wait('@paragraphTranslation');
    cy.get('.translation-text').should('not.exist');
    cy.get('.prose-content p').first().click({ ctrlKey: true });
    cy.wait('@paragraphTranslation');
    cy.get('.translation-text').should('have.length', 1).and('contain', '第一段已翻译');
    cy.get('.prose-content p').eq(1).should('contain', 'Second paragraph stays unchanged');
    cy.then(() => expect(calls).to.equal(3));
  });

  it('searches a captured text selection and copies the article link', () => {
    setup();
    openArticle();
    cy.window().then((win) => {
      const paragraph = win.document.querySelector('.prose-content p')!;
      const range = win.document.createRange();
      range.selectNodeContents(paragraph);
      win.getSelection()!.removeAllRanges();
      win.getSelection()!.addRange(range);
    });
    cy.get('.prose-content p').first().trigger('contextmenu');
    cy.contains('Search with Google').should('be.visible');
    cy.contains('Search with Bing').click();
    cy.wait('@openBrowser')
      .its('request.body.url')
      .should(
        'equal',
        'https://www.bing.com/search?q=' +
          encodeURIComponent('First paragraph with enough English words to translate.')
      );
    cy.window().then((win) => {
      cy.stub(win.navigator.clipboard, 'writeText').as('copyText').resolves();
    });
    cy.get('button[aria-label="Copy Link"]').click();
    cy.get('@copyText').should('have.been.calledWith', article.url);
  });

  it('keeps icon hit targets on tooltip buttons and reflects changed shortcuts', () => {
    setup();
    cy.get('button[title^="Settings"]')
      .should('have.attr', 'title', 'Settings (,)')
      .find('svg')
      .then(($icon) => {
        const icon = $icon[0];
        const rect = icon.getBoundingClientRect();
        expect(
          icon.ownerDocument.elementFromPoint(rect.x + rect.width / 2, rect.y + rect.height / 2)
            ?.tagName
        ).to.equal('BUTTON');
      });
    cy.window().then((win) =>
      win.dispatchEvent(
        new CustomEvent('shortcuts-changed', { detail: { shortcuts: { openSettings: 'Ctrl+,' } } })
      )
    );
    cy.get('button[title^="Settings"]').should('have.attr', 'title', 'Settings (Ctrl+,)');
  });

  for (const [feedMode, globalMode] of [
    ['external', 'rendered'],
    ['global', 'external'],
  ]) {
    it(`opens gallery articles externally with ${feedMode} feed / ${globalMode} global preference`, () => {
      setup({ default_view_mode: globalMode }, feedMode);
      cy.get('[title="Multimedia Gallery"]').click();
      cy.wait('@images');
      cy.contains('English title').click();
      cy.wait('@openBrowser').its('request.body.url').should('equal', article.url);
      cy.get('iframe').should('not.exist');
      cy.get('[role="dialog"][aria-modal="true"]').should('not.exist');
    });
  }

  it('allows an explicit rendered feed preference to override global external mode', () => {
    setup({ default_view_mode: 'external' }, 'rendered');
    cy.get('[title="Multimedia Gallery"]').click();
    cy.wait('@images');
    cy.contains('English title').click();
    cy.get('[role="dialog"][aria-modal="true"]').should('be.visible');
    cy.get('@openBrowser.all').should('have.length', 0);
  });
  for (const theme of ['light', 'dark']) {
    it(`shows a visible keyboard focus target on the gallery close button in ${theme} mode`, () => {
      setup({ theme }, 'rendered');
      cy.get('[title="Multimedia Gallery"]').click();
      cy.wait('@images');
      cy.contains('English title').click();
      cy.get('[data-image-viewer="true"]').should('be.visible');
      cy.press(Cypress.Keyboard.Keys.TAB);
      cy.get('[data-image-viewer="true"] button[aria-label="Close"]')
        .focus()
        .should('have.css', 'background-color', 'rgb(255, 255, 255)')
        .and('have.css', 'color', 'rgb(0, 0, 0)')
        .should(($button) => {
          expect(getComputedStyle($button[0]).boxShadow).not.to.equal('none');
        })
        .click();
      cy.get('[data-image-viewer="true"]').should('not.exist');
    });
    for (const activation of ['click', 'Enter', 'Space']) {
      it(`shows manual summary focus and generates once by ${activation} in ${theme} mode`, () => {
        setup({ theme, summary_enabled: 'true', summary_provider: 'ai', summary_trigger_mode: 'manual' });
        let requests = 0;
        cy.intercept('POST', '/api/articles/summarize', (req) => {
          requests++;
          expect(req.body.article_id).to.equal(article.id);
          req.reply({ summary: 'Generated on request.', sentence_count: 1, is_too_short: false });
        }).as('generateSummary');
        openArticle();
        cy.contains('button', /^Generate Summary$/).should('be.visible');
        cy.then(() => expect(requests).to.equal(0));
        cy.press(Cypress.Keyboard.Keys.TAB);
        cy.contains('button', /^Generate Summary$/)
          .focus()
          .should('be.focused')
          .should(($button) => {
            expect(getComputedStyle($button[0]).boxShadow).not.to.equal('none');
          });
        if (activation === 'click') {
          cy.contains('button', /^Generate Summary$/).click();
        } else if (activation === 'Enter') {
          cy.contains('button', /^Generate Summary$/).type('{enter}');
        } else {
          cy.press(Cypress.Keyboard.Keys.SPACE);
        }
        cy.wait('@generateSummary');
        cy.contains('Generated on request.').should('be.visible');
        cy.then(() => expect(requests).to.equal(1));
      });
    }
  }

  it('remembers the measured chat size after dragging, closing, reopening, and reloading', () => {
    cy.viewport(1280, 900);
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' }, 'global', false, {
      FeedListExpanded: 'false',
    });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', []);
    openArticle();
    cy.get('.js-article-chat-button').click();
    cy.get('.chat-panel')
      .should(($panel) => {
        const rect = $panel[0].getBoundingClientRect();
        expect(rect.width).to.equal(500);
        expect(rect.height).to.equal(600);
      })
      .then(($panel) => {
        const rect = $panel[0].getBoundingClientRect();
        cy.get('.chat-panel .cursor-nw-resize').trigger('mousedown', {
          clientX: rect.left + 2,
          clientY: rect.top + 2,
          button: 0,
          force: true,
        });
        cy.document().trigger('mousemove', { clientX: rect.left - 118, clientY: rect.top - 98 });
      });
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(620);
      expect(rect.height).to.equal(700);
    });
    cy.window().then((win) => expect(win.localStorage.getItem('mrrssChatPanelSize')).to.be.null);
    cy.document().trigger('mouseup');
    cy.window().then((win) => {
      expect(JSON.parse(win.localStorage.getItem('mrrssChatPanelSize')!)).to.deep.equal({
        width: 620,
        height: 700,
      });
    });
    cy.get('.chat-panel button[title="Close"]').click();
    cy.get('.chat-panel').should('not.exist');
    cy.get('.js-article-chat-button').click();
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(620);
      expect(rect.height).to.equal(700);
    });
    cy.reload();
    openArticle();
    cy.get('.js-article-chat-button').click();
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(620);
      expect(rect.height).to.equal(700);
    });
  });

  it('temporarily fits a narrow viewport without replacing the preferred chat size', () => {
    cy.viewport(1280, 900);
    const preferred = { width: 680, height: 720 };
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' }, 'global', false, {
      FeedListExpanded: 'false',
      mrrssChatPanelSize: JSON.stringify(preferred),
    });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', []);
    openArticle();
    cy.get('.js-article-chat-button').click();
    cy.viewport(390, 500);
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(358);
      expect(rect.height).to.equal(444);
      expect(rect.left).to.be.at.least(0);
      expect(rect.top).to.be.at.least(0);
      expect(rect.right).to.be.at.most(390);
      expect(rect.bottom).to.be.at.most(500);
    });
    cy.get('.chat-panel button[title="Close"]').click();
    cy.get('.chat-panel').should('not.exist');
    cy.get('.js-article-chat-button').click();
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(358);
      expect(rect.height).to.equal(444);
    });
    cy.window().then((win) => {
      expect(JSON.parse(win.localStorage.getItem('mrrssChatPanelSize')!)).to.deep.equal(preferred);
    });
    cy.viewport(1280, 900);
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(preferred.width);
      expect(rect.height).to.equal(preferred.height);
    });
    cy.window().then((win) => {
      expect(JSON.parse(win.localStorage.getItem('mrrssChatPanelSize')!)).to.deep.equal(preferred);
    });
  });

  it('uses a 500 by 600 chat default and a viewport-limited 420 by 200 drag minimum', () => {
    cy.viewport(1280, 900);
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    cy.intercept('GET', '/api/ai/profiles', []);
    cy.intercept('GET', '/api/ai/chat/sessions*', []);
    openArticle();
    cy.get('.js-article-chat-button').click();
    cy.get('.chat-panel')
      .should(($panel) => {
        const rect = $panel[0].getBoundingClientRect();
        expect(rect.width).to.equal(500);
        expect(rect.height).to.equal(600);
      })
      .then(($panel) => {
        const rect = $panel[0].getBoundingClientRect();
        cy.get('.chat-panel .cursor-nw-resize').trigger('mousedown', {
          clientX: rect.left + 2,
          clientY: rect.top + 2,
          button: 0,
          force: true,
        });
        cy.document().trigger('mousemove', { clientX: rect.right, clientY: rect.bottom });
        cy.document().trigger('mouseup');
      });
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(420);
      expect(rect.height).to.equal(200);
    });
    cy.viewport(320, 230);
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(288);
      expect(rect.height).to.equal(174);
      expect(rect.left).to.be.at.least(0);
      expect(rect.top).to.be.at.least(0);
      expect(rect.right).to.be.at.most(320);
      expect(rect.bottom).to.be.at.most(230);
    });
    cy.viewport(1280, 900);
    cy.get('.chat-panel').should(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      expect(rect.width).to.equal(420);
      expect(rect.height).to.equal(200);
    });
  });

  it('truncates long session titles without pushing header actions outside the chat panel', () => {
    cy.viewport(1280, 900);
    setup({ ai_chat_enabled: 'true', translation_mode: 'off' });
    const title = 'Long chat session title '.repeat(30);
    cy.intercept('GET', '/api/ai/profiles', [
      { id: 1, name: 'Long AI profile name '.repeat(10), is_default: true },
    ]);
    cy.intercept('GET', '/api/ai/chat/sessions*', [{ id: 1, article_id: article.id, title }]);
    cy.intercept('GET', '/api/ai/chat/messages*', []).as('chatMessages');
    openArticle();
    cy.get('.js-article-chat-button').click();
    cy.wait('@chatMessages');
    cy.get('.chat-panel').then(($panel) => {
      const rect = $panel[0].getBoundingClientRect();
      cy.get('.chat-panel .cursor-nw-resize').trigger('mousedown', {
        clientX: rect.left + 2,
        clientY: rect.top + 2,
        button: 0,
        force: true,
      });
      cy.document().trigger('mousemove', { clientX: rect.right, clientY: rect.bottom });
      cy.document().trigger('mouseup');
    });
    for (const [viewportWidth, viewportHeight, expectedWidth] of [
      [1280, 900, 420],
      [320, 330, 288],
    ]) {
      cy.viewport(viewportWidth, viewportHeight);
      cy.get('.chat-panel').should(($panel) => {
        const panel = $panel[0];
        const rect = panel.getBoundingClientRect();
        expect(rect.width).to.equal(expectedWidth);
        const header = panel.firstElementChild as HTMLElement;
        expect(header.scrollWidth).to.be.at.most(header.clientWidth);
        for (const button of header.querySelectorAll('button')) {
          const bounds = button.getBoundingClientRect();
          expect(bounds.width).to.be.greaterThan(0);
          expect(bounds.left).to.be.at.least(rect.left);
          expect(bounds.right).to.be.at.most(rect.right);
          expect(bounds.top).to.be.at.least(rect.top);
          expect(bounds.bottom).to.be.at.most(rect.bottom);
        }
      });
      cy.get('[data-testid="chat-session-switcher"] span')
        .should('have.text', title)
        .should(($title) => {
          expect($title[0].scrollWidth).to.be.greaterThan($title[0].clientWidth);
          expect(getComputedStyle($title[0]).textOverflow).to.equal('ellipsis');
        });
      cy.get('.chat-panel button[title="Close"]').should('be.visible');
      cy.get('[data-testid="chat-new-session"]').should('be.visible');
    }
    cy.get('.chat-panel button[title="Close"]').click();
    cy.get('.chat-panel').should('not.exist');
  });
});
