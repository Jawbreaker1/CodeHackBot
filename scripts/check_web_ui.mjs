#!/usr/bin/env node
// Exercise the session controls in the built web app against check_webapp.py's
// isolated fixture server. No model request or external target is involved.
import assert from 'node:assert/strict';
import {existsSync} from 'node:fs';
import {createRequire} from 'node:module';

const require = createRequire(new URL('../tools/playwright-runner/package.json', import.meta.url));
const {chromium} = require('playwright');
const base = process.argv[2];
if (!base) throw new Error('usage: node scripts/check_web_ui.mjs <web-base-url>');

const executablePath = process.env.BIRDHACKBOT_BROWSER_EXECUTABLE ||
  (existsSync('/usr/bin/chromium') ? '/usr/bin/chromium' : undefined);
const browser = await chromium.launch({headless: true, ...(executablePath ? {executablePath} : {})});
try {
  const page = await browser.newPage();
  const scriptErrors = [];
  page.on('pageerror', error => scriptErrors.push(error.message));
  page.on('response', response => {
    if (response.request().resourceType() === 'script' && !response.ok()) {
      scriptErrors.push(`${response.status()} ${response.url()}`);
    }
  });
  const index = async () => (await (await page.request.get(base + '/api/v1/customers')).json()).intakes || [];
  const before = new Set((await index()).map(session => session.id));

  await page.goto(base, {waitUntil: 'domcontentloaded'});
  await page.locator('#newAssessment').waitFor();
  assert.equal((await page.request.get(base + '/api/v1/intake')).status(), 405);
  assert.deepEqual(new Set((await index()).map(session => session.id)), before, 'GET created a draft');

  await page.locator('#newAssessment').click();
  await page.waitForFunction(ids => {
    const selected = localStorage.getItem('birdhackbot.selectedSession') || '';
    return selected.startsWith('/api/v1/intake/') && !ids.includes(selected.split('/').at(-1));
  }, [...before]);
  const first = (await index()).filter(session => !before.has(session.id));
  assert.equal(first.length, 1, 'New session did not create exactly one draft');
  const firstID = first[0].id;
  assert.equal(await page.locator('#model').textContent(), 'terminal-fixture');
  assert.equal(await page.locator(`[data-session="${firstID}"]`).getAttribute('aria-current'), 'page');

  await page.locator('#model').click();
  await page.locator('#modelCustom').fill('terminal-fixture-alternate');
  await page.locator('#modelApply').click();
  await page.waitForFunction(() => document.querySelector('#model')?.textContent === 'terminal-fixture-alternate');
  const saved = await (await page.request.get(base + '/api/v1/intake/' + firstID)).json();
  assert.equal(saved.model, 'terminal-fixture-alternate');

  await page.reload({waitUntil: 'domcontentloaded'});
  await page.waitForFunction(() => document.querySelector('#model')?.textContent === 'terminal-fixture-alternate');
  assert.equal(await page.locator(`[data-session="${firstID}"]`).getAttribute('aria-current'), 'page');

  await page.locator('#newAssessment').click();
  await page.waitForFunction(id => {
    const selected = localStorage.getItem('birdhackbot.selectedSession') || '';
    return selected.startsWith('/api/v1/intake/') && !selected.endsWith('/' + id);
  }, firstID);
  const created = (await index()).filter(session => !before.has(session.id));
  assert.equal(created.length, 2, 'second click did not create exactly one more draft');
  assert.equal(await page.locator('#model').textContent(), 'terminal-fixture');
  await page.locator(`[data-session="${firstID}"]`).click();
  await page.waitForFunction(() => document.querySelector('#model')?.textContent === 'terminal-fixture-alternate');
  assert.equal(await page.locator(`[data-session="${firstID}"]`).getAttribute('aria-current'), 'page');

  // A long assessment keeps current work visible while older rounds and
  // completed workers remain available without filling the entire inspector.
  const inspector = await page.evaluate(async () => {
    const {renderWorkers, renderCoordinatorPlans} = await import('/inspector.js');
    const view = {
      id: 'layout-fixture', model: 'fixture-model', context_window: {limit_tokens: 128000},
      workers: [
        {id:'working', phase:'execution_started', goal:'Inspect the current fixture', active_step:'Check one response', plan_steps:['Check one response']},
        {id:'blocked', phase:'blocked', goal:'Review a stopped fixture task'},
        {id:'finished-a', phase:'done', goal:'First completed task'},
        {id:'finished-b', phase:'done', goal:'Second completed task'},
      ],
      plan_timeline: [1, 2, 3].map(round => ({round, signal:'continued', plain_summary:`Round ${round}`})),
    };
    renderWorkers(view);
    renderCoordinatorPlans(view);
    return {
      visibleWorkers: document.querySelectorAll('#workers > .worker').length,
      finishedWorkers: document.querySelectorAll('#workers > .finished-workers .worker').length,
      finishedCollapsed: !document.querySelector('#workers > .finished-workers').open,
      latestRoundOpen: document.querySelector('#planTimeline > details[data-signal]').open,
      olderRounds: document.querySelectorAll('#planTimeline > .plan-history-disclosure .plan-history > details').length,
      olderCollapsed: !document.querySelector('#planTimeline > .plan-history-disclosure').open,
      workerContext: document.querySelector('#workers .worker .detail-grid')?.textContent || '',
    };
  });
  assert.equal(inspector.visibleWorkers, 2);
  assert.equal(inspector.finishedWorkers, 2);
  assert.equal(inspector.finishedCollapsed, true);
  assert.equal(inspector.latestRoundOpen, true);
  assert.equal(inspector.olderRounds, 2);
  assert.equal(inspector.olderCollapsed, true);
  assert.doesNotMatch(inspector.workerContext, /KiB|bytes/i);

  // The Analysis composer researches automatically when useful and keeps
  // web sources visibly separate from saved test evidence.
  let researchRequest;
  const previousMessages = Array.from({length: 14}, (_, index) => ({
    role: index % 2 ? 'assistant' : 'user',
    text: `Saved exchange ${index + 1}. ` + 'A recorded result remains available for review. '.repeat(8),
  }));
  await page.route('**/api/v1/assessments/ui-research/analysis/assistant', async route => {
    if (route.request().method() === 'POST') {
      researchRequest = route.request().postDataJSON();
      await route.fulfill({contentType:'application/json', body: JSON.stringify({profile:'fixture', model:'fixture', research_available:true, messages:[...previousMessages,
        {role:'user', text:researchRequest.question},
        {role:'assistant', text:'A public advisory is available; the saved run did not test whether it applies.', sources:[
          {id:'W1',kind:'web',label:'Public advisory',url:'https://example.org/advisory',retrieved_at:'2026-10-26T10:00:00Z'},
          {id:'P1',label:'Overall analysis',url:'/analysis?assessment=ui-research'}
        ]}
      ]})});
    } else {
      await route.fulfill({contentType:'application/json', body: JSON.stringify({profile:'fixture', model:'fixture', research_available:true, messages:previousMessages})});
    }
  });
  await page.addStyleTag({url:base + '/analysis.css'});
  await page.evaluate(async () => {
    const container = document.createElement('div');
    container.id = 'analysis-assistant';
    document.body.append(container);
    const assistant = await import('/analysis-assistant.js');
    assistant.mountAssistant(container, {kind:'assessment', id:'ui-research'});
    await assistant.loadAssistant({kind:'assessment', id:'ui-research'});
  });
  const transcriptPosition = () => page.locator('.assistant-transcript').evaluate(element => ({
    remaining: element.scrollHeight - element.clientHeight - element.scrollTop,
    scrollable: element.scrollHeight > element.clientHeight,
  }));
  assert.equal((await transcriptPosition()).scrollable, true, 'fixture transcript should scroll');
  assert.ok((await transcriptPosition()).remaining < 2, 'saved conversation did not open at the latest message');
  await page.locator('.assistant-transcript').evaluate(element => { element.scrollTop = 0; });
  assert.equal(await page.locator('#assistant-web-research').count(), 0);
  await page.locator('#assistant-question').fill('What do current advisories say?');
  await page.locator('#assistant-form button[type="submit"]').click();
  await page.locator('.assistant-sources').first().waitFor();
  assert.ok((await transcriptPosition()).remaining < 2, 'new answer did not scroll into view');
  assert.equal(researchRequest.allow_web_research, undefined);
  assert.match(await page.locator('.assistant-sources').first().textContent(), /From saved results/);
  assert.match(await page.locator('.assistant-sources').last().textContent(), /Public web sources/);
  assert.equal(await page.locator('.assistant-sources').last().locator('a').getAttribute('href'), 'https://example.org/advisory');
  assert.deepEqual(scriptErrors, [], 'browser JavaScript failed');
  console.log('web application: browser session controls and analysis research presentation passed');
} finally {
  await browser.close();
}
