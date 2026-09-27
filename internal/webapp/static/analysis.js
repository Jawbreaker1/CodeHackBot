const app = document.getElementById('app');
const params = new URLSearchParams(location.search);
let selectedScope = -1;
let selectedFinding = 0;

const displayStatus = value => String(value ?? '').replaceAll('_', ' ');
const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const compactText = (value, limit) => String(value ?? '').length > limit ? String(value).slice(0, limit).trimEnd() + '…' : String(value ?? '');
const list = (items, empty = 'None recorded') => items?.length
  ? '<ul>' + items.map(item => '<li>' + escapeHTML(item) + '</li>').join('') + '</ul>'
  : '<p>' + empty + '</p>';
const evidenceList = (items, sessionID) => items?.length
  ? '<ul>' + items.map(item => {
    const value = String(item);
    if (!sessionID || !value.startsWith('/')) return '<li>' + escapeHTML(value) + '</li>';
    const href = '/api/v1/assessments/' + encodeURIComponent(sessionID) + '/artifact?path=' + encodeURIComponent(value);
    return '<li><a href="' + escapeHTML(href) + '" target="_blank" rel="noopener">Open ' + escapeHTML(value.split('/').pop() || 'evidence') + ' ↗</a></li>';
  }).join('') + '</ul>' : '<p>None recorded</p>';
const metric = (value, label, tone = '') => '<div class="metric ' + tone + '"><strong>' + escapeHTML(value) + '</strong><span>' + escapeHTML(label) + '</span></div>';
const sessionURL = id => '/analysis?assessment=' + encodeURIComponent(id);

function findingCard(f) {
  const cve = f.cve_ids?.length ? '<div class="finding-meta"><span><strong>CVE references</strong> ' + escapeHTML(f.cve_ids.join(', ')) + '</span></div>' : '';
  const software = f.affected_software?.length ? '<div class="finding-meta"><span><strong>Affected software</strong> ' + escapeHTML(f.affected_software.join(', ')) + '</span></div>' : '';
  const refs = f.references?.length ? '<details><summary>Research references</summary>' + list(f.references) + '</details>' : '';
  const verification = f.verification
    ? '<details><summary>Independent challenge · ' + escapeHTML(f.verification.verdict) + '</summary><p><strong>Claim:</strong> ' + escapeHTML(f.verification.claim) + '</p><p><strong>Alternative:</strong> ' + escapeHTML(f.verification.alternative) + '</p><p><strong>Check:</strong> ' + escapeHTML(f.verification.alternative_result) + '</p><p><strong>Why:</strong> ' + escapeHTML(f.verification.reason) + '</p><strong>Execution logs</strong>' + evidenceList(f.verification.evidence, f.session_id) + '</details>'
    : (f.recorded_status ? '<p class="review-note">Earlier session called this reproduced; no structured challenge verdict was recorded. Review before treating it as confirmed.</p>' : '');
  const session = f.session_id ? '<a href="' + sessionURL(f.session_id) + '">Open session ↗</a>' : '';
  return '<article class="finding" data-priority="' + escapeHTML(f.priority) + '"><div class="finding-head"><h3>' + escapeHTML(f.title) + '</h3><span class="tag ' + escapeHTML(f.priority) + '">' + escapeHTML(f.priority) + '</span></div><p>' + escapeHTML(f.impact) + '</p><div class="finding-meta"><span><strong>Status</strong> ' + escapeHTML(f.status) + '</span><span><strong>Confidence</strong> ' + escapeHTML(f.confidence || 'not rated') + '</span><span><strong>Why here</strong> ' + escapeHTML(f.priority_reason) + '</span>' + (f.scope ? '<span><strong>Scope</strong> ' + escapeHTML(compactText(f.scope, 90)) + '</span>' : '') + '</div>' + cve + software + verification + '<details><summary>Evidence, reproduction, and remediation</summary><strong>Reproduction</strong>' + list(f.steps) + '<strong>Evidence</strong>' + evidenceList(f.evidence, f.session_id) + '<strong>Remediation</strong>' + list(f.remediation) + '</details>' + refs + (session ? '<div class="finding-session">' + session + '</div>' : '') + '</article>';
}

