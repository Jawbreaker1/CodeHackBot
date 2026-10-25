import {webPagesSection, bindWebPages} from './web-map.js';
import {codeReviewSection} from './code-review.js';
import {assistantSection, mountAssistant, loadAssistant, askAboutFinding} from './analysis-assistant.js';

const app = document.getElementById('app');
const params = new URLSearchParams(location.search);
let selectedScope = -1;
let selectedFinding = 0;
let selectedRisk = 'all';
let selectedCode = 0;
let codeOpen = false;

const displayStatus = value => String(value ?? '').replaceAll('_', ' ');
const isConfirmed = finding => finding.status === 'reproduced' || finding.status === 'confirmed_exposure';
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
const metric = (value, label, risk) => '<button type="button" class="metric ' + risk + (selectedRisk === risk ? ' selected' : '') + '" data-risk="' + risk + '" aria-pressed="' + (selectedRisk === risk) + '" aria-label="Show ' + escapeHTML(label) + (risk === 'candidates' ? ' observations' : ' findings') + '"' + (Number(value) ? '' : ' disabled') + '><strong>' + escapeHTML(value) + '</strong><span>' + escapeHTML(label) + (Number(value) ? ' <b>↗</b>' : '') + '</span></button>';
const sessionURL = id => '/analysis?assessment=' + encodeURIComponent(id);

function findingCard(f) {
  const cve = f.cve_ids?.length ? '<div class="finding-meta"><span><strong>CVE references</strong> ' + escapeHTML(f.cve_ids.join(', ')) + '</span></div>' : '';
  const software = f.affected_software?.length ? '<div class="finding-meta"><span><strong>Affected software</strong> ' + escapeHTML(f.affected_software.join(', ')) + '</span></div>' : '';
  const refs = f.references?.length ? '<details><summary>Research references</summary>' + list(f.references) + '</details>' : '';
  const retest = f.latest_review ? '<div class="finding-answer"><strong>Later retest: ' + escapeHTML(f.latest_review.verdict.replaceAll('_', ' ')) + '</strong><p>' + escapeHTML(f.latest_review.reason) + ' <a href="' + sessionURL(f.latest_review.session_id) + '">Open retest ↗</a></p><details><summary>Retest evidence</summary>' + evidenceList(f.latest_review.evidence, f.latest_review.session_id) + '</details></div>' : '';
  const verification = f.verification
    ? '<details><summary>' + (isConfirmed(f) ? 'Independent challenge' : 'Observation check') + ' · ' + escapeHTML(f.verification.verdict) + '</summary>' + (isConfirmed(f) ? '' : '<p>This verdict covers the checked observation. It does not establish its wider security impact.</p>') + '<p><strong>Claim:</strong> ' + escapeHTML(f.verification.claim) + '</p><p><strong>Alternative:</strong> ' + escapeHTML(f.verification.alternative) + '</p><p><strong>Check:</strong> ' + escapeHTML(f.verification.alternative_result) + '</p><p><strong>Why:</strong> ' + escapeHTML(f.verification.reason) + '</p><strong>Execution logs</strong>' + evidenceList(f.verification.evidence, f.session_id) + '</details>'
    : (f.recorded_status ? '<p class="review-note">An earlier confirmation claim lacks a supported challenge or exposure record. Review before treating it as confirmed.</p>' : '');
  const session = f.session_id ? '<a href="' + sessionURL(f.session_id) + '">Open session ↗</a>' : '';
  const action = f.remediation?.length ? escapeHTML(f.remediation[0]) : 'A specific fix has not been recorded yet; review the evidence before assigning work.';
  const confirmed = isConfirmed(f);
  const actionText = confirmed ? '<p>' + action + '</p>' : '<p>Confirm this observation on the current target before assigning a fix.</p><p><strong>If confirmed</strong><br>' + action + '</p>';
  const label = confirmed ? f.priority : 'Needs confirmation';
  return '<article class="finding" data-priority="' + escapeHTML(f.priority) + '"><div class="finding-head"><h3>' + escapeHTML(f.title) + '</h3><span class="tag ' + escapeHTML(f.priority) + '">' + escapeHTML(f.latest_review?.verdict === 'remediated' ? 'Retested · remediated' : label) + '</span></div><div class="finding-answer"><strong>' + (confirmed ? 'Why it matters' : 'Possible impact') + '</strong><p>' + escapeHTML(f.impact || 'Impact has not been recorded yet.') + '</p></div><div class="finding-answer"><strong>What to do</strong>' + (f.latest_review?.verdict === 'remediated' ? '<p>Review the linked retest and keep the control in place.</p>' : actionText) + '</div><div class="finding-meta"><span><strong>Security effect</strong> ' + (confirmed ? (f.status === 'confirmed_exposure' ? 'affected deployment confirmed; exploit not tested' : 'reproduced') : 'not established') + '</span>' + (confirmed ? '<span><strong>Finding confidence</strong> ' + escapeHTML(f.confidence || 'not rated') + '</span>' : '') + '</div><button type="button" class="finding-ask" data-ask-finding="' + escapeHTML(f.id) + '">Ask about this ' + (confirmed ? 'finding' : 'observation') + ' ↗</button>' + retest + cve + software + (f.advisory_assessment_id ? '<div class="finding-meta"><span><strong>Advisory assessment</strong> ' + escapeHTML(f.advisory_id ? f.advisory_id + ' · ' + f.advisory_assessment_id : f.advisory_assessment_id) + '</span></div>' : '') + verification + '<details><summary>See how this was tested and what may need fixing</summary>' + (f.scope ? '<strong>Scope</strong><p>' + escapeHTML(f.scope) + '</p>' : '') + '<strong>Why this priority</strong><p>' + escapeHTML(f.priority_reason) + '</p><strong>Recorded checks</strong>' + list(f.steps) + '<strong>Evidence</strong>' + evidenceList(f.evidence, f.session_id) + '<strong>Proposed correction</strong>' + list(f.remediation) + '</details>' + refs + (session ? '<div class="finding-session">' + session + '</div>' : '') + '</article>';
}

