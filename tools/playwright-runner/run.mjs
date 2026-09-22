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
    if (frame === page.mainFrame()) log(`Navigated to ${frame.url()}`);
  });
  page.on('close', () => log('Browser page closed'));
});
async function step(label, action) {
  log(`Starting: ${label}`);
  try {
    const result = await action();
    log(`Completed: ${label}`);
    return result;
  } catch (error) {
    log(`Failed: ${label} — ${error.message}`);
    throw error;
  } finally {
    await capturePreview();
  }
}
const tracePath = path.join(artifactDir, 'playwright-trace.zip');
await context.tracing.start({ screenshots: true, snapshots: true, sources: true });
const previewTimer = setInterval(capturePreview, 1000);
try {
  await module.run({ context, artifactDir, step });
} finally {
  clearInterval(previewTimer);
  await capturePreview();
  await context.tracing.stop({ path: tracePath });
  log('Browser trace saved');
  await context.close();
  await browser.close();
}
