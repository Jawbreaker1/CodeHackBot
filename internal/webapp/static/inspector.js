export const $ = (id) => document.getElementById(id);
export function node(tag, className, text) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (text !== undefined && text !== null) element.textContent = text;
  return element;
}

// Labels for typed runtime events. These are presentation, not intent parsing.
const phases = {
  observation_requested: 'Local observation',
  task_queued: 'Queued', task_started: 'Starting', decision_started: 'Thinking',
  plan_finished: 'Plan updated', action_proposed: 'Action proposed',
  approval_required: 'Needs approval', execution_started: 'Running tool',
  execution_finished: 'Tool finished', post_exec_eval_started: 'Evaluating evidence',
  post_exec_eval_finished: 'Evidence reviewed', user_question: 'Needs your input',
  user_answered: 'Continuing', task_completed: 'Finished task', done: 'Finished task',
  task_failed: 'Failed', failed: 'Failed', task_blocked: 'Blocked', blocked: 'Blocked',
  aborted: 'Stopped', waiting_user: 'Needs your input', planning: 'Planning',
  plan: 'Assessment plan', assessment_started: 'Assessment started',
  round_update: 'Coordinator update',
  assessment_finished: 'Assessment finished', assessment_stop_requested: 'Stopping workers',
};
export const phaseLabel = (kind) => phases[kind] || kind;
export const isTerminal = (status) => ['done', 'task_completed', 'completed', 'task_failed', 'failed', 'task_blocked', 'blocked', 'aborted'].includes(status);
export function badge(text, phase) {
  const result = node('span', 'status', text);
  result.dataset.state = ['approval_required', 'user_question', 'waiting_user'].includes(phase) ? 'waiting' : phase;
  return result;
}
export function disclosure(title, content, key) {
  const details = node('details');
  if (key) details.dataset.disclosure = key;
  details.append(node('summary', '', title), content);
  return details;
}