function findingExplorer(findings) {
  if (!findings.length) return '<p class="empty">No findings or observations match this view. Check the test coverage before drawing conclusions.</p>';
  if (selectedFinding >= findings.length) selectedFinding = 0;
  const rows = findings.map((finding, index) => '<button type="button" class="finding-select' + (selectedFinding === index ? ' selected' : '') + '" data-finding-index="' + index + '" aria-pressed="' + (selectedFinding === index) + '"><span class="finding-select-title">' + escapeHTML(finding.title) + '</span><span class="finding-select-meta"><span class="tag ' + escapeHTML(finding.priority) + '">' + escapeHTML(finding.latest_review?.verdict === 'remediated' ? 'Retested · remediated' : isConfirmed(finding) ? finding.priority : 'Needs confirmation') + '</span>' + escapeHTML(compactText(finding.affected_software?.join(', ') || finding.session_id, 70)) + '</span></button>').join('');
  return '<div class="finding-explorer"><nav class="finding-picker" aria-label="Findings and observations">' + rows + '</nav><div class="finding-inspector">' + findingCard(findings[selectedFinding]) + '</div></div>';
}

function challengeCard(challenge) {
  return '<article class="challenge" data-verdict="' + escapeHTML(challenge.verdict) + '">' +
    '<div class="challenge-head"><span class="test-state" data-state="' + escapeHTML(challenge.verdict) + '">Needs more evidence</span><a href="' + sessionURL(challenge.session_id) + '">Open session ↗</a></div>' +
    '<h3>' + escapeHTML(challenge.claim) + '</h3><p>' + escapeHTML(challenge.reason || 'No rationale recorded.') + '</p>' +
    '<details><summary>Inspect competing explanation and evidence</summary><strong>Alternative explanation</strong><p>' + escapeHTML(challenge.alternative) + '</p><strong>What the check found</strong><p>' + escapeHTML(challenge.alternative_result) + '</p><strong>Execution logs</strong>' + evidenceList(challenge.evidence, challenge.session_id) + '</details></article>';
}

function ruledOutCheck(challenge) {
  return '<details class="ruled-out-check"><summary><span class="test-state">Ruled out · </span>' + escapeHTML(challenge.claim) + '</summary>' +
    '<p><strong>Why it was ruled out:</strong> ' + escapeHTML(challenge.reason || 'No rationale recorded.') + '</p>' +
    '<p><strong>What the check found:</strong> ' + escapeHTML(challenge.alternative_result) + '</p>' +
    '<div>Recorded evidence' + evidenceList(challenge.evidence, challenge.session_id) + '</div>' +
    '<a href="' + sessionURL(challenge.session_id) + '">Open session ↗</a></details>';
}

