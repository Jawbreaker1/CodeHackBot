export const $ = (id) => document.getElementById(id);
export function node(tag, className, text) {
  const element = document.createElement(tag);
  if (className) element.className = className;
  if (text !== undefined && text !== null) element.textContent = text;
  return element;
}

// Labels for typed runtime events. These are presentation, not intent parsing.
const phases = {
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
export function evidenceNode(evidence) {
  const item = node('div', 'evidence-item');
  item.append(node('div', 'muted', 'Exit: ' + (evidence.exit_status || 'Not reported')));
  if (evidence.command) item.append(node('pre', 'command', evidence.command));
  if (evidence.summary) item.append(node('p', 'pre-wrap', evidence.summary));
  for (const ref of [...(evidence.log_refs || []), ...(evidence.artifact_refs || [])]) item.append(node('code', 'evidence-ref', ref));
  return item;
}
export function eventNode(record) {
  const e = record.event;
  const entry = node('li', 'activity-item');
  const time = node('time', '', new Date(record.at).toLocaleTimeString([], {hour:'2-digit', minute:'2-digit', second:'2-digit'}));
  entry.append(time, node('strong', '', (e.task_id ? e.task_id + ' · ' : '') + phaseLabel(e.kind)));
  if (e.message) entry.append(node('p', 'pre-wrap', e.message));
  if (e.action) entry.append(node('pre', 'command', e.action));
  if (e.evidence) entry.append(evidenceNode(e.evidence));
  return entry;
}
function approvalNode(item, act) {
  const box = node('div', 'approval');
  box.append(node('h4', '', 'Approval required'), node('pre', 'command', item.command));
  box.append(disclosure('Working directory', node('code', 'evidence-ref', item.cwd), 'cwd-' + item.id));
  const row = node('div', 'action-row');
  for (const [decision, label, style] of [['approved_once', 'Approve once', ''], ['denied', 'Deny', 'secondary']]) {
    const button = node('button', style, label);
    button.type = 'button';
    button.onclick = () => act('approvals/' + encodeURIComponent(item.id), {decision}, row);
    row.append(button);
  }
  box.append(row);
  return box;
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
    if (w.action && !approvals.length) card.append(node('div', 'action-label', phase === 'execution_started' ? 'Running' : 'Latest action'), node('pre', 'command', w.action));
    for (const a of approvals) card.append(approvalNode(a, act));
    for (const q of questions) card.append(questionNode(q, act));
    const details = node('div');
    details.append(grid([
      ['Model', view.model || 'Not reported'], ['Remaining budget', w.remaining_budget || 'Not reported'],
      ['Context usage', w.context_usage || 'Not reported'], ['Dependencies', (w.depends_on || []).join(', ') || 'None'],
      ['Last exit', w.exit_status || '—'], ['Updated', w.updated_at ? new Date(w.updated_at).toLocaleTimeString() : '—'],
    ]));
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
    item.append(node('span', 'muted', 'Model-authored · ' + f.status), node('h3', '', f.title), node('p', '', f.impact));
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
