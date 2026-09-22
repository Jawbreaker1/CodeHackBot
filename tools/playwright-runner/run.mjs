import { chromium } from 'playwright';
import { pathToFileURL } from 'node:url';
import path from 'node:path';
import fs from 'node:fs/promises';

const scenario = process.argv[2];
const artifactDir = path.resolve(process.argv[3] || 'browser-artifacts');
if (!scenario) {
  console.error('usage: node run.mjs SCENARIO.mjs [ARTIFACT_DIR]');
  process.exit(2);
}
await fs.mkdir(artifactDir, { recursive: true, mode: 0o700 });
const module = await import(pathToFileURL(path.resolve(scenario)).href);
if (typeof module.run !== 'function') {
  throw new Error('scenario must export async function run({ context, artifactDir })');
}

const executablePath = process.env.BIRDHACKBOT_BROWSER_EXECUTABLE || undefined;
const browser = await chromium.launch({ headless: true, executablePath });
const context = await browser.newContext();
const tracePath = path.join(artifactDir, 'playwright-trace.zip');
await context.tracing.start({ screenshots: true, snapshots: true, sources: true });
try {
  await module.run({ context, artifactDir });
} finally {
  await context.tracing.stop({ path: tracePath });
  await context.close();
  await browser.close();
}