function coverageCard(entry, index) {
  const scopeTests = entry.tests || [];
  const weakPoints = entry.weak_points || [];
  const challenges = entry.challenges || [];
  const complete = scopeTests.filter(test => test.status === 'done').length;
  const tests = scopeTests.map(test => '<li><span class="test-state" data-state="' + escapeHTML(test.status) + '">' + escapeHTML(displayStatus(test.status)) + '</span> ' + escapeHTML(test.goal || test.task_id) + ' <a href="' + sessionURL(test.session_id) + '">session ↗</a></li>').join('');
  const weaknesses = weakPoints.map(point => '<li><span class="test-state" data-state="' + escapeHTML(point.status) + '">' + escapeHTML(point.status) + '</span> ' + escapeHTML(point.title) + '</li>').join('');
  const points = weakPoints.slice(0, 2).map(point => '<span class="weak-point" data-status="' + escapeHTML(point.status) + '">' + escapeHTML(point.title) + '</span>').join('');
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
    '<strong>Checks of suspected issues</strong><p>' + challenges.filter(challenge => challenge.verdict === 'inconclusive').length + ' need more evidence · ' + challenges.filter(challenge => challenge.verdict === 'refuted').length + ' ruled out. Supported checks are attached to their findings.</p>' +
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
  const groups = (data.correlations || []).filter(group => !group.label.startsWith('Same target:') && (selectedScope < 0 || group.matches.some(match => visibleSessions.includes(match.session_id))));
  const cards = groups.map(group => '<article class="correlation"><div><strong>' + escapeHTML(group.label) + '</strong><span>' + group.matches.length + ' recorded finding(s)</span></div><p>' + escapeHTML(group.basis) + '</p><ul>' + group.matches.map(match => '<li><a href="' + sessionURL(match.session_id) + '">' + escapeHTML(match.title) + ' ↗</a> · ' + escapeHTML(match.status) + '</li>').join('') + '</ul></article>').join('');
  if (!sharedCards && !cards) return '';
  return '<section class="section exploration"><div class="section-heading"><div><h2>Cross-session signals</h2><p>Shared declared scope, CVE, software label, or an explicit follow-up link. Overlap is a review lead, not a proven shared weakness.</p></div></div><div class="correlation-grid">' + sharedCards + cards + '</div></section>';
}

function targetHistorySection(data) {
  if (data.kind !== 'customer') return '';
  const groups = new Map();
  for (const session of data.sessions || []) {
    const key = session.target || 'unknown:' + session.id;
    if (!groups.has(key)) groups.set(key, {label: session.target || 'Target not identified', sessions: []});
    groups.get(key).sessions.push(session);
  }
  const cards = [...groups.values()].map(({label, sessions}) => {
    sessions.sort((a, b) => String(a.updated_at).localeCompare(String(b.updated_at)));
    const rows = sessions.map(session => {
      const findings = (data.findings || []).filter(finding => finding.session_id === session.id);
      const reviews = (data.prior_reviews || []).filter(review => review.session_id === session.id);
      const date = session.updated_at && !session.updated_at.startsWith('0001-') ? session.updated_at.slice(0, 10) : 'Date not recorded';
      const detail = findings.map(finding => '<li>' + escapeHTML(finding.title) + ' · ' + escapeHTML(finding.status) + '</li>').join('') +
        reviews.map(review => '<li>Retest: ' + escapeHTML(review.verdict.replaceAll('_', ' ')) + ' · ' + escapeHTML(review.reason) + '</li>').join('');
      return '<li><a href="' + sessionURL(session.id) + '">' + escapeHTML(date) + ' · ' + escapeHTML(compactText(session.goal || session.id, 115)) + ' ↗</a>' +
        '<span> · ' + Number(session.tests || 0) + ' tests · ' + findings.length + ' finding records</span>' +
        (detail ? '<details><summary>What this run recorded</summary><ul>' + detail + '</ul></details>' : '') + '</li>';
    }).join('');
    return '<article class="correlation"><div><strong>' + escapeHTML(label) + '</strong><span>' + sessions.length + ' session(s)</span></div><ol class="target-history">' + rows + '</ol></article>';
  }).join('');
  return '<section class="section exploration"><div class="section-heading"><div><h2>Target history</h2><p>Runs are shown in time order with their own evidence. A later run that does not mention an older finding has not proved it was fixed.</p></div></div><div class="correlation-grid">' + cards + '</div></section>';
}