function findingExplorer(findings) {
  if (!findings.length) return '<p class="empty">No findings are recorded for this scope. This is not evidence that it is secure.</p>';
  if (selectedFinding >= findings.length) selectedFinding = 0;
  const rows = findings.map((finding, index) => '<button type="button" class="finding-select' + (selectedFinding === index ? ' selected' : '') + '" data-finding-index="' + index + '" aria-pressed="' + (selectedFinding === index) + '"><span class="finding-select-title">' + escapeHTML(finding.title) + '</span><span class="finding-select-meta"><span class="tag ' + escapeHTML(finding.priority) + '">' + escapeHTML(finding.priority) + '</span>' + escapeHTML(displayStatus(finding.status)) + ' · ' + escapeHTML(compactText(finding.scope || finding.session_id, 70)) + '</span></button>').join('');
  return '<div class="finding-explorer"><nav class="finding-picker" aria-label="Findings">' + rows + '</nav><div class="finding-inspector">' + findingCard(findings[selectedFinding]) + '</div></div>';
}

function challengeCard(challenge) {
  return '<article class="challenge" data-verdict="' + escapeHTML(challenge.verdict) + '">' +
    '<div class="challenge-head"><span class="test-state" data-state="' + escapeHTML(challenge.verdict) + '">' + escapeHTML(displayStatus(challenge.verdict)) + '</span><a href="' + sessionURL(challenge.session_id) + '">Open session ↗</a></div>' +
    '<h3>' + escapeHTML(challenge.claim) + '</h3><p>' + escapeHTML(challenge.reason || 'No rationale recorded.') + '</p>' +
    '<details><summary>Inspect competing explanation and evidence</summary><strong>Alternative explanation</strong><p>' + escapeHTML(challenge.alternative) + '</p><strong>What the check found</strong><p>' + escapeHTML(challenge.alternative_result) + '</p><strong>Execution logs</strong>' + evidenceList(challenge.evidence, challenge.session_id) + '</details></article>';
}

function coverageCard(entry, index) {
  const scopeTests = entry.tests || [];
  const weakPoints = entry.weak_points || [];
  const challenges = entry.challenges || [];
  const complete = scopeTests.filter(test => test.status === 'done').length;
  const tests = scopeTests.map(test => '<li><span class="test-state" data-state="' + escapeHTML(test.status) + '">' + escapeHTML(displayStatus(test.status)) + '</span> ' + escapeHTML(test.goal || test.task_id) + ' <a href="' + sessionURL(test.session_id) + '">session ↗</a></li>').join('');
  const weaknesses = weakPoints.map(point => '<li><span class="test-state" data-state="' + escapeHTML(point.status) + '">' + escapeHTML(point.status) + '</span> ' + escapeHTML(point.title) + '</li>').join('');
  const points = weakPoints.slice(0, 2).map(point => '<span class="weak-point" data-status="' + escapeHTML(point.status) + '">' + escapeHTML(point.title) + '</span>').join('') +
    challenges.filter(challenge => challenge.verdict !== 'supported').slice(0, 1).map(challenge => '<span class="weak-point" data-status="challenge">' + escapeHTML(displayStatus(challenge.verdict)) + ' challenge</span>').join('');
  const trail = scopeTests.slice(0, 2).map(test => '<span class="scope-test"><i data-state="' + escapeHTML(test.status) + '"></i>' + escapeHTML(compactText(test.goal || test.task_id, 88)) + '</span>').join('');
  return '<article class="scope-card' + (selectedScope === index ? ' selected' : '') + '">' +
    '<button type="button" class="scope-select" data-scope-index="' + index + '" aria-pressed="' + (selectedScope === index) + '">' +
    '<span class="scope-label">Declared scope</span><strong>' + escapeHTML(compactText(entry.scope || 'Scope not recorded', 110)) + '</strong>' +
    '<span class="scope-counts">' + (entry.session_ids || []).length + ' session(s) · ' + complete + '/' + scopeTests.length + ' tests completed · ' + weakPoints.length + ' finding(s) · ' + challenges.length + ' challenge(s)</span></button>' +
    '<div class="scope-trace">' + (trail || '<span class="scope-empty">No worker test recorded</span>') + '</div>' +
    (points ? '<div class="scope-points">' + points + '</div>' : '') + '<details><summary>Inspect test coverage and gaps</summary>' +
    '<strong>Full declared scope</strong><p>' + escapeHTML(entry.scope || 'Not recorded') + '</p>' +
    '<strong>Recorded worker tests</strong>' + (tests ? '<ul>' + tests + '</ul>' : '<p>No worker test recorded.</p>') +
    '<strong>Findings</strong>' + (weaknesses ? '<ul>' + weaknesses + '</ul>' : '<p>No finding recorded.</p>') +
    '<strong>Independent challenges</strong>' + (challenges.length ? '<ul>' + challenges.map(challenge => '<li>' + escapeHTML(displayStatus(challenge.verdict)) + ': ' + escapeHTML(challenge.claim) + ' <a href="' + sessionURL(challenge.session_id) + '">session ↗</a></li>').join('') + '</ul>' : '<p>No challenge recorded.</p>') +
    '<strong>Unresolved gaps</strong>' + list(entry.gaps) + '</details></article>';
}

