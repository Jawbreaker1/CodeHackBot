# Playwright worker helper

This optional helper gives a delegated worker a preprovisioned Playwright
browser context without adding browser logic to the coordinator. Install its
locked dependency during image preparation:

```sh
cd tools/playwright-runner
npm ci --ignore-scripts
```

The assessment image must also provide a browser. Set
`BIRDHACKBOT_BROWSER_EXECUTABLE` to an approved binary such as
`/usr/bin/chromium`; the helper never downloads a browser at task time.

Workers create a task-local scenario module and run it only after the normal
action approval:

```sh
node /absolute/path/to/tools/playwright-runner/run.mjs scenario.mjs browser-artifacts
```

The scenario exports `async function run({context, artifactDir, step})`. It may open
pages, follow the declared application flow, and write screenshots or other
bounded artifacts beneath `artifactDir`. The wrapper records a Playwright
trace at `browser-artifacts/playwright-trace.zip`. The worker must declare the
expected output paths in its action response so BirdHackBot registers them as
evidence. The coordinator still owns task planning and scope; a scenario does
not grant permission to navigate outside the declared target.

Authentication state, cookies, headers, uploads, traces, and screenshots can
contain sensitive customer data. Keep them in the task workspace, do not put
them in source control, and do not send them to a model unless the operator
has deliberately chosen that evidence for the current request.

## Observable browser steps

Wrap meaningful scenario actions in `await step('Click the sign-in button', async () => { ... })`.
The helper prints the start, completion, or failure of each step, plus actual
page-open/navigation/close events. These are visible in the worker's **Watch
execution** view alongside its exact invocation and live stdout/stderr.

The helper refreshes `browser-live.png` in `artifactDir` about once per second
and after named steps. Declare that path in the worker action's `artifacts`
array to make the preview available while running. Keep a separately named
final screenshot when it is needed as report evidence. The live preview shows
the most recently opened page; it is observation, not remote browser control.
The Playwright trace retains the detailed interaction history for local review.

Example:

```js
export async function run({context, artifactDir, step}) {
  const page = await context.newPage();
  await step('Open the authorized fixture', () => page.goto('http://127.0.0.1:8091/'));
  await step('Click Run local check', () => page.getByRole('button', {name: 'Run local check'}).click());
  await step('Save the result screenshot', () => page.screenshot({path: `${artifactDir}/result.png`}));
}
```

The worker remains responsible for restricting requests and actions to its
approved scope. Console messages describe observed activity; they are not
proof of successful validation by themselves.