// richText is a presentation renderer for model-authored prose. It treats all
// input as text and only gives special treatment to explicit fenced code and
// Mermaid flowchart blocks; it never executes markup or infers security facts.
export function richText(value, key = '') {
  const root = node('div', 'rich-text');
  const lines = String(value || '').replaceAll('\r\n', '\n').split('\n');
  let paragraph = [];
  let index = 0;
  const flush = () => {
    const text = paragraph.join('\n').trim();
    if (text) root.append(node('p', 'rich-paragraph', text));
    paragraph = [];
  };
  while (index < lines.length) {
    const line = lines[index];
    const fence = line.trimStart();
    if (fence.startsWith('```')) {
      flush();
      const language = fence.slice(3).trim().toLowerCase();
      const content = [];
      index++;
      while (index < lines.length && !lines[index].trimStart().startsWith('```')) content.push(lines[index++]);
      if (index < lines.length) index++;
      root.append(language === 'mermaid' ? mermaidNode(content.join('\n'), key + '-' + index) : codeBlock(content.join('\n'), language));
      continue;
    }
    if (!line.trim()) { flush(); index++; continue; }
    if (line.startsWith('# ')) { flush(); root.append(node('h3', 'rich-heading', line.slice(2).trim())); index++; continue; }
    if (line.startsWith('## ')) { flush(); root.append(node('h4', 'rich-heading', line.slice(3).trim())); index++; continue; }
    if (line.startsWith('- ') || line.startsWith('* ')) {
      flush();
      const list = node('ul', 'rich-list');
      while (index < lines.length && (lines[index].startsWith('- ') || lines[index].startsWith('* '))) {
        list.append(node('li', '', lines[index].slice(2).trim())); index++;
      }
      root.append(list); continue;
    }
    paragraph.push(line);
    index++;
  }
  flush();
  if (!root.children.length) root.append(node('p', 'rich-paragraph', ''));
  return root;
}
function codeBlock(value, language) {
  const pre = node('pre', 'rich-code');
  const code = node('code', '', value);
  if (language) code.dataset.language = language;
  pre.append(code);
  return pre;
}
function mermaidNode(source, key) {
  const lines = source.split('\n').map(line => line.trim()).filter(Boolean);
  const direction = lines[0]?.startsWith('flowchart') || lines[0]?.startsWith('graph');
  const edges = [];
  const labels = new Map();
  for (const line of (direction ? lines.slice(1) : lines)) {
    const arrow = line.indexOf('-->') >= 0 ? '-->' : line.indexOf('-.->') >= 0 ? '-.->' : '';
    if (!arrow) continue;
    const parts = line.split(arrow);
    if (parts.length !== 2) continue;
    const left = mermaidEndpoint(parts[0]);
    const right = mermaidEndpoint(parts[1]);
    if (!left.id || !right.id) continue;
    labels.set(left.id, left.label); labels.set(right.id, right.label);
    edges.push([left.id, right.id]);
  }
  if (!edges.length) return disclosure('Flowchart source', codeBlock(source, 'mermaid'), 'mermaid-source-' + key);
  const ids = [];
  for (const [from, to] of edges) for (const id of [from, to]) if (!ids.includes(id)) ids.push(id);
  const width = 500, rowHeight = 58, height = Math.max(74, ids.length * rowHeight + 16);
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', `0 0 ${width} ${height}`); svg.setAttribute('role', 'img'); svg.setAttribute('aria-label', 'Coordinator flowchart'); svg.classList.add('flowchart');
  const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
  const marker = document.createElementNS('http://www.w3.org/2000/svg', 'marker'); marker.id = 'arrow-' + key; marker.setAttribute('markerWidth', '8'); marker.setAttribute('markerHeight', '8'); marker.setAttribute('refX', '7'); marker.setAttribute('refY', '3'); marker.setAttribute('orient', 'auto');
  const arrow = document.createElementNS('http://www.w3.org/2000/svg', 'path'); arrow.setAttribute('d', 'M0,0 L0,6 L7,3 z'); arrow.classList.add('flowchart-arrow'); marker.append(arrow); defs.append(marker); svg.append(defs);
  const y = id => 8 + ids.indexOf(id) * rowHeight;
  for (let i = 0; i < edges.length; i++) {
    const [from, to] = edges[i];
    const line = document.createElementNS('http://www.w3.org/2000/svg', 'line'); line.setAttribute('x1', String(width / 2)); line.setAttribute('x2', String(width / 2)); line.setAttribute('y1', String(y(from) + 42)); line.setAttribute('y2', String(y(to) - 5)); line.setAttribute('marker-end', `url(#arrow-${key})`); line.classList.add('flowchart-edge'); svg.append(line);
  }
  for (const id of ids) {
    const group = document.createElementNS('http://www.w3.org/2000/svg', 'g'); group.classList.add('flowchart-node');
    const rect = document.createElementNS('http://www.w3.org/2000/svg', 'rect'); rect.setAttribute('x', '70'); rect.setAttribute('y', String(y(id))); rect.setAttribute('width', String(width - 140)); rect.setAttribute('height', '38'); rect.setAttribute('rx', '8');
    const text = document.createElementNS('http://www.w3.org/2000/svg', 'text'); text.setAttribute('x', String(width / 2)); text.setAttribute('y', String(y(id) + 24)); text.setAttribute('text-anchor', 'middle'); text.textContent = labels.get(id) || id;
    group.append(rect, text); svg.append(group);
  }
  const wrap = node('div', 'flowchart-wrap'); wrap.append(svg, disclosure('Flowchart source', codeBlock(source, 'mermaid'), 'mermaid-source-' + key)); return wrap;
}
function mermaidEndpoint(value) {
  const clean = value.trim();
  const bracket = clean.indexOf('['), close = clean.lastIndexOf(']');
  if (bracket > 0 && close > bracket) return {id: clean.slice(0, bracket).trim(), label: clean.slice(bracket + 1, close).trim()};
  const space = clean.indexOf(' ');
  return {id: space < 0 ? clean : clean.slice(0, space), label: clean};
}
export function replacePreservingDetails(target, children) {
  const known = new Set([...target.querySelectorAll('details[data-disclosure]')].map(d => d.dataset.disclosure));
  const open = new Set([...target.querySelectorAll('details[open][data-disclosure]')].map(d => d.dataset.disclosure));
  target.replaceChildren(...children);
  for (const d of target.querySelectorAll('details[data-disclosure]')) d.open = open.has(d.dataset.disclosure) || (!known.has(d.dataset.disclosure) && d.dataset.defaultOpen === 'true');
}
function grid(values) {
  const list = node('dl', 'detail-grid');
  for (const [label, value] of values) list.append(node('dt', '', label), node('dd', '', value));
  return list;
}
function formatBytes(value) {
  const bytes = Number(value) || 0;
  if (bytes < 1024) return bytes + ' B';
  return (bytes / 1024).toFixed(1) + ' KiB';
}
function contextLabel(worker) {
  if (!worker.context_limit_bytes) return worker.context_usage || 'Not reported';
  const percent = Number(worker.context_usage_percent) || Math.min(100, Math.round((worker.context_used_bytes || 0) * 100 / worker.context_limit_bytes));
  return `${formatBytes(worker.context_used_bytes)} / ${formatBytes(worker.context_limit_bytes)} (${percent}%)`;
}
export function evidenceNode(evidence) {
  const item = node('div', 'evidence-item');
  item.append(node('div', 'muted', 'Exit: ' + (evidence.exit_status || 'Not reported')));
  if (evidence.command) item.append(node('pre', 'command', evidence.command));
  if (evidence.summary) item.append(node('p', 'pre-wrap', evidence.summary));
  for (const ref of (evidence.log_refs || [])) item.append(node('code', 'evidence-ref', ref));
  const refs = evidence.artifact_refs || [];
  const urls = evidence.artifact_urls || [];
  for (let index = 0; index < refs.length; index++) {
    const ref = refs[index];
    const url = urls[index];
    const extension = ref.slice(ref.lastIndexOf('.') + 1).toLowerCase();
    if (url && ['png', 'jpg', 'jpeg', 'webp', 'gif'].includes(extension)) {
      const link = node('a', 'evidence-image-link'); link.href = url; link.target = '_blank'; link.rel = 'noreferrer';
      const image = document.createElement('img'); image.src = url; image.alt = 'Browser evidence ' + ref; image.loading = 'lazy'; image.className = 'evidence-image'; link.append(image); item.append(link);
    }
    if (url) {
      const link = node('a', 'evidence-ref', ref); link.href = url; link.target = '_blank'; link.rel = 'noreferrer'; item.append(link);
    } else item.append(node('code', 'evidence-ref', ref));
  }
  return item;
}
function observationResultNode(evidence, sequence) {
  let observation;
  try { observation = JSON.parse(evidence.summary || ''); } catch (_) { return evidenceNode(evidence); }
  if (!observation || !observation.tool || !observation.data) return evidenceNode(evidence);
  const box = node('div', 'observation-result');
  const tool = observation.tool.name || 'observation';
  if (tool === 'list_directory' && Array.isArray(observation.data.entries)) {
    const entries = observation.data.entries;
    box.append(node('strong', '', `${entries.length} entries · ${observation.data.path || 'workspace'}`));
    const list = node('ul', 'observation-list');
    for (const entry of entries) list.append(node('li', '', `${entry.type === 'directory' ? '▸' : '·'} ${entry.name}`));
    box.append(list);
    if (observation.data.truncated) box.append(node('p', 'muted', 'Listing truncated at the observation limit.'));
  } else if (tool === 'host_system') {
    const values = [];
    if (observation.data.hostname) values.push(['Hostname', observation.data.hostname.trim()]);
    if (observation.data.kernel) values.push(['Kernel', observation.data.kernel.trim()]);
    if (observation.data.architecture) values.push(['Architecture', observation.data.architecture.trim()]);
    box.append(node('strong', '', 'Host identity · fixed read-only metadata'), grid(values));
    if (observation.data.os_release) box.append(disclosure('/etc/os-release', node('pre', 'pre-wrap', observation.data.os_release), 'os-release-' + sequence));
  } else if (typeof observation.data === 'object') {
    box.append(node('strong', '', `${tool} result`));
    const summary = node('ul', 'observation-list');
    for (const [key, value] of Object.entries(observation.data)) {
      const count = Array.isArray(value) ? `${value.length} records` : typeof value === 'object' ? 'structured data' : String(value);
      summary.append(node('li', '', `${key}: ${count}`));
    }
    box.append(summary);
  }
  const raw = node('pre', 'pre-wrap', JSON.stringify(observation, null, 2));
  box.append(disclosure('Raw observation', raw, 'observation-' + sequence));
  if (observation.evidence_ref) box.append(node('code', 'evidence-ref', observation.evidence_ref));
  const refs = new Set([...(evidence.log_refs || []), ...(evidence.artifact_refs || [])]);
  for (const ref of refs) box.append(node('code', 'evidence-ref', ref));
  return box;
}
export function eventNode(record) {
  const e = record.event;
  const entry = node('li', 'activity-item');
  const time = node('time', '', new Date(record.at).toLocaleTimeString([], {hour:'2-digit', minute:'2-digit', second:'2-digit'}));
  entry.append(time, node('strong', '', (e.task_id ? e.task_id + ' · ' : '') + phaseLabel(e.kind)));
  if (e.message) entry.append(node('p', 'event-preview', preview(e.message, 160)));
  const details = node('div', 'event-detail');
  if (e.message && e.message.length > 160) details.append(richText(e.message, 'event-' + record.sequence));
  if (e.action) details.append(node('pre', 'command', e.action));
  if (e.rationale) details.append(richText(e.rationale, 'rationale-' + record.sequence));
  if (e.evidence) details.append(e.kind === 'execution_finished' ? observationResultNode(e.evidence, record.sequence) : evidenceNode(e.evidence));
  if (details.children.length) entry.append(disclosure('Details and evidence', details, 'event-details-' + record.sequence));
  return entry;
}
function questionNode(item, act) {
  const box = node('form', 'approval');
  box.append(node('h4', '', 'Worker needs your input'), node('p', 'worker-detail', item.text));
  const input = node('input');
  input.required = true;
  input.setAttribute('aria-label', 'Answer ' + item.task_id);
  const saved = $('workers').querySelector('[data-question="' + item.id + '"]');
  input.dataset.question = item.id;
  input.value = saved?.value || '';
  const button = node('button', 'primary-button', 'Reply');
  const row = node('div', 'action-row');
  row.append(button);
  box.append(input, row);
  box.onsubmit = event => { event.preventDefault(); act('questions/' + encodeURIComponent(item.id), {text: input.value}, row); };
  return box;
}
export function preview(value, limit = 200) {
  const text = String(value || '').replaceAll('\n', ' ').trim();
  return text.length > limit ? text.slice(0, limit - 1).trimEnd() + '…' : text;
}
export function renderCoordinatorPlans(view) {
  const target = $('planTimeline');
  const plans = view.plan_timeline || [];
  target.classList.toggle('hidden', !plans.length);
  if (!plans.length) return;
  const signals = {
    review: ['Choose work', 'The coordinator has proposed this work. You can choose which tasks to run.'],
    in_progress: ['In progress', 'Workers are still running. The result of this round is not known yet.'],
    needs_attention: ['Needs review', 'A worker stopped or failed. The coordinator needs to review what was established.'],
    not_run: ['Not run', 'No worker ran in this round.'],
    awaiting_review: ['Awaiting review', 'The workers finished. The coordinator has not reported what their results mean yet.'],
    continued: ['Round reviewed', 'The coordinator reviewed this round’s evidence. Its conclusion appears below.'],
    candidate: ['Possible finding', 'The coordinator reported a possible finding. Independent verification was still needed.'],
    verified: ['Finding verified', 'A separate worker verified a finding from this round.'],
    verified_final: ['Assessment complete', 'The assessment ended with an independently verified finding.'],
    concluded: ['Assessment ended', 'The coordinator ended the assessment. Read its conclusion in the conversation.'],
  };
  const taskState = {done:'Finished', running:'Working', queued:'Queued', review:'Proposed', skipped:'Not run', failed:'Failed', blocked:'Blocked', aborted:'Stopped', waiting_user:'Needs input'};
  const items = [node('h3', 'plan-heading', 'Coordinator plan')];
  for (const plan of [...plans].reverse()) {
    const [signalLabel, signalText] = signals[plan.signal] || ['Work updated', 'Review the worker result and coordinator conclusion.'];
    const content = node('div', 'plan-content');
    const purpose = plan.plain_summary || (plan.tasks?.length ? `The coordinator assigned ${plan.tasks.length} worker${plan.tasks.length === 1 ? '' : 's'} to investigate this step.` : 'The coordinator reviewed the assessment evidence.');
    content.append(node('p', 'plan-summary', preview(purpose, 220)));
    if (plan.review) content.append(node('div', 'plan-kicker', 'What we learned'), node('p', 'plan-review', preview(plan.review, 220)));
    else content.append(node('p', 'plan-status-note', signalText));
    if (plan.summary || purpose.length > 220 || (plan.review || '').length > 220) {
      const detail = node('div');
      if (purpose.length > 220) detail.append(node('p', 'worker-detail', purpose));
      if ((plan.review || '').length > 220) detail.append(node('p', 'worker-detail', plan.review));
      if (plan.summary) detail.append(richText(plan.summary, view.id + '-coordinator-summary-' + plan.round));
      content.append(disclosure('Full coordinator notes', detail, view.id + '-coordinator-summary-' + plan.round));
    }
    const tasks = node('ol', 'coordinator-tasks');
    for (const task of plan.tasks || []) {
      const item = node('li', 'coordinator-task');
      const row = node('div', 'plan-step-row');
      row.append(node('span', '', preview(task.goal || task.id, 115)), node('span', 'plan-step-state', taskState[task.status] || task.status));
      item.append(row);
      const taskDetail = node('div');
      taskDetail.append(node('p', 'worker-detail', 'Task ID: ' + task.id));
      if (task.goal) taskDetail.append(node('p', 'worker-detail', task.goal));
      if (task.done_when) taskDetail.append(node('p', 'worker-detail', 'Done when: ' + task.done_when));
      if (task.strategy_hints?.length) taskDetail.append(node('p', 'worker-detail', 'Suggested guidance: ' + task.strategy_hints.join(', ')));
      if (task.result_summary) taskDetail.append(richText(task.result_summary, 'worker-result-' + task.id));
      item.append(disclosure('Task details', taskDetail, view.id + '-coordinator-task-' + plan.round + '-' + task.id));
      tasks.append(item);
    }
    if (tasks.children.length) content.append(tasks);
    const label = `Round ${plan.round} · ${signalLabel}`;
    const details = disclosure(label, content, view.id + '-coordinator-plan-' + plan.round);
    details.dataset.signal = plan.signal || '';
    if (plan.round === plans.length) details.dataset.defaultOpen = 'true';
    items.push(details);
  }
  replacePreservingDetails(target, items);
}
function renderWorkerPlan(worker, phase, sessionID) {
  const steps = worker.plan_steps || [];
  if (!steps.length) return null;
  const current = Math.max(0, steps.indexOf(worker.active_step));
  const finished = isTerminal(phase) && ['done', 'task_completed', 'completed'].includes(phase);
  const blocked = isTerminal(phase) && !finished;
  const section = node('section', 'worker-plan-section');
  const header = node('div', 'plan-step-row');
  header.append(node('strong', '', 'Worker plan'), node('span', 'plan-step-state', `Step ${current + 1}/${steps.length} · revision ${worker.plan_revision || 1}`));
  section.append(header);
  if (worker.plan_summary) {
    section.append(node('p', 'plan-summary', preview(worker.plan_summary, 155)));
    if (worker.plan_summary.length > 155) section.append(disclosure('Full worker plan', node('p', 'worker-detail', worker.plan_summary), sessionID + '-worker-plan-summary-' + worker.id));
  }
  const list = node('ol', 'worker-plan');
  for (let i = 0; i < steps.length; i++) {
    const step = steps[i];
    const status = i < current ? 'completed' : i === current ? (finished ? 'completed' : blocked ? 'blocked' : 'active') : finished ? 'unreported' : 'queued';
    const item = node('li', 'worker-plan-step ' + status);
    const body = node('div', 'plan-content');
    body.append(node('p', 'worker-detail', worker.step_purposes?.[step] || step));
    const details = disclosure(`${preview(step, 72)} · ${status}`, body, sessionID + '-worker-step-' + worker.id + '-' + (worker.plan_revision || 1) + '-' + i);
    item.append(details);
    list.append(item);
  }
  section.append(list);
  if ((worker.plan_history || []).length > 1) {
    const history = node('ol', 'plan-revisions');
    for (const [index, revision] of worker.plan_history.slice(0, -1).entries()) {
      history.append(node('li', '', `Revision ${index + 1} · ${revision.Plan?.Summary || ''}`));
    }
    section.append(disclosure('Earlier worker plans', history, sessionID + '-worker-plan-history-' + worker.id));
  }
  return section;
}
export function renderWorkers(view, act, watch) {
  const workers = new Map((view.workers || []).map(w => [w.id, w]));
  // An approval can arrive just before the first progress snapshot.
  for (const item of [...(view.pending_approvals || []), ...(view.pending_questions || [])]) {
    if (!workers.has(item.task_id)) workers.set(item.task_id, {id: item.task_id, phase:'task_started'});
  }
  const cards = [];
  if (view.pending_tool) cards.push(node('div', 'worker-approval-note', 'Read-only observation approval is shown in the conversation.'));
  for (const w of workers.values()) {
    const approvals = (view.pending_approvals || []).filter(a => a.task_id === w.id);
    const questions = (view.pending_questions || []).filter(q => q.task_id === w.id);
    const phase = approvals.length ? 'approval_required' : questions.length ? 'user_question' : w.phase;
    const card = node('article', 'worker');
    card.dataset.worker = w.id;
    const heading = node('div', 'worker-heading');
    heading.append(node('h3', '', w.id), badge(phaseLabel(phase), phase));
    card.append(heading, node('p', 'worker-goal', preview(w.goal, 180)));
    const plan = renderWorkerPlan(w, phase, view.id);
    if (plan) card.append(plan);
    else if (w.active_step) card.append(node('p', 'worker-detail', w.active_step));
    const metrics = node('div', 'metrics');
    for (const [value, label] of [[`${w.step || 0}/${view.limits?.steps_per_task || '?'}`, 'decisions'], [w.model_calls || 0, 'calls'], [w.evidence_count || 0, 'evidence']]) {
      const metric = node('span'); metric.append(node('strong', '', value), document.createTextNode(' ' + label)); metrics.append(metric);
    }
    card.append(metrics);
    if (w.execution_log && watch) {
      const button = node('button', 'watch-button', w.phase === 'execution_started' ? 'Watch execution' : 'Inspect last execution');
      button.type = 'button'; button.onclick = () => watch(w.id); card.append(button);
    }
    if (approvals.length) card.append(node('div', 'worker-approval-note', 'Approval requested in the conversation'));
    for (const q of questions) card.append(questionNode(q, act));
    const details = node('div');
    details.append(node('div', 'action-label', 'Task objective'), node('p', 'worker-detail', w.goal));
    details.append(grid([
      ['Model', view.model || 'Not reported'], ['Remaining budget', w.remaining_budget || 'Not reported'],
      ['Context window', contextLabel(w)], ['Context remaining', w.context_limit_bytes ? formatBytes(Math.max(0, w.context_limit_bytes - w.context_used_bytes)) : 'Not reported'], ['Dependencies', (w.depends_on || []).join(', ') || 'None'],
      ['Last exit', w.exit_status || '—'], ['Updated', w.updated_at ? new Date(w.updated_at).toLocaleTimeString() : '—'],
    ]));
    if (w.action) details.append(node('div', 'action-label', phase === 'execution_started' ? 'Running invocation' : 'Latest invocation'), node('pre', 'command', w.action));
    if (w.rationale) details.append(disclosure('Model summary', node('p', 'pre-wrap', w.rationale), 'worker-rationale-' + w.id));
    if (w.done_when) details.append(node('div', 'action-label', 'Completion criteria'), node('p', '', w.done_when));
    if (w.detail) details.append(node('div', 'action-label', 'Latest update'), node('p', 'pre-wrap', w.detail));
    card.append(disclosure('Task details', details, 'task-' + w.id));
    if (w.evidence?.length) {
      const evidence = node('div');
      for (const e of w.evidence) evidence.append(evidenceNode(e));
      card.append(disclosure('Evidence & tool results · ' + w.evidence.length, evidence, 'evidence-' + w.id));
    }
    cards.push(card);
  }
  if (!cards.length) cards.push(node('p', 'empty', 'No delegated work yet. Discuss your objective with the coordinator to get started.'));
  replacePreservingDetails($('workers'), cards);
  return workers.size;
}
export function renderFindings(view) {
  const items = [];
  for (const f of view.findings || []) {
    const item = node('article', 'finding');
    item.append(node('span', 'muted', 'Model-authored · ' + f.status + (f.severity ? ' · ' + f.severity : '') + (f.confidence ? ' · ' + f.confidence + ' confidence' : '')), node('h3', '', f.title), node('p', '', f.impact));
    if (f.cve_ids?.length) item.append(node('p', 'muted', 'CVE references: ' + f.cve_ids.join(', ')));
    if (f.affected_software?.length) item.append(node('p', 'muted', 'Affected software: ' + f.affected_software.join(', ')));
    for (const [label, values] of [['Reproduction steps', f.steps], ['Evidence references', f.evidence], ['Remediation', f.remediation]]) {
      const list = node('ul');
      for (const value of values || []) list.append(node('li', 'pre-wrap', value));
      if (list.children.length) item.append(disclosure(label, list, f.title + label));
    }
    items.push(item);
  }
  if (!items.length) items.push(node('p', 'empty', 'No findings recorded. Worker observations are available under each task.'));
  replacePreservingDetails($('findings'), items);
  $('findingCount').textContent = (view.findings || []).length;
}