function coverageSection(data) {
  const entries = data.coverage || [];
  if (!entries.length) return '';
  const root = data.kind === 'customer' ? 'Customer project' : 'Assessment scope and tests';
  return '<section class="section exploration"><div class="section-heading"><div><h2>Coverage map</h2><p>Declared scopes, recorded worker tests, and reported weak points. Connections and unobserved components are not inferred.</p></div>' + (selectedScope >= 0 ? '<button type="button" class="clear-filter" id="clear-scope">Show all scopes</button>' : '') + '</div><div class="coverage-map"><div class="map-root">' + escapeHTML(root) + '</div><div class="scope-grid">' + entries.map(coverageCard).join('') + '</div></div></section>';
}

function correlationSection(data, visibleSessions) {
  if (data.kind !== 'customer') return '';
  const shared = (data.coverage || []).filter(entry => (entry.session_ids || []).length > 1 && (selectedScope < 0 || visibleSessions.includes(entry.session_ids[0])));
  const sharedCards = shared.map(entry => '<article class="correlation shared"><div><strong>' + escapeHTML(compactText(entry.scope || 'Scope not recorded', 100)) + '</strong><span>' + entry.session_ids.length + ' sessions</span></div><p>' + (entry.tests || []).length + ' recorded worker tests · ' + (entry.weak_points || []).length + ' findings · ' + (entry.challenges || []).length + ' independent challenges. Review changes in coverage and verdicts across these sessions.</p><div class="correlation-links">' + entry.session_ids.map(id => '<a href="' + sessionURL(id) + '">' + escapeHTML(id) + ' ↗</a>').join('') + '</div></article>').join('');
  const groups = (data.correlations || []).filter(group => selectedScope < 0 || group.matches.some(match => visibleSessions.includes(match.session_id)));
  const cards = groups.map(group => '<article class="correlation"><div><strong>' + escapeHTML(group.label) + '</strong><span>' + group.matches.length + ' recorded finding(s)</span></div><p>' + escapeHTML(group.basis) + '</p><ul>' + group.matches.map(match => '<li><a href="' + sessionURL(match.session_id) + '">' + escapeHTML(match.title) + ' ↗</a> · ' + escapeHTML(match.status) + '</li>').join('') + '</ul></article>').join('');
  return '<section class="section exploration"><div class="section-heading"><div><h2>Cross-session signals</h2><p>Shared declared scope, CVE reference, or recorded software label. Overlap is a review lead, not a proven attack chain.</p></div></div>' + (sharedCards || cards ? '<div class="correlation-grid">' + sharedCards + cards + '</div>' : '<p class="empty-inline">No cross-session overlap is recorded yet.</p>') + '</section>';
}