function sessionComparisonSection(data) {
  if (data.kind !== 'customer' || !(data.sessions || []).length) return '';
  const rows = data.sessions.map(session => '<tr><th scope="row"><a href="' + sessionURL(session.id) + '">' + escapeHTML(compactText(session.goal || session.id, 115)) + ' ↗</a><small>' + escapeHTML(session.id) + '</small></th><td>' + escapeHTML(displayStatus(session.status)) + '</td><td>' + escapeHTML(compactText(session.scope || 'Not recorded', 90)) + '</td><td>' + (session.tests || 0) + '</td><td>' + (session.findings || 0) + '</td><td>' + (session.challenges || 0) + '</td><td>' + (session.gaps || 0) + '</td></tr>').join('');
  return '<section class="section exploration"><div class="section-heading"><div><h2>Session comparison</h2><p>Every assessment in this customer project remains separate. Open a row to inspect its plan, evidence, and report.</p></div></div><div class="session-matrix"><table><thead><tr><th>Session</th><th>Status</th><th>Declared scope</th><th>Tests</th><th>Findings</th><th>Challenges</th><th>Gaps</th></tr></thead><tbody>' + rows + '</tbody></table></div></section>';
}

function challengeSection(data, visibleSessions) {
  const challenges = (data.challenges || []).filter(challenge => !visibleSessions || visibleSessions.includes(challenge.session_id));
  const unresolved = challenges.filter(challenge => challenge.verdict === 'inconclusive');
  const ruledOut = challenges.filter(challenge => challenge.verdict === 'refuted');
  if (!unresolved.length && !ruledOut.length) return '';
  return '<section class="section exploration" id="verification-history"><div class="section-heading"><div><h2>Checks that did not become findings</h2><p>Unresolved questions may need another test. Ruled-out concerns are recorded only as test history; they are not vulnerabilities to fix.</p></div></div>' +
    (unresolved.length ? '<h3>Needs more evidence · ' + unresolved.length + '</h3><div class="challenge-grid">' + unresolved.map(challengeCard).join('') + '</div>' : '') +
    (ruledOut.length ? '<details class="ruled-out-history"><summary>Ruled-out concerns · ' + ruledOut.length + '</summary><div class="ruled-out-list">' + ruledOut.map(ruledOutCheck).join('') + '</div></details>' : '') + '</section>';
}

function filteredRisk(findings) {
  const risk = {critical:0,high:0,medium:0,low:0,reproduced:0,confirmed_exposures:0,candidates:0,remediated:0};
  for (const finding of findings) {
    if (finding.latest_review?.verdict === 'remediated') { risk.remediated++; continue; }
    if (!isConfirmed(finding)) { risk.candidates++; continue; }
    if (finding.status === 'confirmed_exposure') risk.confirmed_exposures++;
    else risk.reproduced++;
    if (Object.hasOwn(risk, finding.severity)) risk[finding.severity]++;
  }
  return risk;
}

