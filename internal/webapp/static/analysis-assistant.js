const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const assistantPath = data => data.kind === 'customer'
  ? '/api/v1/customers/' + encodeURIComponent(data.id) + '/analysis/assistant'
  : '/api/v1/assessments/' + encodeURIComponent(data.id) + '/analysis/assistant';

let view = {messages: [], profile: '', model: ''};
let models = [];
let element = null;
let analysis = null;
let busy = false;
let draft = '';
let focus = null;
let error = '';
let selectedProfile = '';

export function assistantSection() {
  return '<section class="section analysis-assistant" id="analysis-assistant" aria-label="Ask about these results"></section>';
}

export function mountAssistant(container, data) {
  element = container;
  analysis = data;
  renderAssistant();
}

export async function loadAssistant(data) {
  try {
    const [chatResponse, modelResponse] = await Promise.all([
      fetch(assistantPath(data)),
      fetch('/api/v1/models')
    ]);
    if (!chatResponse.ok) throw new Error((await chatResponse.json()).error || 'Could not load the conversation.');
    view = await chatResponse.json();
    selectedProfile = view.profile || '';
    if (modelResponse.ok) models = (await modelResponse.json()).models || [];
    error = '';
  } catch (cause) {
    error = cause.message;
  }
  renderAssistant();
}

export function askAboutFinding(finding) {
  focus = {id: finding.id, title: finding.title};
  draft = 'What does this finding mean, and what should we do next?';
  renderAssistant();
  element?.scrollIntoView({block:'start', behavior:'smooth'});
  element?.querySelector('textarea')?.focus();
}

