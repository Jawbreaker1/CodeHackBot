import assert from 'node:assert/strict';
import test from 'node:test';
import {webPagesSection} from './web-map.js';

const escapeHTML = value => String(value).replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('"', '&quot;');

test('project map combines runs and distinguishes page links from actual navigation', () => {
  const home = 'https://example.test/';
  const login = 'https://example.test/login';
  const account = 'https://example.test/account';
  const data = {
    kind: 'customer',
    findings: [],
    sessions: [
      {id: 'without-browser'},
      {id: 'first', updated_at: '2026-10-03T12:00:00Z'},
      {id: 'second', updated_at: '2026-10-13T12:00:00Z'}
    ],
    web_pages: [
      {session_id: 'first', task_id: 'browse', url: home, links_captured: true},
      {session_id: 'first', task_id: 'browse', url: login},
      {session_id: 'second', task_id: 'browse', url: login},
      {session_id: 'second', task_id: 'browse', url: account}
    ],
    web_links: [
      {session_id: 'first', task_id: 'browse', from: home, to: login},
      {session_id: 'first', task_id: 'browse', from: home, to: account},
      {session_id: 'first', task_id: 'browse', from: home, to: 'https://example.test/help'}
    ],
    web_transitions: [{session_id: 'second', task_id: 'browse', from: login, to: account}]
  };
  const html = webPagesSection(data, undefined, escapeHTML);
  assert.match(html, /value="all" selected>All runs/);
  assert.match(html, /2026-10-03 · Run 1/);
  assert.match(html, /2026-10-13 · Run 2/);
  assert.doesNotMatch(html, /Run 3/);
  assert.match(html, /3 opened pages · 4 captures/);
  assert.match(html, /1 recorded navigation · 2 observed links between opened pages/);
  assert.match(html, /class="link-only"/);
  assert.match(html, /class="navigation"/);
  assert.match(html, /Not opened/);
  assert.match(html, /Links were observed in the page, not necessarily opened or tested/);

  const limited = webPagesSection(data, ['first'], escapeHTML);
  assert.doesNotMatch(limited, /All runs/);
  assert.match(limited, /2 opened pages · 2 captures/);
  assert.doesNotMatch(limited, /class="navigation"/);
});

test('legacy captures say when link data was not recorded', () => {
  const html = webPagesSection({kind: 'assessment', findings: [], web_pages: [
    {session_id: 'legacy', task_id: 'browse', url: 'https://legacy.test/'}
  ]}, undefined, escapeHTML);
  assert.match(html, /This capture did not save a link inventory/);
});