function render(data) {
  const coverage = data.coverage || [];
  if (selectedScope >= coverage.length) selectedScope = -1;
  const visibleSessions = selectedScope < 0 ? null : coverage[selectedScope].session_ids;
  const allFindings = (data.findings || []).filter(finding => !visibleSessions || visibleSessions.includes(finding.session_id));
  const findings = allFindings.filter(finding => selectedRisk === 'all' || (finding.latest_review?.verdict !== 'remediated' && (selectedRisk === 'candidates' ? !isConfirmed(finding) : selectedRisk === 'reproduced' ? finding.status === 'reproduced' : selectedRisk === 'confirmed_exposures' ? finding.status === 'confirmed_exposure' : isConfirmed(finding) && finding.severity === selectedRisk)));
  const risk = visibleSessions ? filteredRisk(allFindings) : (data.risk || {});
  const title = data.kind === 'customer' ? 'Customer security analysis' : 'Security analysis';
  const goal = data.goal ? '<p class="goal-description">' + escapeHTML(compactText(data.goal, 185)) + '</p>' : '';
  const gaps = data.gaps || [];
  const hasLegacyClaims = (data.findings || []).some(finding => finding.recorded_status);
  const conclusionDetail = data.conclusion_detail && data.conclusion_detail !== data.conclusion ? '<details><summary>Full coordinator notes</summary><p>' + escapeHTML(data.conclusion_detail) + '</p></details>' : '';
  const conclusion = data.conclusion ? '<section class="section"><h2>' + (hasLegacyClaims ? 'Coordinator conclusion · review required' : 'Coordinator conclusion') + '</h2>' + (hasLegacyClaims ? '<p class="review-note">This analysis includes earlier reproduction claims without independent challenge records. Those claims remain unconfirmed here.</p>' : '') + '<p>' + escapeHTML(data.conclusion) + '</p>' + conclusionDetail + '</section>' : '';
  const latestResult = data.review_pending ? '<section class="section"><h2>Latest worker result · awaiting review</h2><p>' + escapeHTML(data.latest_result || 'No summary recorded.') + '</p></section>' : '';
  const gapItems = items => items.map(item => '<li>' + escapeHTML(item) + '</li>').join('');
  const gapList = '<ul class="gap-list">' + gapItems(gaps.slice(0, 4)) + '</ul>' + (gaps.length > 4 ? '<details><summary>Show ' + (gaps.length - 4) + ' more gaps</summary><ul class="gap-list">' + gapItems(gaps.slice(4)) + '</ul></details>' : '');
  const findingHeading = selectedRisk === 'all' ? 'Findings and observations' : selectedRisk === 'candidates' ? 'Observations to confirm' : selectedRisk === 'reproduced' ? 'Reproduced findings' : selectedRisk === 'confirmed_exposures' ? 'Confirmed software exposures' : selectedRisk[0].toUpperCase() + selectedRisk.slice(1) + ' findings';
  const actions = (data.next_actions || []);
  const actionLabels = {fix:'Fix and retest', verify:'Confirm first', investigate:'Further check', review:'Review limits'};
  const actionItem = item => '<li><span class="action-kind">' + escapeHTML(actionLabels[item.kind] || 'Next step') + '</span>' + (item.finding_id ? '<button type="button" class="action-finding" data-action-finding="' + escapeHTML(item.finding_id) + '">' + escapeHTML(item.title) + '</button>' : '<strong>' + escapeHTML(item.title) + '</strong>') + (item.detail ? '<p>' + escapeHTML(item.detail) + '</p>' : '') + '</li>';
  const actionSection = actions.length ? '<section class="section priority-actions"><div class="section-heading"><div><h2>What to do next</h2><p>Review confirmed issues and open questions. Each item links to its evidence and next step.</p></div></div><ol class="action-list">' + actions.slice(0, 3).map(actionItem).join('') + '</ol>' + (actions.length > 3 ? '<details><summary>Show ' + (actions.length - 3) + ' more actions</summary><ol class="action-list">' + actions.slice(3).map(actionItem).join('') + '</ol></details>' : '') + (gaps.length ? '<button type="button" class="subtle-action" id="show-details">See test coverage and remaining questions ↓</button>' : '') + '</section>' : '';
  app.innerHTML =
    '<header class="analysis-head"><div><div class="eyebrow">' + escapeHTML(data.kind === 'customer' ? 'Across customer sessions' : 'Assessment evidence') + '</div><h1>' + escapeHTML(title) + '</h1>' + goal + '<p>' + escapeHTML(data.summary || '') + '</p></div><div class="head-side"><span class="status" data-state="' + escapeHTML(data.status) + '">' + escapeHTML(displayStatus(data.status)) + '</span>' + (data.report_url ? '<a class="report-link" href="' + escapeHTML(data.report_url) + '" target="_blank" rel="noopener">Open formal report ↗</a>' : '') + '</div></header>' +
    '<section class="summary-grid" aria-label="Filter findings and observations by risk">' + metric(risk.critical || 0,'Critical','critical') + metric(risk.high || 0,'High','high') + metric(risk.medium || 0,'Medium','medium') + metric(risk.low || 0,'Low','low') + metric(risk.reproduced || 0,'Reproduced','reproduced') + metric(risk.confirmed_exposures || 0,'Affected software','confirmed_exposures') + metric(risk.candidates || 0,'Unconfirmed','candidates') + '</section><p class="metric-note">Select a number to review its records. Severity counts include reproduced findings and confirmed affected software. The latter does not mean exploitation was tested.' + (data.kind === 'customer' ? ' Across sessions these are finding records, not a count of distinct weaknesses.' + (risk.remediated ? ' ' + risk.remediated + ' earlier finding(s) have an explicit remediation retest and are excluded from current risk counts.' : '') : '') + '</p>' +
    actionSection +
    assistantSection() +
    targetHistorySection(data) +
    '<section class="section finding-workspace" id="findings"><div class="section-heading"><div><h2>' + findingHeading + '</h2><p>Confirmed findings call for action. Candidate observations need more evidence before their security impact is established.</p></div><div class="finding-controls"><span class="workspace-count">' + findings.length + ' shown</span>' + (selectedRisk !== 'all' ? '<button type="button" class="clear-filter" id="clear-risk">Show all</button>' : '') + '</div></div>' + findingExplorer(findings) + '</section>' +
    codeReviewSection(allFindings, escapeHTML, selectedCode) +
    webPagesSection(data, visibleSessions, escapeHTML) +
    '<details class="analysis-more" id="analysis-details"><summary>Explore tests, open questions, and session history <span>↘</span></summary><div class="analysis-more-body">' + coverageSection(data) + sessionComparisonSection(data) + challengeSection(data, visibleSessions) + correlationSection(data, visibleSessions || []) + '<div class="columns"><div>' + conclusion + latestResult + '</div><aside><section class="side-card"><h2>Recorded limits and open questions · ' + gaps.length + '</h2>' + gapList + '</section></aside></div></div></details><p class="analysis-caveat">Only recorded work is shown. Review the evidence before sharing a conclusion; untested areas may still contain weaknesses.</p>';
  mountAssistant(app.querySelector('#analysis-assistant'), data);
  app.querySelectorAll('[data-ask-finding]').forEach(button => button.addEventListener('click', () => {
    const finding = (data.findings || []).find(item => item.id === button.dataset.askFinding);
    if (finding) askAboutFinding(finding);
  }));
  app.querySelector('#show-details')?.addEventListener('click', () => {
    const details = app.querySelector('#analysis-details');
    details.open = true;
    details.scrollIntoView({block:'start', behavior:'smooth'});
  });
  app.querySelectorAll('[data-action-finding]').forEach(button => button.addEventListener('click', () => {
    const index = (data.findings || []).findIndex(finding => finding.id === button.dataset.actionFinding);
    if (index < 0) return;
    selectedScope = -1;
    selectedRisk = 'all';
    selectedFinding = index;
    render(data);
    app.querySelector('#findings')?.scrollIntoView({block:'start', behavior:'smooth'});
  }));
  const codeView = app.querySelector('#code-review');
  if (codeView) {
    codeView.open = codeOpen;
    codeView.addEventListener('toggle', () => { codeOpen = codeView.open; });
    codeView.querySelectorAll('[data-code-index]').forEach(button => button.addEventListener('click', () => {
      selectedCode = Number(button.dataset.codeIndex);
      codeOpen = true;
      render(data);
      app.querySelector('#code-review')?.scrollIntoView({block:'start'});
    }));
    codeView.querySelector('[data-code-finding]')?.addEventListener('click', event => {
      selectedRisk = 'all';
      selectedFinding = Number(event.currentTarget.dataset.codeFinding);
      render(data);
      app.querySelector('#findings')?.scrollIntoView({block:'start', behavior:'smooth'});
    });
  }
  app.querySelectorAll('[data-risk]').forEach(button => button.addEventListener('click', () => {
    selectedRisk = button.dataset.risk;
    selectedFinding = 0;
    render(data);
    app.querySelector('#findings')?.scrollIntoView({block:'start', behavior:'smooth'});
  }));
  app.querySelector('#clear-risk')?.addEventListener('click', () => { selectedRisk = 'all'; selectedFinding = 0; render(data); });
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
  bindWebPages(app, data, visibleSessions, escapeHTML, index => {
    selectedRisk = 'all';
    selectedFinding = index;
    render(data);
    app.querySelector('#findings')?.scrollIntoView({block:'start', behavior:'smooth'});
  });
}

async function load() {
  const assessment = params.get('assessment');
  const customer = params.get('customer');
  if (!assessment && !customer) { app.innerHTML = '<p class="error">Choose an assessment or customer from the coordinator first.</p>'; return; }
  const path = assessment ? '/api/v1/assessments/' + encodeURIComponent(assessment) + '/analysis' : '/api/v1/customers/' + encodeURIComponent(customer) + '/analysis';
  try {
    const response = await fetch(path);
    if (!response.ok) throw new Error((await response.json()).error || response.statusText);
    const data = await response.json();
    const findingID = params.get('finding');
    if (findingID) {
      const index = (data.findings || []).findIndex(finding => finding.id === findingID);
      if (index >= 0) selectedFinding = index;
    }
    render(data);
    if (findingID) app.querySelector('#findings')?.scrollIntoView({block:'start'});
    await loadAssistant(data);
  } catch (error) {
    app.innerHTML = '<p class="error">' + escapeHTML(error.message) + '</p>';
  }
}
load();
