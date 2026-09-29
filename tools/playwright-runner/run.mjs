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
context.on('page', page => {
  log('Browser page opened');
  page.on('framenavigated', frame => {
    if (frame === page.mainFrame()) {
      log(`Navigated to ${visibleURL(frame.url()) || 'a new page'}`);
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
  activePhase = runFailed || activePhase === 'failed' ? 'failed' : 'finished';
  await publishStatus();
  await context.tracing.stop({ path: tracePath });
  log('Browser trace saved');
  await context.close();
  await browser.close();
}
