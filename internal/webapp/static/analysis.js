const app = document.getElementById('app');
const params = new URLSearchParams(location.search);
const escapeHTML = value => String(value ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const list = (items, empty = 'None recorded') => items?.length ? '<ul>' + items.map(item => '<li>' + escapeHTML(item) + '</li>').join('') + '</ul>' : '<p>' + empty + '</p>';
const metric = (value, label, tone = '') => '<div class="metric ' + tone + '"><strong>' + escapeHTML(value) + '</strong><span>' + escapeHTML(label) + '</span></div>';
function findingCard(f) {
  const cve = f.cve_ids?.length ? '<div class="finding-meta"><span><strong>CVE references</strong> ' + escapeHTML(f.cve_ids.join(', ')) + '</span></div>' : '';
  const software = f.affected_software?.length ? '<div class="finding-meta"><span><strong>Affected software</strong> ' + escapeHTML(f.affected_software.join(', ')) + '</span></div>' : '';
  const refs = f.references?.length ? '<details><summary>Research references</summary>' + list(f.references) + '</details>' : '';
  return '<article class="finding" data-priority="' + escapeHTML(f.priority) + '"><div class="finding-head"><h3>' + escapeHTML(f.title) + '</h3><span class="tag ' + escapeHTML(f.priority) + '">' + escapeHTML(f.priority) + '</span></div><p>' + escapeHTML(f.impact) + '</p><div class="finding-meta"><span><strong>Status</strong> ' + escapeHTML(f.status) + '</span><span><strong>Confidence</strong> ' + escapeHTML(f.confidence || 'not rated') + '</span><span><strong>Why here</strong> ' + escapeHTML(f.priority_reason) + '</span>' + (f.session_id ? '<span><strong>Session</strong> ' + escapeHTML(f.session_id) + '</span>' : '') + '</div>' + cve + software + '<details><summary>Evidence, reproduction, and remediation</summary><strong>Reproduction</strong>' + list(f.steps) + '<strong>Evidence</strong>' + list(f.evidence) + '<strong>Remediation</strong>' + list(f.remediation) + '</details>' + refs + '</article>';
}
function render(data) {
  const r = data.risk || {};
  const title = data.kind === 'customer' ? 'Customer analysis' : (data.goal || 'Assessment analysis');
  const findings = data.findings || [];
  const sessions = data.sessions || [];
  const gaps = data.gaps || [];
  const conclusionDetail = data.conclusion_detail && data.conclusion_detail !== data.conclusion ? '<details><summary>Full coordinator notes</summary><p>' + escapeHTML(data.conclusion_detail) + '</p></details>' : '';
  const conclusion = data.conclusion ? '<section class="section"><h2>Coordinator conclusion</h2><p>' + escapeHTML(data.conclusion) + '</p>' + conclusionDetail + '</section>' : '';
  const gapItems = items => items.map(item => '<li>' + escapeHTML(item) + '</li>').join('');
  const gapList = '<ul class="gap-list">' + gapItems(gaps.slice(0, 4)) + '</ul>' + (gaps.length > 4 ? '<details><summary>Show ' + (gaps.length - 4) + ' more gaps</summary><ul class="gap-list">' + gapItems(gaps.slice(4)) + '</ul></details>' : '');
  app.innerHTML = '<header class="analysis-head"><div><div class="eyebrow">' + escapeHTML(data.kind === 'customer' ? 'Unified customer view' : 'Evidence review') + '</div><h1>' + escapeHTML(title) + '</h1><p>' + escapeHTML(data.summary || '') + '</p>' + (data.report_url ? '<p><a class="report-link" href="' + escapeHTML(data.report_url) + '" target="_blank" rel="noopener">Open formal Markdown report ↗</a></p>' : '') + '</div><span class="status" data-state="' + escapeHTML(data.status) + '">' + escapeHTML(data.status) + '</span></header><section class="summary-grid">' + metric(r.critical || 0,'Critical','critical') + metric(r.high || 0,'High','high') + metric(r.medium || 0,'Medium','medium') + metric(r.low || 0,'Low','low') + metric(r.reproduced || 0,'Validated') + metric(r.candidates || 0,'Candidates') + '</section><div class="columns"><div>' + conclusion + '<section class="section"><h2>Prioritized findings</h2>' + (findings.length ? findings.map(findingCard).join('') : '<p class="empty">No findings are recorded yet. This is not evidence that the target is secure.</p>') + '</section></div><aside><section class="side-card"><h2>Next actions</h2><ul class="action-list">' + (data.next_actions || []).map(item => '<li>' + escapeHTML(item) + '</li>').join('') + '</ul></section><section class="side-card"><h2>Assessment gaps · ' + gaps.length + '</h2>' + gapList + '</section>' + (sessions.length ? '<section class="side-card"><h2>Sessions · ' + sessions.length + '</h2><div class="session-list">' + sessions.map(s => '<a href="/analysis?assessment=' + encodeURIComponent(s.id) + '">' + escapeHTML(s.goal || s.id) + '<small>' + escapeHTML(s.status) + ' · ' + escapeHTML(s.id) + '</small></a>').join('') + '</div></section>' : '') + '<section class="side-card"><h2>Evidence boundary</h2><p>Priorities are derived from reported severity, confidence, and validation status. Findings remain model-authored drafts; review the linked evidence before taking action.</p></section></aside></div>';
}
async function load() {
  const assessment = params.get('assessment');
  const customer = params.get('customer');
  if (!assessment && !customer) { app.innerHTML = '<p class="error">Choose an assessment or customer from the coordinator first.</p>'; return; }
  const path = assessment ? '/api/v1/assessments/' + encodeURIComponent(assessment) + '/analysis' : '/api/v1/customers/' + encodeURIComponent(customer) + '/analysis';
  try { const response = await fetch(path); if (!response.ok) throw new Error((await response.json()).error || response.statusText); render(await response.json()); }
  catch (error) { app.innerHTML = '<p class="error">' + escapeHTML(error.message) + '</p>'; }
}
load();
