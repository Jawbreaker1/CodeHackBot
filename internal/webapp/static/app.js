import {$, node, badge, disclosure, richText, eventNode, phaseLabel, preview, replacePreservingDetails, renderWorkers, renderCoordinatorPlans, renderFindings, renderArtifacts} from './inspector.js';

let current = null;
let selection = 0;
let pendingMessage = null;
let selectedFiles = [];
let starting = false;
let eventRecords = new Map();
let signatures = {};
let modelCatalog = [];
let folderNames = new Map();
let profileMode = false;
let loginPoll = null;
let subscriptionReady = false;
const subscriptionStates = {
  ready: ['Bridge ready', 'Codex ChatGPT sign-in and the local subscription bridge are available on this host. The selected model is verified when a request completes.'],
  sign_in_required: ['Sign-in needed', 'Use Continue with ChatGPT below to connect this Kali host.'],
  bridge_token_missing: ['Bridge setup needed', 'The private local bridge token is missing. Finish the subscription bridge setup on this Kali host.'],
  bridge_token_mismatch: ['Bridge token mismatch', 'The web server and the local bridge have different client tokens. Restart the bridge with the configured token file.'],
  bridge_unavailable: ['Bridge stopped', 'The local subscription bridge is not reachable. Start it before selecting Daybreak for a model request.'],
  external_bridge: ['External bridge configured', 'This subscription endpoint is managed outside the local web server; BirdHackBot has not verified its connection.']
};
function showSubscriptionState(models) {
  const selected = models.find(item => item.id === current?.model_profile && item.provider === 'subscription') || models.find(item => item.provider === 'subscription');
  subscriptionReady = selected?.connection === 'ready';
  const [label, detail] = selected ? subscriptionStates[selected.connection] || ['Connection unknown', 'Check the configured ChatGPT subscription bridge.'] : ['Not configured', 'Add a ChatGPT subscription model profile on this Kali host.'];
  $('settingsSubscriptionValue').textContent = label;
  $('subscriptionDetail').textContent = detail;
  if ($('startChatGPTLogin')) $('startChatGPTLogin').textContent = subscriptionReady ? 'Switch ChatGPT account' : 'Continue with ChatGPT';
}
async function refreshSubscriptionState() {
  try { const data = await api('/api/v1/models'); showSubscriptionState(data.models || []); }
  catch (error) { $('settingsSubscriptionValue').textContent = 'Unavailable'; $('subscriptionDetail').textContent = error.message; }
}
function showChatGPTLogin(state) {
  const status = state?.status || 'idle';
  const active = status === 'starting' || status === 'awaiting_user';
  $('startChatGPTLogin').classList.toggle('hidden', active);
  $('startChatGPTLogin').textContent = status === 'connected' || subscriptionReady ? 'Switch ChatGPT account' : 'Continue with ChatGPT';
  $('cancelChatGPTLogin').classList.toggle('hidden', !active);
  $('chatGPTLoginInstructions').classList.toggle('hidden', status === 'idle');
  $('chatGPTLoginMessage').textContent = state?.message || '';
  $('chatGPTLoginCode').classList.toggle('hidden', status !== 'awaiting_user');
  $('chatGPTLoginCode').textContent = status === 'awaiting_user' ? state.user_code || '' : '';
  $('chatGPTLoginLink').classList.toggle('hidden', status !== 'awaiting_user');
  if (status === 'awaiting_user') $('chatGPTLoginLink').href = state.verification_url;
  else $('chatGPTLoginLink').removeAttribute('href');
  if (status === 'connected') refreshSubscriptionState();
  if (!active && loginPoll) { clearInterval(loginPoll); loginPoll = null; }
  if (active && !loginPoll) loginPoll = setInterval(refreshChatGPTLogin, 1500);
}
async function refreshChatGPTLogin() {
  if (!$('subscriptionDialog').open) return;
  try { showChatGPTLogin(await api('/api/v1/subscription/login/status')); }
  catch (error) { showChatGPTLogin({status: 'failed', message: error.message}); }
}
let deletingSession = null;
const narrow = matchMedia('(max-width: 1150px)');
const mobile = matchMedia('(max-width: 680px)');
const activeStatuses = ['running', 'starting'];
function isAssessmentView(view) { return !!view && typeof view.goal === 'string'; }
function displayStatus(status) { return (status || '').replaceAll('_', ' '); }

