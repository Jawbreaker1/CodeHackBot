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

The scenario exports `async function run({context, artifactDir})`. It may open
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