function renderAssistant() {
  if (!element || !analysis) return;
  const projectView = analysis.kind === 'customer';
  const heading = projectView ? 'Ask about this project' : 'Ask about this assessment';
  const scope = projectView
    ? 'Answers can compare the saved sessions in this project.'
    : 'Answers cover this assessment only.';
  const starters = projectView
    ? ['What matters most here?', 'What changed between the runs?', 'Create an OWASP report as PDF']
    : ['What matters most in this assessment?', 'Which findings are confirmed?', 'Create an OWASP report as PDF'];
  const messages = view.messages || [];
  const transcript = messages.length ? '<div class="assistant-transcript" role="log" aria-label="Analysis conversation">' + messages.map(message => {
    const sources = message.sources?.length ? '<div class="assistant-sources"><span>From saved results</span>' + message.sources.map(source => {
      const href = String(source.url || '').startsWith('/') ? source.url : '#analysis-assistant';
      return '<a href="' + escapeHTML(href) + '">' + escapeHTML(source.label) + ' ↗</a>';
    }).join('') + '</div>' : '';
    const savedRefs = (message.sources || []).slice(0, 2).map(source => source.label + ' (' + source.url + ')').join('; ');
    const handoff = message.next_step && (projectView || message.session_id)
      ? '<button type="button" class="assistant-handoff" data-session="' + escapeHTML(message.session_id || '') + '" data-draft="' + escapeHTML(message.next_step) + '" data-summary="' + escapeHTML(message.text) + '" data-sources="' + escapeHTML(savedRefs) + '">' +
        (projectView ? 'Start a follow-up session ↗' : 'Continue this assessment ↗') + '</button>'
      : '';
    const report = message.report && String(message.report.url || '').startsWith('/api/v1/')
      ? '<div class="assistant-report"><a href="' + escapeHTML(message.report.url) + '" target="_blank" rel="noopener">Open ' + escapeHTML(message.report.output === 'pdf' ? 'PDF' : 'Markdown') + ' report ↗</a>' +
        (message.report.output === 'pdf' && String(message.report.markdown_url || '').startsWith('/api/v1/')
          ? '<a href="' + escapeHTML(message.report.markdown_url) + '" target="_blank" rel="noopener">Open editable Markdown ↗</a>' : '') + '</div>'
      : '';
    return '<article class="assistant-turn"><span class="assistant-speaker">' + (message.role === 'user' ? 'You' : 'Analysis guide') + '</span><p>' + escapeHTML(message.text) + '</p>' + sources + report + handoff + '</article>';
  }).join('') + '</div>' : '<div class="assistant-starters"><p>Ask what a result means or what would be useful to check next.</p><div>' +
    starters.map(question => '<button type="button" data-starter="' + escapeHTML(question) + '">' + escapeHTML(question) + '</button>').join('') + '</div></div>';
  const options = models.length ? models.map(item => '<option value="' + escapeHTML(item.id) + '"' + (item.id === selectedProfile ? ' selected' : '') + '>' + escapeHTML(item.label || item.id) + '</option>').join('') : '';
  element.innerHTML = '<div class="assistant-heading"><div><h2>' + heading + '</h2><p>' + scope + ' Ask for an OWASP or PTES report in Markdown or PDF, or a revised draft.</p></div>' +
    '<label class="assistant-model">Model ' + (options ? '<select id="assistant-model">' + options + '</select>' : '<span>' + escapeHTML(view.model || 'Loading…') + '</span>') + '</label></div>' +
    transcript + (busy ? '<p class="assistant-working" role="status"><span class="assistant-pulse"></span>Reading the saved results…</p>' : '') +
    (error ? '<p class="assistant-error" role="alert">' + escapeHTML(error) + '</p>' : '') +
    '<form id="assistant-form"><div class="assistant-compose">' + (focus ? '<div class="assistant-focus">About: ' + escapeHTML(focus.title) + ' <button type="button" id="assistant-clear-focus" aria-label="Clear selected finding">×</button></div>' : '') +
    '<textarea id="assistant-question" rows="2" maxlength="1500" placeholder="Ask a question about this analysis" aria-label="Question about the analysis">' + escapeHTML(draft) + '</textarea><div class="assistant-compose-bottom"><span>Answers link back to the saved results. No test runs from here.</span><button type="submit"' + (busy ? ' disabled' : '') + '>Ask ↗</button></div></div></form>';
  element.querySelectorAll('[data-starter]').forEach(button => button.onclick = () => {
    draft = button.dataset.starter;
    renderAssistant();
    element.querySelector('textarea')?.focus();
  });
  element.querySelector('#assistant-clear-focus')?.addEventListener('click', () => { focus = null; renderAssistant(); });
  element.querySelector('#assistant-model')?.addEventListener('change', event => { selectedProfile = event.target.value; });
  element.querySelector('#assistant-question').addEventListener('input', event => { draft = event.target.value; });
  element.querySelectorAll('.assistant-handoff').forEach(button => button.onclick = async () => {
    if (!projectView) {
      sessionStorage.setItem('birdhackbot.analysisHandoff', JSON.stringify({assessment: button.dataset.session, draft: button.dataset.draft}));
      location.href = '/?assessment=' + encodeURIComponent(button.dataset.session);
      return;
    }
    button.disabled = true;
    button.textContent = 'Creating follow-up session…';
    try {
      const createdResponse = await fetch('/api/v1/intake');
      const created = await createdResponse.json();
      if (!createdResponse.ok || !created.id) throw new Error(created.error || 'Could not create the follow-up session.');
      const assignedResponse = await fetch('/api/v1/intake/' + encodeURIComponent(created.id) + '/customer', {
        method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({customer: analysis.id})
      });
      if (!assignedResponse.ok) throw new Error((await assignedResponse.json()).error || 'Could not place the follow-up session in this project.');
      const followup = 'Follow up on the saved analysis for project ' + analysis.id + '.\n\n' +
        'Suggested next step: ' + button.dataset.draft + '\n\n' +
        'Analysis summary (review against saved evidence): ' + button.dataset.summary.slice(0, 700) + '\n\n' +
        'Saved references: ' + button.dataset.sources.slice(0, 500);
      sessionStorage.setItem('birdhackbot.analysisHandoff', JSON.stringify({intake: created.id, draft: followup}));
      location.href = '/?intake=' + encodeURIComponent(created.id);
    } catch (cause) {
      error = cause.message;
      renderAssistant();
    }
  });
  element.querySelector('#assistant-form').onsubmit = submitQuestion;
}

async function submitQuestion(event) {
  event.preventDefault();
  const question = draft.trim();
  if (!question || busy || !analysis) return;
  const profile = selectedProfile || element.querySelector('#assistant-model')?.value || view.profile || '';
  busy = true;
  error = '';
  renderAssistant();
  try {
    const response = await fetch(assistantPath(analysis), {
      method: 'POST', headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({question, profile, finding_id: focus?.id || ''})
    });
    const result = await response.json();
    if (!response.ok) throw new Error(result.error || 'The assistant could not answer.');
    view = result;
    selectedProfile = result.profile || profile;
    draft = '';
    focus = null;
  } catch (cause) {
    error = cause.message;
  } finally {
    busy = false;
    renderAssistant();
    element.querySelector('.assistant-transcript')?.scrollTo({top: 1000000});
  }
}
