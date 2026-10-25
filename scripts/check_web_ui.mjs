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
  assert.deepEqual(scriptErrors, [], 'browser JavaScript failed');
  console.log('web application: browser session creation, reopen, model selection, and reload passed');
} finally {
  await browser.close();
}
