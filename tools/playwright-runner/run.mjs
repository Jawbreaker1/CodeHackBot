import { chromium } from 'playwright';
import { pathToFileURL } from 'node:url';
import path from 'node:path';
import fs from 'node:fs/promises';

const scenario = process.argv[2];
const artifactDir = path.resolve(process.argv[3] || 'browser-artifacts');
if (!scenario || scenario === '--help') {
  console.error('usage: node run.mjs SCENARIO.mjs [ARTIFACT_DIR]');
  process.exit(scenario === '--help' ? 0 : 2);
}
await fs.mkdir(artifactDir, { recursive: true, mode: 0o700 });
const module = await import(pathToFileURL(path.resolve(scenario)).href);
if (typeof module.run !== 'function') {
  throw new Error('scenario must export async function run({ context, artifactDir })');
}

const executablePath = process.env.BIRDHACKBOT_BROWSER_EXECUTABLE || undefined;
const browser = await chromium.launch({ headless: true, executablePath });
const context = await browser.newContext();
const log = message => console.log(`${new Date().toISOString()} · ${message}`);
let activeStep = '';
let activePhase = 'ready';
let lastURL = '';
let statusWrite = Promise.resolve();
const visited = new Map();
const transitions = [];
const pageLocations = new WeakMap();
let journeyWrite = Promise.resolve();
const maxRecordedPages = 40;
const maxRecordedTransitions = 120;
function recordNavigation(page, rawURL) {
  const url = visibleURL(rawURL);
  if (!url) return;
  const previous = pageLocations.get(page);
  if (!visited.has(url) && visited.size < maxRecordedPages) {
    visited.set(url, {url, screenshot: ''});
  }
  if (previous && previous !== url && transitions.length < maxRecordedTransitions && visited.has(previous) && visited.has(url)) {
    transitions.push({from: previous, to: url});
  }
  pageLocations.set(page, url);
  void publishJourney();
}
function publishJourney() {
  const payload = JSON.stringify({version: 1, pages: [...visited.values()], transitions});
  journeyWrite = journeyWrite.then(async () => {
    const temporary = path.join(artifactDir, '.browser-pages.json');
    await fs.writeFile(temporary, payload, {mode: 0o600});
    await fs.rename(temporary, path.join(artifactDir, 'browser-pages.json'));
  }).catch(error => { log(`Browser page map unavailable: ${error.message}`); });
  return journeyWrite;
}
function visibleURL(rawURL) {
  try {
    const parsed = new URL(rawURL);
    if (parsed.protocol === 'http:' || parsed.protocol === 'https:') return parsed.origin + parsed.pathname;
  } catch { /* A new page has no web URL yet. */ }
  return '';
}
function publishStatus() {
  const page = context.pages().at(-1);
  const rawURL = page && !page.isClosed() ? page.url() : '';
  lastURL = visibleURL(rawURL) || lastURL;
  const status = JSON.stringify({url: lastURL, step: activeStep, phase: activePhase, updated_at: new Date().toISOString()});
  statusWrite = statusWrite.then(async () => {
    const temporary = path.join(artifactDir, '.browser-live.json');
    await fs.writeFile(temporary, status, {mode: 0o600});
    await fs.rename(temporary, path.join(artifactDir, 'browser-live.json'));
  }).catch(error => { log(`Browser preview status unavailable: ${error.message}`); });
  return statusWrite;
}
let capturing;
async function capturePreview() {
  if (capturing) return capturing;
  const page = context.pages().at(-1);
  if (!page || page.isClosed()) return;
  capturing = (async () => {
    const bytes = await page.screenshot({type: 'png', timeout: 2000});
    const temporary = path.join(artifactDir, '.browser-live.png');
    await fs.writeFile(temporary, bytes, {mode: 0o600});
    await fs.rename(temporary, path.join(artifactDir, 'browser-live.png'));
  })().catch(() => {}).finally(() => { capturing = undefined; });
  return capturing;
}
async function capturePage() {
  const page = context.pages().at(-1);
  if (!page || page.isClosed()) return;
  recordNavigation(page, page.url());
  const url = visibleURL(page.url());
  const entry = visited.get(url);
  if (!entry) return;
  try {
    const bytes = await page.screenshot({type: 'png', timeout: 5000});
    const number = [...visited.keys()].indexOf(url) + 1;
    const filename = `browser-page-${String(number).padStart(3, '0')}.png`;
    const temporary = path.join(artifactDir, `.${filename}`);
    await fs.writeFile(temporary, bytes, {mode: 0o600});
    await fs.rename(temporary, path.join(artifactDir, filename));
    entry.screenshot = filename;
    await publishJourney();
  } catch (error) { log(`Page screenshot unavailable: ${error.message}`); }
}
context.on('page', page => {
  log('Browser page opened');
  page.on('framenavigated', frame => {
    if (frame === page.mainFrame()) {
      log(`Navigated to ${visibleURL(frame.url()) || 'a new page'}`);
      recordNavigation(page, frame.url());
      void publishStatus();
    }
  });
  page.on('close', () => { log('Browser page closed'); void publishStatus(); });
  void publishStatus();
});
async function step(label, action) {
  activeStep = label;
  activePhase = 'running';
  await publishStatus();
  log(`Starting: ${label}`);
  try {
    const result = await action();
    activePhase = 'completed';
    log(`Completed: ${label}`);
    return result;
  } catch (error) {
    activePhase = 'failed';
    log(`Failed: ${label} — ${error.message}`);
    throw error;
  } finally {
    await publishStatus();
    await capturePreview();
    await capturePage();
  }
}
const tracePath = path.join(artifactDir, 'playwright-trace.zip');
await publishStatus();
await context.tracing.start({ screenshots: true, snapshots: true, sources: true });
const previewTimer = setInterval(capturePreview, 1000);
let runFailed = false;
try {
  await module.run({ context, artifactDir, step });
} catch (error) {
  runFailed = true;
  throw error;
} finally {
  clearInterval(previewTimer);
  await capturePreview();
  await capturePage();
  await journeyWrite;
  activePhase = runFailed || activePhase === 'failed' ? 'failed' : 'finished';
  await publishStatus();
  await context.tracing.stop({ path: tracePath });
  log('Browser trace saved');
  await context.close();
  await browser.close();
}
