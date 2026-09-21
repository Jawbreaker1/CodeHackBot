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
  user_answered: 'Continuing', task_completed: 'Completed', done: 'Completed',
  task_failed: 'Failed', failed: 'Failed', task_blocked: 'Blocked', blocked: 'Blocked',
  aborted: 'Stopped', waiting_user: 'Needs your input', planning: 'Planning',
  plan: 'Assessment plan', assessment_started: 'Assessment started',
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
export function replacePreservingDetails(target, children) {
  const open = new Set([...target.querySelectorAll('details[open][data-disclosure]')].map(d => d.dataset.disclosure));
  target.replaceChildren(...children);
  for (const d of target.querySelectorAll('details[data-disclosure]')) d.open = open.has(d.dataset.disclosure);
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
  for (const ref of [...(evidence.log_refs || []), ...(evidence.artifact_refs || [])]) item.append(node('code', 'evidence-ref', ref));
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
  if (e.message) entry.append(node('p', 'pre-wrap', e.message));
  if (e.action) entry.append(node('pre', 'command', e.action));
  if (e.rationale) entry.append(disclosure('Model summary', node('p', 'pre-wrap', e.rationale), 'rationale-' + record.sequence));
  if (e.evidence) entry.append(e.kind === 'execution_finished' ? observationResultNode(e.evidence, record.sequence) : evidenceNode(e.evidence));
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
export function renderWorkers(view, act) {
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
    card.append(heading, node('p', 'worker-goal', w.goal));
    if (w.active_step) card.append(node('p', 'worker-detail', w.active_step));
    const metrics = node('div', 'metrics');
    for (const [value, label] of [[w.step || 0, 'step'], [w.model_calls || 0, 'calls'], [w.evidence_count || 0, 'evidence']]) {
      const metric = node('span'); metric.append(node('strong', '', value), document.createTextNode(' ' + label)); metrics.append(metric);
    }
    card.append(metrics);
    if (approvals.length) card.append(node('div', 'worker-approval-note', 'Approval requested in the conversation'));
    for (const q of questions) card.append(questionNode(q, act));
    const details = node('div');
    details.append(grid([
      ['Model', view.model || 'Not reported'], ['Remaining budget', w.remaining_budget || 'Not reported'],
      ['Context window', contextLabel(w)], ['Context remaining', w.context_limit_bytes ? formatBytes(Math.max(0, w.context_limit_bytes - w.context_used_bytes)) : 'Not reported'], ['Dependencies', (w.depends_on || []).join(', ') || 'None'],
      ['Last exit', w.exit_status || '—'], ['Updated', w.updated_at ? new Date(w.updated_at).toLocaleTimeString() : '—'],
    ]));
    if (w.action) details.append(node('div', 'action-label', phase === 'execution_started' ? 'Running invocation' : 'Latest invocation'), node('pre', 'command', w.action));
    if (w.rationale) details.append(disclosure('Model summary', node('p', 'pre-wrap', w.rationale), 'worker-rationale-' + w.id));
    if (w.done_when) details.append(node('div', 'action-label', 'Completion criteria'), node('p', '', w.done_when));
    if (w.detail) details.append(node('div', 'action-label', 'Latest update'), node('p', 'pre-wrap', w.detail));
    if (w.plan_steps?.length) {
      const plan = node('ol', 'worker-plan');
      for (const step of w.plan_steps) plan.append(node('li', step === w.active_step ? 'active' : '', step));
      details.append(node('div', 'action-label', 'Worker plan'), plan);
    }
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