async function api(path, options = {}) {
  const headers = options.body instanceof FormData ? {} : {'Content-Type': 'application/json'};
  const response = await fetch(path, {headers, ...options});
  const text = await response.text();
  const body = text ? JSON.parse(text) : {};
  if (!response.ok) throw new Error(body.error || response.statusText);
  return body;
}
function sessionModelPath() {
  return isAssessmentView(current) ? '/api/v1/assessments/' + encodeURIComponent(current.id) + '/model' : '/api/v1/intake/' + encodeURIComponent(current?.id || '') + '/model';
}
function sessionPath(view) {
  return isAssessmentView(view) ? '/api/v1/assessments/' + encodeURIComponent(view.id) : '/api/v1/intake/' + encodeURIComponent(view?.id || '');
}
function renderModelOptions(models, selected) {
	const target = $('modelOptions');
	if (!models.length) {
		target.replaceChildren(node('p', 'empty', profileMode ? 'No model profiles are configured.' : 'The provider did not publish a model catalog. Enter an exact model ID below.'));
		return;
	}
  target.replaceChildren(...models.map(item => {
    const label = node('label', 'model-option' + (item.id === selected ? ' active' : ''));
    const radio = document.createElement('input');
    radio.type = 'radio'; radio.name = 'model-choice'; radio.value = item.id; radio.checked = item.id === selected;
		const copy = node('span', '', item.label || item.id);
		if (profileMode) {
      copy.append(node('small', '', `${item.provider === 'subscription' ? 'ChatGPT plan' : 'Local model'} · ${item.model}`));
      if (item.provider === 'subscription') copy.append(node('small', 'model-connection', (subscriptionStates[item.connection] || ['Connection unknown'])[0]));
    }
		else if (item.current) copy.append(node('small', '', 'Configured provider model'));
    label.append(radio, copy);
    return label;
  }));
}
async function openModelPicker() {
	const dialog = $('modelDialog');
	$('modelDialogHint').textContent = current?.can_change_model === false ? 'This session keeps its model. Choosing another opens a new session; current work continues.' : 'Choose a configured model for this session. Each model keeps its own endpoint and limits.';
	$('modelCustom').value = '';
  $('modelOptions').replaceChildren(node('p', 'empty', 'Loading available models…'));
  dialog.showModal();
	try {
		const data = await api('/api/v1/models');
		modelCatalog = data.models || [];
		showSubscriptionState(modelCatalog);
		profileMode = !!data.profiles_enabled;
		$('modelCustom').parentElement.classList.toggle('hidden', profileMode);
		$('modelApply').textContent = current?.can_change_model === false ? 'Use in new session' : 'Use model';
		renderModelOptions(modelCatalog, profileMode ? (current?.model_profile || data.current || '') : (current?.model || data.current || ''));
		if (data.catalog_error) $('modelDialogHint').textContent += ' ' + data.catalog_error;
	} catch (error) {
		profileMode = false;
		$('modelCustom').parentElement.classList.remove('hidden');
		renderModelOptions([], current?.model || '');
    $('modelDialogHint').textContent = error.message;
  }
}
function showError(error) {
  $('errorBanner').textContent = error.message;
  $('errorBanner').classList.remove('hidden');
}
function clearError() { $('errorBanner').classList.add('hidden'); }
function changed(key, value) {
  const signature = JSON.stringify(value);
  if (signatures[key] === signature) return false;
  signatures[key] = signature;
  return true;
}
function setInspector(open) {
  $('shell').classList.toggle('inspector-open', open && narrow.matches);
  $('shell').classList.toggle('inspector-hidden', !open && !narrow.matches);
  $('toggleInspector').setAttribute('aria-expanded', String(open));
  $('drawerBackdrop').classList.toggle('hidden', !open || !narrow.matches);
}
function closePanels() {
  setInspector(false);
  $('shell').classList.remove('sidebar-open');
  $('toggleSidebar').setAttribute('aria-expanded', String(!mobile.matches));
}
function selectTab(button) {
  for (const tab of $('inspectorTabs').querySelectorAll('[role="tab"]')) {
    const selected = tab === button;
    tab.setAttribute('aria-selected', String(selected));
    tab.tabIndex = selected ? 0 : -1;
    $(tab.dataset.panel).classList.toggle('hidden', !selected);
  }
}
function messageNode(message) {
  const entry = node('article', 'transcript-entry ' + message.role);
  const complete = String(message.text || '');
  const rich = richText(complete, 'message-' + (message.at || ''));
  rich.classList.add('transcript-text');
  entry.append(node('div', 'transcript-role', message.role === 'user' ? 'You' : message.role === 'assistant' ? 'Coordinator' : 'System'), rich);
  if (message.attachments?.length || message.images?.length) {
    const files = node('div', 'message-attachments');
    for (const attachment of [...(message.attachments || []), ...(message.images || [])]) {
      const item = node('div', 'message-attachment');
      if (attachment.mime_type?.startsWith('image/') && attachment.url) {
        const image = document.createElement('img');
        image.src = attachment.url; image.alt = attachment.filename || 'Attached image'; image.loading = 'lazy'; image.className = 'message-attachment-image';
        const preview = document.createElement('a');
        preview.href = attachment.url; preview.target = '_blank'; preview.rel = 'noreferrer'; preview.className = 'message-attachment-preview';
        preview.append(image); item.append(preview);
      }
      const link = document.createElement('a');
      link.href = attachment.url; link.target = '_blank'; link.rel = 'noreferrer'; link.className = 'attachment-chip';
      link.textContent = attachment.filename + ' · ' + formatBytes(attachment.bytes);
      item.append(link); files.append(item);
    }
    entry.append(files);
  }
  return entry;
}
function conclusionNode(view) {
  const entry = messageNode({role: 'assistant', text: view.conclusion, at: 'conclusion-' + view.id});
  if (view.conclusion_detail && view.conclusion_detail !== view.conclusion) {
    entry.append(disclosure('Full conclusion and evidence references', richText(view.conclusion_detail, 'conclusion-detail-' + view.id), 'conclusion-detail-' + view.id));
  }
  return entry;
}
function chatApprovalNode(item, kind) {
  const box = node('article', 'chat-approval');
  box.setAttribute('aria-live', 'assertive');
  const observation = kind === 'observation';
  const observationNames = {host_system: 'Identify this computer’s operating system', local_network: 'Inspect this computer’s network metadata', list_directory: 'List files in the workspace', dns_lookup: 'Look up public DNS', web_fetch: 'Read a public page'};
  const title = observation ? item.summary || observationNames[item.tool.name] || 'Review coordinator observation' : item.summary || 'Review worker execution';
  box.append(node('div', 'chat-approval-title', title));
  const impact = observation ? item.impact || 'Read-only observation; review its exact target below.' : item.impact || 'The worker has not explained the effects. Review the command before approving.';
  if (item.target) box.append(node('p', 'approval-target', item.target));
  box.append(node('p', 'worker-detail', impact));
  if (!observation && item.risk !== 'low') {
    const warning = item.risk === 'dangerous' ? 'Potentially dangerous · review before running' : 'Risk uncertain · review before running';
    box.append(node('p', 'approval-risk', item.review_reason ? warning + ' · ' + item.review_reason : warning));
  }
  const technical = node('div');
  technical.append(node('pre', 'command', observation ? JSON.stringify(item.tool, null, 2) : item.command));
  if (item.cwd) technical.append(node('code', 'evidence-ref', item.cwd));
  if (item.task_id) technical.append(node('p', 'muted', 'Worker: ' + item.task_id));
  box.append(disclosure('Command details', technical, 'approval-command-' + item.id));
  const row = node('div', 'action-row');
  for (const [decision, label, style] of [['approved_once', kind === 'observation' ? 'Allow once' : 'Approve once', ''], ['denied', 'Deny', 'secondary']]) {
    const button = node('button', style, label);
    button.type = 'button';
    button.onclick = () => act('approvals/' + encodeURIComponent(item.id), {decision}, row);
    row.append(button);
  }
  box.append(row);
  return box;
}
function planReviewNode(plan) {
  const box = node('article', 'chat-approval plan-review');
  box.setAttribute('aria-live', 'assertive');
  const research = plan.phase === 'research';
  box.append(node('div', 'chat-approval-title', research ? 'Review proposed research' : 'Review proposed test sequence'));
  if (plan.plain_summary) box.append(node('p', 'plan-summary', plan.plain_summary));
  box.append(node('p', 'worker-detail', research ? 'Select which research tasks should run.' : 'Select which tests should run. Unselected tasks will not execute or become evidence.'));
  const choices = node('div', 'plan-choices');
  for (const task of plan.tasks || []) {
    const label = node('label', 'plan-choice');
    const input = document.createElement('input');
    input.type = 'checkbox'; input.value = task.id; input.checked = true;
    label.append(input, node('span', '', preview(task.goal, 170)), node('small', '', task.id));
    choices.append(label);
    if (task.done_when) choices.append(disclosure('What this task should establish', node('p', 'worker-detail', task.done_when), 'review-task-' + plan.id + '-' + task.id));
  }
  box.append(choices);
  const row = node('div', 'action-row');
  const run = node('button', '', 'Run selected'); run.type = 'button';
  run.onclick = () => {
    const ids = [...choices.querySelectorAll('input:checked')].map(input => input.value);
    if (!ids.length) return showError(new Error('Select at least one proposed task, or reject the plan.'));
    act('plans/' + encodeURIComponent(plan.id), {decision: 'approved', approved_task_ids: ids}, row);
  };
  const reject = node('button', 'secondary', 'Reject plan'); reject.type = 'button';
  reject.onclick = () => act('plans/' + encodeURIComponent(plan.id), {decision: 'denied'}, row);
  const redirect = node('button', 'secondary', 'Suggest another path'); redirect.type = 'button';
  redirect.onclick = () => { $('chatInput').focus(); $('chatInput').placeholder = 'Tell the coordinator what you would prefer to investigate…'; };
  row.append(run, redirect, reject); box.append(row);
  return box;
}
function traceNode(records) {
  const list = node('ol', 'activity-list');
  for (const record of records) list.append(eventNode(record));
  const tasks = new Set(records.map(r => r.event.task_id).filter(id => id && id !== 'coordinator'));
  const tools = records.filter(r => r.event.kind === 'execution_finished').length;
  const label = ['Coordinator activity'];
  if (tasks.size) label.push(tasks.size + ' worker' + (tasks.size === 1 ? '' : 's'));
  if (tools) label.push(tools + ' tool result' + (tools === 1 ? '' : 's'));
  const brief = node('ol', 'trace-brief');
  for (const record of records.slice(-4)) {
    const event = record.event;
    const item = node('li');
    item.append(node('strong', '', (event.task_id ? event.task_id + ' · ' : '') + phaseLabel(event.kind)));
    const description = event.message || event.rationale || event.action;
    if (description) item.append(node('span', '', preview(description, 130)));
    brief.append(item);
  }
  const content = node('div', 'trace-content');
  content.append(brief, disclosure(`All ${records.length} recorded events`, list, 'trace-events-' + records[0].sequence));
  const trace = disclosure(label.join(' · '), content, 'trace-' + records[0].sequence);
  trace.className = 'trace';
  return trace;
}
function reportNode(view) {
  const card = node('article', 'chat-report');
  card.append(node('div', 'chat-report-title', 'Assessment report ready'));
  const links = node('div', 'chat-report-links');
  const report = node('a', '', 'Open session report ↗');
  report.href = view.report_url;
  report.target = '_blank';
  report.rel = 'noopener';
  const analysis = node('a', '', 'Review findings ↗');
  analysis.href = '/analysis?assessment=' + encodeURIComponent(view.id);
  analysis.target = '_blank';
  analysis.rel = 'noopener';
  links.append(report, analysis);
  if (view.evidence_index_url) {
    const evidence = node('a', '', 'Review execution evidence ↗');
    evidence.href = view.evidence_index_url;
    evidence.target = '_blank';
    evidence.rel = 'noopener';
    links.append(evidence);
  }
  card.append(links);
  return card;
}
function workProgressNode(view) {
  if (!isAssessmentView(view) || !activeStatuses.includes(view.status) || view.pending_plan) return null;
  const workers = (view.workers || []).filter(worker => !['task_completed', 'done', 'task_failed', 'failed', 'task_blocked', 'blocked', 'aborted'].includes(worker.phase));
  const card = node('section', 'chat-work-progress');
  card.setAttribute('role', 'status');
  card.append(node('div', 'chat-work-title', workers.length ? `Working · ${workers.length} ${workers.length === 1 ? 'worker' : 'workers'}` : 'Coordinator working'));
  if (!workers.length) {
    const message = view.pending_approvals?.length ? 'Waiting for your approval.' : view.results?.length ? 'Reviewing results and deciding what comes next.' : view.plans > 0 ? 'Preparing approved workers.' : 'Planning the first test.';
    card.append(node('p', '', message));
  } else {
    for (const worker of workers) {
      const waiting = (view.pending_approvals || []).some(approval => approval.task_id === worker.id);
      const purpose = worker.step_purposes?.[worker.active_step] || worker.active_step || worker.goal || phaseLabel(worker.phase);
      const line = node('p');
      line.append(node('strong', '', worker.id + ' · '), node('span', '', waiting ? 'Waiting for your approval' : preview(purpose, 180)));
      card.append(line);
    }
  }
  return card;
}
function renderTranscript() {
  const messages = [...(current?.messages || [])];
  const records = [...eventRecords.values()];
  const activeWork = (current?.workers || []).map(w => [w.id, w.phase, w.active_step, w.step_purposes?.[w.active_step], w.goal]);
  if (!changed('transcript', [messages, records, activeWork, pendingMessage, current?.pending_tool, current?.pending_approvals, current?.pending_plan, current?.conclusion, current?.conclusion_detail, current?.report_ready, current?.report_url, current?.evidence_index_url])) return;
  const pane = $('chat');
  const follow = pane.scrollHeight - pane.scrollTop - pane.clientHeight < 100;
  const scrollTop = pane.scrollTop;
  const items = [];
  const timeline = [
    ...messages.map((message, index) => ({at: message.at, message, index})),
    ...records.filter(r => !['operator_message', 'coordinator_message'].includes(r.event.kind)).map(record => ({at: record.at, record})),
  ];
  const finishAt = current?.finished_at || records.find(r => r.event.kind === 'assessment_finished')?.at || '9999-12-31';
  if (current?.conclusion) timeline.push({at: finishAt, conclusion: current});
  if (isAssessmentView(current) && current.report_ready) timeline.push({at: finishAt, report: current});
  timeline.sort((a, b) => new Date(a.at) - new Date(b.at));
  let trace = [];
  const flushTrace = () => { if (trace.length) items.push(traceNode(trace)); trace = []; };
  for (const item of timeline) {
    if (item.record?.event.kind === 'round_update') { flushTrace(); items.push(messageNode({role:'assistant', text:item.record.event.message, at:'round-' + item.record.sequence})); }
    else if (item.record) trace.push(item.record);
    else if (item.conclusion) { flushTrace(); items.push(conclusionNode(item.conclusion)); }
    else if (item.report) { flushTrace(); items.push(reportNode(item.report)); }
    else { flushTrace(); items.push(messageNode(item.message)); }
  }
  flushTrace();
  const progress = workProgressNode(current);
  if (progress) items.push(progress);
  for (const approval of current?.pending_approvals || []) items.push(chatApprovalNode(approval, 'action'));
  if (current?.pending_tool) items.push(chatApprovalNode(current.pending_tool, 'observation'));
  if (current?.pending_plan) items.push(planReviewNode(current.pending_plan));
  if (pendingMessage) {
    const last = messages[messages.length - 1];
    if (!last || last.role !== 'user' || last.text !== pendingMessage) items.push(messageNode({role:'user', text:pendingMessage}));
    items.push(node('div', 'pending-message', current?.pending_tool || current?.pending_approvals?.length ? 'Waiting for your approval…' : 'Coordinator is responding…'));
  }
  if (!items.length) {
    const welcome = node('div', 'welcome');
    const mark = document.createElement('img');
    mark.src = '/birdhackbot-hero.png'; mark.alt = 'Detailed BirdHackBot cybernetic raven'; mark.className = 'welcome-artwork';
    welcome.append(mark, node('h2', '', 'What are we investigating?'), node('p', '', "Tell us what you're testing.\nLet's find out what holds up—and what doesn't."));
    items.push(welcome);
  }
  replacePreservingDetails(pane, items);
  pane.scrollTop = follow ? pane.scrollHeight : scrollTop;
}
function updateComposer() {
  const assessment = isAssessmentView(current);
  const finished = assessment && ['completed', 'completed_with_gaps', 'incomplete', 'aborted'].includes(current.status);
  const canChat = !assessment || activeStatuses.includes(current.status) || finished;
  $('chatInput').disabled = !current || !canChat;
  $('send').disabled = !current || !canChat || !!pendingMessage || (!$('chatInput').value.trim() && !selectedFiles.length);
  $('newAssessment').disabled = starting;
  $('conversationState').textContent = current?.pending_plan ? (current.pending_plan.phase === 'research' ? 'Waiting for your research selection · No worker is running' : 'Waiting for your test selection · No worker is running') : current?.pending_tool ? 'Waiting for your approval · No tool is running' : pendingMessage ? 'Coordinator is responding…' : finished ? 'Assessment finished · Ask a question or continue the work here' : current?.resumable ? 'Session paused · Open Workers to review and resume' : assessment ? 'Workers can run while you discuss the assessment' : 'Ready when you are';
  $('chatInput').placeholder = finished ? 'Ask about results, request a report, or continue the assessment…' : current?.resumable ? 'Resume this session to continue' : 'Ask, investigate, or plan an assessment…';
}
function formatBytes(value) {
  const bytes = Number(value) || 0;
  if (bytes < 1024) return bytes + ' B';
  return (bytes / 1024).toFixed(1) + ' KiB';
}
function renderWorkStatus(view) {
  const assessmentRunning = isAssessmentView(view) && activeStatuses.includes(view.status);
  const workers = assessmentRunning ? (view.workers || []).filter(worker => !['task_completed', 'done', 'task_failed', 'failed', 'task_blocked', 'blocked', 'aborted', 'interrupted'].includes(worker.phase)) : [];
  const approvals = view.pending_approvals || [];
  const questions = view.pending_questions || [];
  let coordinator = '';
  let coordinatorState = 'working';
  if (view.pending_plan) {
    coordinatorState = 'waiting';
    coordinator = view.pending_plan.phase === 'research' ? 'Waiting for research selection' : 'Waiting for plan review';
  } else if (view.pending_tool) {
    coordinatorState = 'waiting';
    coordinator = 'Waiting for tool approval';
  } else if (pendingMessage || view.status === 'thinking') {
    coordinator = 'Responding';
  } else if (workers.length) {
    coordinator = 'Overseeing ' + workers.length + (workers.length === 1 ? ' worker' : ' workers');
  } else if (assessmentRunning) {
    const latest = [...eventRecords.values()].sort((a, b) => a.sequence - b.sequence).at(-1)?.event;
    coordinator = latest?.kind === 'planning' ? 'Planning next steps' : latest?.kind === 'research' ? 'Researching' : 'Working on assessment';
  }
  const items = [];
  const item = (name, description, state) => {
    const chip = node('div', 'live-status-item');
    chip.dataset.state = state;
    chip.append(node('strong', '', name), node('span', '', description));
    return chip;
  };
  if (coordinator) items.push(item('Coordinator', coordinator, coordinatorState));
  for (const worker of workers) {
    const waiting = approvals.some(approval => approval.task_id === worker.id) || questions.some(question => question.task_id === worker.id);
    const phase = waiting ? approvals.some(approval => approval.task_id === worker.id) ? 'Needs approval' : 'Needs your input' : phaseLabel(worker.phase);
    const description = worker.active_step && !waiting ? phase + ' · ' + preview(worker.active_step, 58) : phase;
    items.push(item(worker.id, description, waiting ? 'waiting' : worker.phase === 'task_queued' ? 'queued' : 'working'));
  }
  $('liveStatus').classList.toggle('hidden', !items.length);
  if (changed('live-status', [view.id, items.map(item => [item.textContent, item.dataset.state])])) $('liveStatusItems').replaceChildren(...items);
}
function renderOverview(view) {
  const assessment = isAssessmentView(view);
  const running = activeStatuses.includes(view.status);
  const status = assessment ? (view.pending_plan ? 'Plan ready' : displayStatus(view.status)) : view.pending_tool ? 'Needs approval' : view.status === 'thinking' ? 'Thinking' : view.proposal ? 'Ready for review' : 'Conversation';
  $('assessmentStatus').textContent = status;
  $('assessmentStatus').dataset.state = view.status;
  renderWorkStatus(view);
  $('assessmentGoal').textContent = view.goal || 'Workers appear here as the coordinator delegates work.';
  $('assessmentApproach').classList.toggle('hidden', !view.approach);
  $('assessmentApproach').textContent = view.approach ? `${view.approach.label} investigation · initial estimate ${view.approach.estimate}` : '';
  $('assessmentMetrics').classList.toggle('hidden', !assessment);
  $('assessmentMetrics').textContent = (view.usage?.calls || 0) + ' assessment calls · ' + (view.post_run_usage?.calls || 0) + ' follow-up calls · ' + (view.plans || 0) + ' plans';
  const context = view.context_window || {};
  const hasContext = !!(assessment && context.limit_bytes > 0);
  $('contextWindow').classList.toggle('hidden', !hasContext);
  if (hasContext) {
    const percent = Math.max(0, Math.min(100, Number(context.percent) || 0));
    $('contextUsageLabel').textContent = context.used_bytes ? formatBytes(context.used_bytes) + ' / ' + formatBytes(context.limit_bytes) : 'No request yet';
    $('contextFill').style.width = percent + '%';
    $('contextFill').dataset.state = percent >= 90 ? 'high' : percent >= 75 ? 'warm' : '';
    $('contextUsageDetail').textContent = context.used_bytes ? `${context.active ? 'Current' : 'Last saved'} request · ${percent}% of app input limit` : 'Waiting for coordinator request';
    const agents = view.context_windows || [context];
    if (changed('context-breakdown', [view.id, agents])) {
      const intro = node('p', 'context-explainer', 'Each agent has a separate request packet. These are bytes against the app’s input limit, not the model’s token window.');
      const rows = agents.map(agent => {
        const row = node('div', 'context-agent');
        const name = node('span', 'context-agent-name', agent.role === 'worker' ? `Worker · ${agent.agent_id}` : 'Coordinator');
        const state = node('span', 'context-agent-status', agent.status === 'interrupted' ? 'Interrupted' : agent.status === 'waiting' ? 'Waiting for workers' : agent.status === 'thinking' ? 'Thinking' : agent.status === 'finished' ? 'Finished' : agent.status === 'idle' ? 'Idle' : phaseLabel(agent.status));
        const measure = node('span', 'context-agent-measure', agent.used_bytes ? `${agent.active ? 'Current' : 'Last saved'} · ${formatBytes(agent.used_bytes)} / ${formatBytes(agent.limit_bytes)} · ${agent.percent}%` : 'No request yet');
        row.append(name, state, measure);
        return row;
      });
      $('contextBreakdown').replaceChildren(intro, ...rows);
    }
  }
  const readiness = assessment ? view.report_readiness : null;
  const checks = readiness?.checks || [];
  $('reportReadiness').classList.toggle('hidden', !readiness || !checks.length);
  if (readiness && checks.length) {
    const attention = checks.filter(check => check.status === 'needs_attention').length;
    $('reportReadinessStatus').textContent = attention ? attention + ' to review' : readiness.status === 'ready_for_review' ? 'Ready for review' : 'Collecting';
    $('reportReadiness').dataset.state = attention ? 'needs_attention' : readiness.status;
    if (changed('report-readiness', [view.id, checks])) {
      $('reportReadinessChecks').replaceChildren(...checks.map(check => {
        const item = node('li', '', check.label + ' · ' + check.detail);
        item.dataset.state = check.status;
        return item;
      }));
    }
  }
  $('scopeDetails').classList.toggle('hidden', !view.scope);
  $('assessmentScope').textContent = view.scope || '';
  $('assessmentLimits').textContent = view.limits?.workers ? 'Up to ' + view.limits.workers + ' workers · ' + view.limits.tasks + ' tasks · ' + view.limits.model_calls + ' model calls' : '';
  $('stop').classList.toggle('hidden', !assessment || !running);
  $('resume').classList.toggle('hidden', !view.resumable);
  $('report').classList.toggle('hidden', !assessment || !view.report_ready);
  if (view.report_url) $('report').href = view.report_url;
  $('proposalReview').classList.toggle('hidden', !view.proposal);
  $('reviewProposal').classList.toggle('hidden', !view.proposal);
  $('start').disabled = starting;
  if (view.proposal) {
    $('proposalGoal').textContent = view.proposal.goal;
    $('proposalScope').textContent = view.proposal.scope;
    if (!$('customer').value.trim() || view.customer) $('customer').value = view.customer || '';
  }
  if (changed('approaches', [view.id, view.proposal?.approaches])) {
    const options = view.proposal?.approaches || [];
    $('approachSection').classList.toggle('hidden', !options.length);
    $('approachOptions').replaceChildren(...options.map((option, index) => {
      const label = node('label', 'approach-option');
      const input = document.createElement('input');
      input.type = 'radio'; input.name = 'approach'; input.value = option.id;
      input.checked = index === Math.floor(options.length / 2);
      label.append(input, node('strong', '', `${option.label} · ${option.estimate}`), node('small', '', option.description));
      return label;
    }));
  }
  $('customerReport').classList.toggle('hidden', !assessment);
  if (assessment) $('customerReport').href = '/api/v1/customers/' + encodeURIComponent(view.customer) + '/report';
  $('analysisLink').classList.toggle('hidden', !assessment);
  if (assessment) $('analysisLink').href = '/analysis?assessment=' + encodeURIComponent(view.id);
  $('contextLink').classList.toggle('hidden', !assessment);
  if (assessment) $('contextLink').href = '/context?assessment=' + encodeURIComponent(view.id);
}
function renderView(view) {
  current = view;
  for (const record of view.events || []) eventRecords.set(record.sequence, record);
  $('sessionTitle').textContent = view.title || view.goal || 'New session';
  $('sessionTitle').title = view.title || view.goal || 'New session';
  $('headerCustomer').textContent = folderNames.get(view.customer) || view.customer || 'Workspace';
  $('model').textContent = view.model || 'Model not configured';
  $('permissions').textContent = permissionLabels[view.permission_mode] || permissionLabels.per_action;
  $('permissions').disabled = !isAssessmentView(view) && view.model_busy;
  $('model').title = view.model ? 'Change model · ' + view.model : 'Choose a model';
  renderTranscript();
  renderOverview(view);
  if (changed('plans', [view.id, view.plan_timeline, view.pending_plan])) renderCoordinatorPlans(view);
  if (changed('workers', [view.id, view.workers, view.context_window, view.pending_approvals, view.pending_questions, view.pending_tool, view.model])) {
    $('workerCount').textContent = renderWorkers(view, act, openWatch, openBrowser);
  }
  syncBrowserWorkers(view);
  if (changed('findings', [view.id, view.findings])) renderFindings(view);
  if (changed('artifacts', [view.id, view.artifacts])) renderArtifacts(view);
  const records = [...eventRecords.values()];
  if (changed('activity', records)) {
    if (records.length) $('activity').replaceChildren(...records.map(eventNode));
    else $('activity').replaceChildren(node('li', 'empty', 'Runtime events will appear here.'));
  }
  const attention = (view.pending_approvals?.length || 0) + (view.pending_questions?.length || 0) + (view.pending_tool ? 1 : 0) + (view.pending_plan ? 1 : 0);
  $('attentionDot').classList.toggle('hidden', !attention);
  $('toggleInspector').title = attention ? attention + ' pending operator actions' : 'Show worker details';
  updateComposer();
  markSelection();
}
function markSelection() {
  for (const button of $('sidebarSessions').querySelectorAll('[data-session]')) {
    const selected = button.dataset.session === current?.id;
    button.classList.toggle('active', selected);
    if (selected) button.setAttribute('aria-current', 'page');
    else button.removeAttribute('aria-current');
  }
}
function sessionRow(session, isIntake) {
  const label = session.title || session.goal || 'Untitled session';
  const row = node('div', 'session-row');
  row.draggable = true;
  row.title = 'Drag into a folder';
  row.addEventListener('dragstart', event => {
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', JSON.stringify({id:session.id, isIntake}));
    row.classList.add('dragging');
  });
  row.addEventListener('dragend', () => row.classList.remove('dragging'));
  const button = node('button', 'session-link');
  button.dataset.session = session.id;
  button.title = isIntake ? label : label + ' · ' + displayStatus(session.status);
  const dot = node('span', 'session-dot ' + session.status);
  dot.setAttribute('aria-hidden', 'true');
  button.append(dot, node('span', 'session-link-title', label));
  button.setAttribute('aria-label', label + (isIntake ? '' : ' · ' + displayStatus(session.status)) + ' · ' + session.id.slice(-6));
  button.onclick = () => isIntake ? selectIntake(session.id) : selectSession(session.id);
  const remove = node('button', 'session-delete');
  remove.type = 'button';
  remove.title = 'Delete session';
  remove.setAttribute('aria-label', 'Delete ' + label);
  const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  icon.setAttribute('class', 'icon');
  const use = document.createElementNS('http://www.w3.org/2000/svg', 'use');
  use.setAttribute('href', '#icon-trash');
  icon.append(use);
  remove.append(icon);
  remove.onclick = () => deleteSession(session, isIntake);
  row.append(button, remove);
  return row;
}
let movingSession = null;
async function assignSession(id, customer, isIntake) {
  if (movingSession) return;
  movingSession = id;
  try {
    const prefix = isIntake ? '/api/v1/intake/' : '/api/v1/assessments/';
    const view = await api(prefix + encodeURIComponent(id) + '/customer', {method:'POST', body: JSON.stringify({customer})});
    if (current?.id === id) renderView(view);
    clearError();
    await refreshSidebar();
  } catch (error) { showError(error); }
  finally { movingSession = null; }
}
function sessionPathFromIndex(data, exclude = '') {
  const candidates = [...(data.intakes || []).map(s => ({...s, kind:'intake'}))];
  for (const group of data.customers || []) {
    candidates.push(...(group.drafts || []).map(s => ({...s, kind:'intake'})));
    candidates.push(...(group.sessions || []).map(s => ({...s, kind:'assessments'})));
  }
  candidates.sort((a, b) => (Date.parse(b.updated_at) || 0) - (Date.parse(a.updated_at) || 0) || b.id.localeCompare(a.id));
  const selected = candidates.find(s => s.id !== exclude);
  return selected ? '/api/v1/' + selected.kind + '/' + encodeURIComponent(selected.id) : null;
}
async function openExistingSession(exclude = '') {
  const data = await api('/api/v1/customers');
  const path = sessionPathFromIndex(data, exclude);
  if (path) return navigate(path);
  current = null;
  clearError();
  localStorage.removeItem('birdhackbot.selectedSession');
  $('sessionTitle').textContent = 'No session selected';
  $('headerCustomer').textContent = 'Workspace';
  $('chat').replaceChildren(node('div', 'welcome', 'Create a session to start a conversation.'));
  $('proposalReview').classList.add('hidden');
  $('reviewProposal').classList.add('hidden');
  $('workers').replaceChildren();
  $('activity').replaceChildren();
  $('workerCount').textContent = '0';
  updateComposer();
  await refreshSidebar();
  return false;
}
async function deleteSession(session, isIntake) {
  if (deletingSession) return;
  const label = session.title || session.goal || 'this session';
  const contents = isIntake ? 'conversation and local observations' : 'conversation, evidence, and report, including its findings in the customer summary';
  if (!window.confirm('Delete "' + label + '"?\n\nThis permanently removes its ' + contents + '. This cannot be undone.')) return;
  const path = (isIntake ? '/api/v1/intake/' : '/api/v1/assessments/') + encodeURIComponent(session.id);
  deletingSession = session.id;
  try {
    await api(path, {method: 'DELETE'});
    clearError();
    if (current?.id === session.id) {
      localStorage.removeItem('birdhackbot.selectedSession');
      await openExistingSession(session.id);
    } else {
      await refreshSidebar();
    }
  } catch (error) { showError(error); }
  finally { deletingSession = null; }
}
async function refreshSidebar() {
  try {
    const data = await api('/api/v1/customers');
    folderNames = new Map((data.customers || []).map(group => [group.id, group.title || group.id]));
    if (current) $('headerCustomer').textContent = folderNames.get(current.customer) || current.customer || 'Workspace';
    const groups = (data.customers || []).map(g => ({id:g.id, title:g.title || g.id, sessions:(g.sessions || []).map(s => ({id:s.id, goal:s.goal, title:s.goal, status:s.status})), drafts:(g.drafts || []).map(s => ({id:s.id, title:s.title || 'New session', status:s.status}))}));
    const intakes = (data.intakes || []).map(s => ({id:s.id, goal:s.title || 'New session', title:s.title || 'New session', status:s.status}));
    if (!changed('sidebar', [groups, intakes])) { markSelection(); return; }
    const collapsed = new Set([...$('sidebarSessions').querySelectorAll('[data-customer][aria-expanded="false"]')].map(e => e.dataset.customer));
    const sections = [];
    if (intakes.length) {
      const section = node('section', 'customer-group');
      const heading = node('button', 'customer-heading');
      heading.append(node('span', '', 'Draft conversations'), node('span', 'customer-count', intakes.length));
      const list = node('div', '');
      for (const session of [...intakes].reverse()) {
        list.append(sessionRow(session, true));
      }
      section.append(heading, list); sections.push(section);
    }
    for (const group of groups) {
      const section = node('section', 'customer-group');
      section.dataset.customer = group.id;
      section.title = 'Drop a session here to place it in ' + group.title;
      section.addEventListener('dragover', event => { if (event.dataTransfer.types.includes('text/plain')) { event.preventDefault(); event.dataTransfer.dropEffect = 'move'; section.classList.add('drag-over'); } });
      section.addEventListener('dragleave', event => { if (!section.contains(event.relatedTarget)) section.classList.remove('drag-over'); });
      section.addEventListener('drop', event => { event.preventDefault(); section.classList.remove('drag-over'); try { const item = JSON.parse(event.dataTransfer.getData('text/plain')); if (item?.id) assignSession(item.id, group.id, !!item.isIntake); } catch { /* Ignore unrelated drops. */ } });
      const heading = node('button', 'customer-heading');
      heading.dataset.customer = group.id;
      heading.setAttribute('aria-expanded', String(!collapsed.has(group.id)));
      heading.append(node('span', '', group.title), node('span', 'customer-count', group.sessions.length + group.drafts.length));
      const analysis = node('a', 'customer-analysis-link', 'Analysis');
      analysis.href = '/analysis?customer=' + encodeURIComponent(group.id);
      analysis.target = '_blank';
      analysis.rel = 'noopener';
      analysis.setAttribute('aria-label', 'Analyze all sessions for ' + group.id);
      const header = node('div', 'customer-header');
      header.append(heading, analysis);
      const list = node('div', collapsed.has(group.id) ? 'hidden' : '');
      heading.onclick = () => { const hide = !list.classList.contains('hidden'); list.classList.toggle('hidden', hide); heading.setAttribute('aria-expanded', String(!hide)); };
      for (const draft of [...group.drafts].reverse()) list.append(sessionRow(draft, true));
      for (const session of [...group.sessions].reverse()) {
        list.append(sessionRow(session, false));
      }
      section.append(header, list);
      sections.push(section);
    }
    $('sidebarSessions').replaceChildren(...(sections.length ? sections : [node('p', 'empty', 'Your assessments will appear here.')]));
    markSelection();
  } catch (error) { showError(error); }
}
async function navigate(path) {
  closeWatch();
  closeBrowser();
  const token = ++selection;
  current = null;
  pendingMessage = null;
  eventRecords = new Map();
  signatures = {sidebar: signatures.sidebar};
  $('chatInput').value = '';
  clearError();
  updateComposer();
  try {
    const view = await api(path);
    if (token !== selection) return;
    renderView(view);
    localStorage.setItem('birdhackbot.selectedSession', sessionPath(view));
    $('chat').scrollTop = $('chat').scrollHeight;
    if (!isAssessmentView(view)) $('chatInput').focus();
    $('shell').classList.remove('sidebar-open');
    $('drawerBackdrop').classList.add('hidden');
    refreshSidebar();
    return true;
  } catch (error) { if (token === selection) showError(error); return false; }
}
function selectSession(id) { return navigate('/api/v1/assessments/' + encodeURIComponent(id)); }
function selectIntake(id) { return navigate('/api/v1/intake/' + encodeURIComponent(id)); }
async function act(path, body, container) {
  const id = current.id;
  const token = selection;
  for (const button of container.querySelectorAll('button')) button.disabled = true;
  try {
    const prefix = isAssessmentView(current) ? '/api/v1/assessments/' : '/api/v1/intake/';
    const view = await api(prefix + encodeURIComponent(id) + '/' + path, {method:'POST', body: JSON.stringify(body)});
    if (token === selection) { clearError(); renderView(view); }
  } catch (error) {
    if (token === selection) showError(error);
  } finally {
    for (const button of container.querySelectorAll('button')) button.disabled = false;
  }
}
$('composer').onsubmit = async event => {
  event.preventDefault();
  const text = $('chatInput').value.trim();
  if ((!text && !selectedFiles.length) || pendingMessage || !current) return;
  const token = selection;
  const path = isAssessmentView(current) ? '/api/v1/assessments/' : '/api/v1/intake/';
  const url = path + encodeURIComponent(current.id) + '/messages';
  const files = [...selectedFiles];
  pendingMessage = text || 'Please inspect the attached artifact(s).';
  $('chatInput').value = '';
  $('chatInput').style.height = '';
  selectedFiles = []; renderAttachmentList();
  clearError(); renderTranscript(); updateComposer(); renderWorkStatus(current);
  $('chat').scrollTop = $('chat').scrollHeight;
  try {
    let body;
    if (files.length) {
      body = new FormData(); body.append('text', text);
      for (const file of files) body.append('attachment', file, file.name);
    } else { body = JSON.stringify({text}); }
    const view = await api(url, {method:'POST', body});
    if (token !== selection) return;
    pendingMessage = null;
    renderView(view);
    localStorage.setItem('birdhackbot.selectedSession', sessionPath(view));
    $('chatInput').focus();
  } catch (error) {
    if (token !== selection) return;
    pendingMessage = null;
    selectedFiles = files; renderAttachmentList();
    showError(error); updateComposer(); renderTranscript(); renderWorkStatus(current);
    $('chatInput').value = text;
    updateComposer();
  }
};
function renderAttachmentList() {
  const list = $('attachmentList');
  list.replaceChildren(...selectedFiles.map((file, index) => {
    const chip = node('span', 'attachment-chip');
    chip.append(node('span', '', file.name));
    const remove = node('button', 'attachment-remove', '×'); remove.type = 'button'; remove.title = 'Remove ' + file.name;
    remove.onclick = () => { selectedFiles.splice(index, 1); renderAttachmentList(); updateComposer(); };
    chip.append(remove); return chip;
  }));
  list.classList.toggle('hidden', !selectedFiles.length);
}
$('attach').onclick = () => $('attachments').click();
$('attachments').onchange = () => {
  selectedFiles = [...$('attachments').files].slice(0, 4);
  $('attachments').value = ''; renderAttachmentList(); updateComposer();
};
$('chatInput').oninput = () => {
  $('chatInput').style.height = 'auto';
  $('chatInput').style.height = Math.min($('chatInput').scrollHeight, 180) + 'px';
  updateComposer();
};
$('chatInput').onkeydown = event => {
  if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) { event.preventDefault(); $('composer').requestSubmit(); }
};
$('startForm').onsubmit = async event => {
  event.preventDefault();
  if (!current?.proposal || starting) return;
  const token = selection;
  starting = true; $('start').disabled = true; updateComposer();
  try {
    const view = await api('/api/v1/intake/' + encodeURIComponent(current.id) + '/start', {method:'POST', body:JSON.stringify({customer:$('customer').value.trim(), approach_id:document.querySelector('#approachOptions input:checked')?.value || ''})});
    if (token !== selection) return;
    eventRecords = new Map();
    renderView(view); localStorage.setItem('birdhackbot.selectedSession', sessionPath(view)); refreshSidebar(); setInspector(true);
  } catch (error) { if (token === selection) showError(error); }
  finally { starting = false; $('start').disabled = false; updateComposer(); }
};
$('stop').onclick = () => act('stop', {}, $('assessmentStatus').parentElement);
$('resume').onclick = () => act('start', {}, $('assessmentStatus').parentElement);
$('newAssessment').onclick = () => navigate('/api/v1/intake');
$('newFolder').onclick = () => { $('folderTitle').value = ''; $('folderError').textContent = ''; $('folderDialog').showModal(); $('folderTitle').focus(); };
$('closeFolder').onclick = $('cancelFolder').onclick = () => $('folderDialog').close();
$('folderForm').onsubmit = async event => {
  event.preventDefault();
  try {
    await api('/api/v1/customers', {method:'POST', body: JSON.stringify({title:$('folderTitle').value.trim()})});
    $('folderDialog').close(); clearError(); await refreshSidebar();
  } catch (error) { $('folderError').textContent = error.message; }
};
$('model').onclick = openModelPicker;
for (const button of document.querySelectorAll('[data-close-model]')) button.onclick = () => $('modelDialog').close();
$('modelForm').onsubmit = async event => {
  event.preventDefault();
  if (!current) return;
	const custom = profileMode ? '' : $('modelCustom').value.trim();
	const selected = custom || document.querySelector('#modelOptions input[name="model-choice"]:checked')?.value || (profileMode ? current.model_profile : current.model);
	if (!selected) { $('modelDialogHint').textContent = 'Choose or enter a model ID.'; return; }
	$('modelApply').disabled = true;
	try {
		if (current.can_change_model === false) {
			const draft = await api('/api/v1/intake');
			await api('/api/v1/intake/' + encodeURIComponent(draft.id) + '/model', {method:'POST', body: JSON.stringify(profileMode ? {profile:selected} : {model:selected})});
			$('modelDialog').close(); clearError();
			await navigate('/api/v1/intake/' + encodeURIComponent(draft.id));
		} else {
			const view = await api(sessionModelPath(), {method:'POST', body: JSON.stringify(profileMode ? {profile:selected} : {model:selected})});
			renderView(view); $('modelDialog').close(); clearError();
		}
  } catch (error) { $('modelDialogHint').textContent = error.message; showError(error); }
  finally { $('modelApply').disabled = false; }
};
$('toggleInspector').onclick = () => setInspector($('toggleInspector').getAttribute('aria-expanded') !== 'true');
$('closeInspector').onclick = closePanels;
$('collapseWorkers').onclick = () => {
  for (const details of $('workers').querySelectorAll('details')) details.open = false;
};
$('drawerBackdrop').onclick = closePanels;
$('reviewProposal').onclick = () => { setInspector(true); $('proposalReview').scrollIntoView({block:'nearest'}); };
$('toggleSidebar').onclick = () => {
  const open = !$('shell').classList.contains('sidebar-open');
  $('shell').classList.toggle('sidebar-open', open);
  $('toggleSidebar').setAttribute('aria-expanded', String(open));
  $('drawerBackdrop').classList.toggle('hidden', !open);
};
for (const tab of $('inspectorTabs').querySelectorAll('[role="tab"]')) {
  tab.onclick = () => selectTab(tab);
  tab.onkeydown = event => {
    if (!['ArrowLeft', 'ArrowRight'].includes(event.key)) return;
    event.preventDefault();
    const tabs = [...$('inspectorTabs').querySelectorAll('[role="tab"]')];
    const next = tabs[(tabs.indexOf(tab) + (event.key === 'ArrowRight' ? 1 : tabs.length - 1)) % tabs.length];
    selectTab(next); next.focus();
  };
}
document.addEventListener('keydown', event => { if (event.key === 'Escape') closePanels(); });
narrow.addEventListener('change', () => { closePanels(); setInspector(!narrow.matches); });
setInspector(!narrow.matches);
selectTab($('workersTab'));
$('toggleSidebar').setAttribute('aria-expanded', String(!mobile.matches));
const permissionLabels = {per_action: 'Approve every execution', dangerous_only: 'Review risky actions', full_access: 'Approve everything'};
$('settings').onclick = () => {
  $('settingsModelValue').textContent = current?.model || 'Choose a model';
  $('settingsPermissionsValue').textContent = permissionLabels[current?.permission_mode] || permissionLabels.per_action;
  $('settingsPermissions').disabled = $('permissions').disabled;
  $('appSettingsDialog').showModal();
  refreshSubscriptionState();
};
$('closeAppSettings').onclick = () => $('appSettingsDialog').close();
$('settingsModel').onclick = () => { $('appSettingsDialog').close(); openModelPicker(); };
$('settingsSubscription').onclick = () => { $('appSettingsDialog').close(); $('subscriptionDialog').showModal(); refreshSubscriptionState(); refreshChatGPTLogin(); };
$('closeSubscription').onclick = () => $('subscriptionDialog').close();
$('subscriptionDialog').addEventListener('close', () => { if (loginPoll) { clearInterval(loginPoll); loginPoll = null; } });
$('startChatGPTLogin').onclick = async () => {
  showChatGPTLogin({status: 'starting', message: 'Preparing ChatGPT sign-in…'});
  try { showChatGPTLogin(await api('/api/v1/subscription/login/start', {method: 'POST'})); }
  catch (error) { showChatGPTLogin({status: 'failed', message: error.message}); }
};
$('cancelChatGPTLogin').onclick = async () => {
  try { showChatGPTLogin(await api('/api/v1/subscription/login/cancel', {method: 'POST'})); }
  catch (error) { showChatGPTLogin({status: 'failed', message: error.message}); }
};
$('settingsPermissions').onclick = () => { $('appSettingsDialog').close(); $('permissions').click(); };
$('permissions').onclick = () => {
  const selected = current?.permission_mode || 'per_action';
  $('permissionsForm').querySelector('input[value="' + selected + '"]').checked = true;
  $('permissionAck').checked = false;
  $('permissionError').textContent = '';
  syncPermissionChoice(); $('permissionsDialog').showModal();
};
function syncPermissionChoice() {
  const mode = $('permissionsForm').querySelector('input[name="permission-mode"]:checked')?.value;
  $('permissionAcknowledgement').classList.toggle('hidden', mode === 'per_action');
  $('permissionAck').required = mode !== 'per_action';
}
$('permissionsForm').onchange = syncPermissionChoice;
$('closePermissions').onclick = () => $('permissionsDialog').close();
$('permissionsForm').onsubmit = async event => {
  event.preventDefault();
  const mode = $('permissionsForm').querySelector('input[name="permission-mode"]:checked').value;
  const token = selection;
  try {
    const view = await api(sessionPath(current) + '/permissions', {method:'POST', body:JSON.stringify({mode, acknowledge:$('permissionAck').checked})});
    if (token === selection) { clearError(); renderView(view); }
    $('permissionsDialog').close();
  } catch (error) { $('permissionError').textContent = error.message; }
};
let watchTimer = null;
let watchGeneration = 0;
function closeWatch() { watchGeneration++; clearTimeout(watchTimer); $('watchDialog').close(); }
$('closeWatch').onclick = closeWatch;
$('watchDialog').addEventListener('cancel', () => { watchGeneration++; clearTimeout(watchTimer); });
$('watchStop').onclick = async () => { await act('stop', {}, $('watchDialog')); };
function openWatch(workerID) {
  const token = selection, generation = ++watchGeneration;
  const path = sessionPath(current) + '/watch?worker=' + encodeURIComponent(workerID);
  clearTimeout(watchTimer);
  $('watchTitle').textContent = workerID;
  $('watchOutput').textContent = 'Waiting for tool output…';
  $('watchImage').classList.add('hidden');
  $('watchDialog').showModal();
  async function update() {
    if (token !== selection || generation !== watchGeneration || !$('watchDialog').open) return;
    try {
      const data = await api(path);
      if (token !== selection || generation !== watchGeneration || !$('watchDialog').open) return;
      $('watchPhase').textContent = data.phase === 'execution_started' ? 'Running · updates every second' : 'Execution ended · recorded output';
      $('watchCommand').textContent = data.action;
      $('watchOutput').textContent = data.stdout + (data.stderr ? '\n' + data.stderr : '') || (data.phase === 'execution_started' ? 'No output yet. The worker is still running.' : 'This execution produced no console output.');
      const image = $('watchImage');
      if (data.images.length) { image.src = data.images[0] + '&preview=' + Date.now(); image.classList.remove('hidden'); }
      $('watchStop').classList.toggle('hidden', !activeStatuses.includes(current.status));
    } catch (error) { $('watchOutput').textContent = error.message; }
    watchTimer = setTimeout(update, 1000);
  }
  update();
}
let browserTimer = null;
let browserGeneration = 0;
let browserWorker = '';
let browserAutoHidInspector = false;
function browserWorkers(view) {
  if (!isAssessmentView(view)) return [];
  return (view.workers || []).filter(worker => [
    ...(worker.expected_artifacts || []),
    ...(worker.evidence || []).flatMap(evidence => evidence.artifact_refs || []),
  ].some(ref => ref.split('/').at(-1) === 'browser-live.png'));
}
function closeBrowser() {
  browserGeneration++;
  clearTimeout(browserTimer);
  $('browserPanel').classList.add('hidden');
  $('toggleBrowser').setAttribute('aria-expanded', 'false');
  $('workArea').parentElement.classList.remove('browser-open', 'browser-expanded');
  $('toggleBrowser').textContent = 'View browser';
  $('workArea').classList.remove('browser-expanded');
  $('expandBrowser').textContent = 'Expand';
  $('expandBrowser').setAttribute('aria-pressed', 'false');
  if (browserAutoHidInspector) setInspector(true);
  browserAutoHidInspector = false;
}
function syncBrowserWorkers(view) {
  const workers = browserWorkers(view);
  $('toggleBrowser').classList.toggle('hidden', !workers.length);
  if (!workers.length) { closeBrowser(); return; }
  $('toggleBrowser').textContent = $('browserPanel').classList.contains('hidden')
    ? (workers.some(worker => worker.phase === 'execution_started') ? 'Watch browser' : 'View browser')
    : 'Hide browser';
  if (!workers.some(worker => worker.id === browserWorker)) browserWorker = workers[0].id;
  const picker = $('browserWorker');
  const ids = workers.map(worker => worker.id);
  if (JSON.stringify([...picker.options].map(option => option.value)) !== JSON.stringify(ids)) {
    picker.replaceChildren(...workers.map(worker => {
      const option = document.createElement('option'); option.value = worker.id; option.textContent = worker.id; return option;
    }));
  }
  picker.value = browserWorker;
  picker.parentElement.classList.toggle('hidden', workers.length < 2);
}
function openBrowser(workerID) {
  const workers = browserWorkers(current);
  if (!workers.length) return;
  if ($('browserPanel').classList.contains('hidden') && !narrow.matches && innerWidth < 1600 && !$('shell').classList.contains('inspector-hidden')) {
    browserAutoHidInspector = true;
    setInspector(false);
  }
  browserWorker = workers.some(worker => worker.id === workerID) ? workerID : workers[0].id;
  $('browserPanel').classList.remove('hidden');
  $('workArea').parentElement.classList.add('browser-open');
  $('toggleBrowser').setAttribute('aria-expanded', 'true');
  syncBrowserWorkers(current);
  browserGeneration++;
  clearTimeout(browserTimer);
  updateBrowser(selection, browserGeneration);
}
async function updateBrowser(sessionToken, generation) {
  if (sessionToken !== selection || generation !== browserGeneration || $('browserPanel').classList.contains('hidden')) return;
  try {
    const data = await api(sessionPath(current) + '/watch?worker=' + encodeURIComponent(browserWorker) + '&browser=1');
    if (sessionToken !== selection || generation !== browserGeneration || $('browserPanel').classList.contains('hidden')) return;
    const phaseLabels = {ready: 'Ready', running: 'Running', completed: 'Step complete', failed: 'Step failed', finished: 'Finished'};
    $('browserPhase').textContent = phaseLabels[data.browser?.phase] || (data.phase === 'execution_started' ? 'Running' : 'Last captured view');
    $('browserURL').textContent = data.browser?.url || 'Waiting for navigation';
    $('browserURL').title = data.browser?.url || '';
    $('browserStep').textContent = data.browser?.step || 'Waiting for browser activity';
    const image = $('browserImage');
    if (data.browser_image) {
      image.src = data.browser_image + '&preview=' + Date.now();
      image.classList.remove('hidden');
      $('browserEmpty').classList.add('hidden');
      $('browserOpenImage').href = data.browser_image;
      $('browserOpenImage').classList.remove('hidden');
    } else {
      image.removeAttribute('src');
      image.classList.add('hidden');
      $('browserEmpty').classList.remove('hidden');
      $('browserOpenImage').classList.add('hidden');
    }
  } catch (error) {
    $('browserPhase').textContent = 'Preview unavailable';
    $('browserStep').textContent = error.message;
  }
  browserTimer = setTimeout(() => updateBrowser(sessionToken, generation), 1000);
}
$('toggleBrowser').onclick = () => $('browserPanel').classList.contains('hidden') ? openBrowser(browserWorker) : closeBrowser();
$('closeBrowser').onclick = closeBrowser;
$('expandBrowser').onclick = () => {
  const expanded = $('workArea').classList.toggle('browser-expanded');
  $('workArea').parentElement.classList.toggle('browser-expanded', expanded);
  $('expandBrowser').textContent = expanded ? 'Split view' : 'Expand';
  $('expandBrowser').setAttribute('aria-pressed', String(expanded));
};
$('browserWorker').onchange = () => openBrowser($('browserWorker').value);
const savedSession = localStorage.getItem('birdhackbot.selectedSession');
if (!savedSession || !/^\/api\/v1\/(intake|assessments)\//.test(savedSession) || !(await navigate(savedSession))) {
  await openExistingSession();
}

async function poll() {
  const token = selection;
  if (current?.id === deletingSession) {
    setTimeout(poll, 1500);
    return;
  }
  if (isAssessmentView(current)) {
    try {
      const after = Math.max(0, ...eventRecords.keys());
      const view = await api('/api/v1/assessments/' + encodeURIComponent(current.id) + '?after=' + after);
      if (token === selection) renderView(view);
    } catch (error) { if (token === selection && current?.id !== deletingSession) showError(error); }
  } else if (current) {
    try {
      const view = await api('/api/v1/intake/' + encodeURIComponent(current.id));
      if (token === selection) renderView(view);
    } catch (error) { if (token === selection && current?.id !== deletingSession) showError(error); }
  }
  await refreshSidebar();
  setTimeout(poll, 1500);
}
setTimeout(poll, 1500);