function sessionComparisonSection(data) {
  if (data.kind !== 'customer' || !(data.sessions || []).length) return '';
  const rows = data.sessions.map(session => '<tr><th scope="row"><a href="' + sessionURL(session.id) + '">' + escapeHTML(compactText(session.goal || session.id, 115)) + ' ↗</a><small>' + escapeHTML(session.id) + '</small></th><td>' + escapeHTML(displayStatus(session.status)) + '</td><td>' + escapeHTML(compactText(session.scope || 'Not recorded', 90)) + '</td><td>' + (session.tests || 0) + '</td><td>' + (session.findings || 0) + '</td><td>' + (session.challenges || 0) + '</td><td>' + (session.gaps || 0) + '</td></tr>').join('');
  return '<section class="section exploration"><div class="section-heading"><div><h2>Session comparison</h2><p>Every assessment in this customer project remains separate. Open a row to inspect its plan, evidence, and report.</p></div></div><div class="session-matrix"><table><thead><tr><th>Session</th><th>Status</th><th>Declared scope</th><th>Tests</th><th>Findings</th><th>Challenges</th><th>Gaps</th></tr></thead><tbody>' + rows + '</tbody></table></div></section>';
}

function challengeSection(data, visibleSessions) {
  const challenges = (data.challenges || []).filter(challenge => !visibleSessions || visibleSessions.includes(challenge.session_id));
  if (!challenges.length) return '';
  const open = challenges.filter(challenge => challenge.verdict !== 'supported');
  return '<section class="section exploration"><div class="section-heading"><div><h2>Independent challenges</h2><p>' + open.length + ' unresolved or refuted claim(s). Each check tests a plausible competing explanation before a finding can be called reproduced.</p></div></div><div class="challenge-grid">' + challenges.map(challengeCard).join('') + '</div></section>';
}

function filteredRisk(findings) {
  const risk = {critical:0,high:0,medium:0,low:0,reproduced:0,candidates:0};
  for (const finding of findings) {
    if (finding.status !== 'reproduced') { risk.candidates++; continue; }
    risk.reproduced++;
    if (Object.hasOwn(risk, finding.severity)) risk[finding.severity]++;
  }
  return risk;
}

function render(data) {
  const coverage = data.coverage || [];
  if (selectedScope >= coverage.length) selectedScope = -1;
  const visibleSessions = selectedScope < 0 ? null : coverage[selectedScope].session_ids;
  const findings = (data.findings || []).filter(finding => !visibleSessions || visibleSessions.includes(finding.session_id));
  const risk = visibleSessions ? filteredRisk(findings) : (data.risk || {});
  const title = data.kind === 'customer' ? 'Customer analysis' : 'Assessment analysis';
  const goal = data.goal ? '<p class="goal-description">' + escapeHTML(compactText(data.goal, 185)) + '</p>' : '';
  const gaps = data.gaps || [];
  const hasLegacyClaims = (data.findings || []).some(finding => finding.recorded_status);
  const conclusionDetail = data.conclusion_detail && data.conclusion_detail !== data.conclusion ? '<details><summary>Full coordinator notes</summary><p>' + escapeHTML(data.conclusion_detail) + '</p></details>' : '';
  const conclusion = data.conclusion ? '<section class="section"><h2>' + (hasLegacyClaims ? 'Coordinator conclusion · review required' : 'Coordinator conclusion') + '</h2>' + (hasLegacyClaims ? '<p class="review-note">This analysis includes earlier reproduction claims without independent challenge records. Those claims remain unconfirmed here.</p>' : '') + '<p>' + escapeHTML(data.conclusion) + '</p>' + conclusionDetail + '</section>' : '';
  const latestResult = data.review_pending ? '<section class="section"><h2>Latest worker result · awaiting review</h2><p>' + escapeHTML(data.latest_result || 'No summary recorded.') + '</p></section>' : '';
  const gapItems = items => items.map(item => '<li>' + escapeHTML(item) + '</li>').join('');
  const gapList = '<ul class="gap-list">' + gapItems(gaps.slice(0, 4)) + '</ul>' + (gaps.length > 4 ? '<details><summary>Show ' + (gaps.length - 4) + ' more gaps</summary><ul class="gap-list">' + gapItems(gaps.slice(4)) + '</ul></details>' : '');
  const findingHeading = selectedScope < 0 ? 'Prioritized findings' : 'Findings for selected scope';
  app.innerHTML =
    '<header class="analysis-head"><div><div class="eyebrow">' + escapeHTML(data.kind === 'customer' ? 'Unified customer view' : 'Evidence review') + '</div><h1>' + escapeHTML(title) + '</h1>' + goal + '<p>' + escapeHTML(data.summary || '') + '</p>' + (data.report_url ? '<p><a class="report-link" href="' + escapeHTML(data.report_url) + '" target="_blank" rel="noopener">Open formal Markdown report ↗</a></p>' : '') + '</div><span class="status" data-state="' + escapeHTML(data.status) + '">' + escapeHTML(displayStatus(data.status)) + '</span></header>' +
    '<section class="summary-grid">' + metric(risk.critical || 0,'Critical','critical') + metric(risk.high || 0,'High','high') + metric(risk.medium || 0,'Medium','medium') + metric(risk.low || 0,'Low','low') + metric(risk.reproduced || 0,'Reproduced') + metric(risk.candidates || 0,'Candidates') + '</section><p class="metric-note">Severity totals count reproduced findings; candidates remain separate.</p>' +
    coverageSection(data) + sessionComparisonSection(data) + challengeSection(data, visibleSessions) + correlationSection(data, visibleSessions || []) +
    '<section class="section finding-workspace"><div class="section-heading"><div><h2>' + findingHeading + '</h2><p>Select a finding to inspect its claim, independent check, evidence, and remediation.</p></div><span class="workspace-count">' + findings.length + ' recorded</span></div>' + findingExplorer(findings) + '</section>' +
    '<div class="columns"><div>' + conclusion + latestResult + '</div><aside><section class="side-card"><h2>Next actions</h2><ul class="action-list">' + (data.next_actions || []).map(item => '<li>' + escapeHTML(item) + '</li>').join('') + '</ul></section><section class="side-card"><h2>' + (data.review_pending ? 'Gaps from last plan' : 'Assessment gaps') + ' · ' + gaps.length + '</h2>' + gapList + '</section><section class="side-card"><h2>Evidence boundary</h2><p>Map nodes are declared scopes, not inferred systems. Cross-session signals show recorded overlaps only. Reproduced findings have a worker challenge verdict and cited execution log, but every model-authored claim still needs professional review.</p></section></aside></div>';
  app.querySelectorAll('[data-scope-index]').forEach(button => button.addEventListener('click', () => {
    const index = Number(button.dataset.scopeIndex);
    selectedScope = selectedScope === index ? -1 : index;
    render(data);
  }));
  app.querySelector('#clear-scope')?.addEventListener('click', () => { selectedScope = -1; render(data); });
  app.querySelectorAll('[data-finding-index]').forEach(button => button.addEventListener('click', () => {
    selectedFinding = Number(button.dataset.findingIndex);
    render(data);
  }));
}

async function load() {
  const assessment = params.get('assessment');
  const customer = params.get('customer');
  if (!assessment && !customer) { app.innerHTML = '<p class="error">Choose an assessment or customer from the coordinator first.</p>'; return; }
  const path = assessment ? '/api/v1/assessments/' + encodeURIComponent(assessment) + '/analysis' : '/api/v1/customers/' + encodeURIComponent(customer) + '/analysis';
  try {
    const response = await fetch(path);
    if (!response.ok) throw new Error((await response.json()).error || response.statusText);
    render(await response.json());
  } catch (error) {
    app.innerHTML = '<p class="error">' + escapeHTML(error.message) + '</p>';
  }
}
load();
