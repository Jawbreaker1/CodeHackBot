let selectedSession = '';
let selectedPage = '';
let showAll = false;

const pageKey = page => [page.session_id, page.url].join('\n');
const shortPath = url => {
  try {
    const parsed = new URL(url);
    return parsed.pathname === '/' ? parsed.host + '/' : parsed.pathname;
  } catch { return url; }
};

function currentPages(data, visibleSessions) {
  const eligible = (data.web_pages || []).filter(page => !visibleSessions || visibleSessions.includes(page.session_id));
  if (!eligible.length) return {eligible, pages: [], sessions: []};
  const sessions = [...new Set(eligible.map(page => page.session_id))];
  if (!sessions.includes(selectedSession)) selectedSession = sessions.at(-1);
  const observations = eligible.filter(page => page.session_id === selectedSession);
  const byURL = new Map();
  for (const page of observations) {
    const previous = byURL.get(page.url);
    if (!previous) {
      byURL.set(page.url, {...page, task_ids: [page.task_id], finding_refs: [...(page.finding_refs || [])], captures: [page]});
      continue;
    }
    if (!previous.task_ids.includes(page.task_id)) previous.task_ids.push(page.task_id);
    if (page.screenshot_url) previous.screenshot_url = page.screenshot_url;
    previous.finding_refs = [...new Set([...previous.finding_refs, ...(page.finding_refs || [])])];
    previous.captures.push(page);
  }
  const pages = [...byURL.values()];
  if (!pages.some(page => pageKey(page) === selectedPage)) {
    const degree = new Map(pages.map(page => [page.url, 0]));
    for (const edge of data.web_transitions || []) {
      if (edge.session_id !== selectedSession) continue;
      if (degree.has(edge.from)) degree.set(edge.from, degree.get(edge.from) + 1);
      if (degree.has(edge.to)) degree.set(edge.to, degree.get(edge.to) + 1);
    }
    selectedPage = pageKey(pages.reduce((best, page) => degree.get(page.url) > degree.get(best.url) ? page : best, pages[0]));
  }
  return {eligible, pages, sessions, observations};
}

function visibleGraphPages(pages, edges) {
  if (showAll || pages.length <= 18) return pages;
  const neighbors = new Map(pages.map(page => [pageKey(page), new Set()]));
  for (const edge of edges) {
    const from = pageKey({session_id: edge.session_id, url: edge.from});
    const to = pageKey({session_id: edge.session_id, url: edge.to});
    if (neighbors.has(from) && neighbors.has(to)) {
      neighbors.get(from).add(to);
      neighbors.get(to).add(from);
    }
  }
  const shown = new Set([selectedPage]);
  let frontier = [selectedPage];
  for (let distance = 0; distance < 2 && shown.size < 18; distance++) {
    const next = [];
    for (const key of frontier) {
      for (const neighbor of neighbors.get(key) || []) {
        if (shown.size >= 18) break;
        if (!shown.has(neighbor)) { shown.add(neighbor); next.push(neighbor); }
      }
    }
    frontier = next;
  }
  return pages.filter(page => shown.has(pageKey(page)));
}

function layoutGraph(pages, edges) {
  const keys = new Set(pages.map(pageKey));
  const depths = new Map();
  const origin = keys.has(selectedPage) ? selectedPage : pageKey(pages[0]);
  depths.set(origin, 0);
  const queue = [origin];
  for (let i = 0; i < queue.length; i++) {
    const current = queue[i];
    for (const edge of edges) {
      const from = pageKey({session_id: edge.session_id, url: edge.from});
      const to = pageKey({session_id: edge.session_id, url: edge.to});
      if (from === current && keys.has(to) && !depths.has(to)) {
        depths.set(to, Math.min(depths.get(current) + 1, 5));
        queue.push(to);
      }
    }
  }
  for (const page of pages) if (!depths.has(pageKey(page))) depths.set(pageKey(page), 0);
  const rows = new Map();
  const positions = new Map();
  for (const page of pages) {
    const depth = depths.get(pageKey(page));
    const row = rows.get(depth) || 0;
    positions.set(pageKey(page), {x: 20 + depth * 254, y: 20 + row * 112});
    rows.set(depth, row + 1);
  }
  const width = 260 + Math.max(...[...depths.values()]) * 254;
  const height = 130 + Math.max(...[...rows.values()]) * 112;
  return {positions, width, height};
}

function mapHTML(pages, edges, findings, escapeHTML) {
  const graphPages = visibleGraphPages(pages, edges);
  const {positions, width, height} = layoutGraph(graphPages, edges);
  const paths = [];
  const seen = new Set();
  for (const edge of edges) {
    const from = positions.get(pageKey({session_id: edge.session_id, url: edge.from}));
    const to = positions.get(pageKey({session_id: edge.session_id, url: edge.to}));
    const identity = [edge.session_id, edge.from, edge.to].join('\n');
    if (!from || !to || seen.has(identity)) continue;
    seen.add(identity);
    if (to.x > from.x) {
      const x1 = from.x + 218, y1 = from.y + 45, x2 = to.x - 4, y2 = to.y + 45;
      const curve = Math.max(34, Math.abs(x2 - x1) / 2);
      paths.push(`<path d="M${x1} ${y1} C${x1 + curve} ${y1},${x2 - curve} ${y2},${x2} ${y2}" marker-end="url(#web-arrow)"/>`);
    } else {
      const downward = from.y <= to.y;
      const x1 = from.x + 109, x2 = to.x + 109;
      const y1 = from.y + (downward ? 90 : 0), y2 = to.y + (downward ? 0 : 90);
      const bend = downward ? 34 : -34;
      paths.push(`<path d="M${x1} ${y1} C${x1} ${y1 + bend},${x2} ${y2 - bend},${x2} ${y2}" marker-end="url(#web-arrow)"/>`);
    }
  }
  const nodes = graphPages.map(page => {
    const position = positions.get(pageKey(page));
    const selected = pageKey(page) === selectedPage;
    const count = (page.finding_refs || []).filter(index => findings[index]).length;
    const image = page.screenshot_url ? `<img src="${escapeHTML(page.screenshot_url)}" alt="" loading="lazy">` : '<span class="web-map-no-image">No capture</span>';
    return `<button type="button" class="web-map-node${selected ? ' selected' : ''}${count ? ' has-finding' : ''}" data-web-page-index="${pages.indexOf(page)}" style="left:${position.x}px;top:${position.y}px" aria-pressed="${selected}" title="${escapeHTML(page.url)}">${image}<span class="web-map-node-text"><strong>${escapeHTML(shortPath(page.url))}</strong><small>${count ? count + ' linked finding' + (count === 1 ? '' : 's') : 'Observed page'}</small></span></button>`;
  });
  const transitionText = seen.size ? `${seen.size} visible transition${seen.size === 1 ? '' : 's'}` : 'No transition between these pages was recorded';
  return `<div class="web-map-scroll" role="region" aria-label="Observed page navigation" tabindex="0"><div class="web-map-canvas" style="width:${width}px;height:${height}px"><svg class="web-map-edges" width="${width}" height="${height}" aria-hidden="true"><defs><marker id="web-arrow" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto"><path d="M0 0 L7 3.5 L0 7"/></marker></defs>${paths.join('')}</svg>${nodes.join('')}</div></div><p class="web-map-count">Showing ${graphPages.length} of ${pages.length} observed pages · ${transitionText}</p>`;
}

function pageDetail(page, findings, escapeHTML) {
  const linked = (page.finding_refs || []).filter(index => findings[index]);
  const captures = (page.captures || [page]).filter(capture => capture.screenshot_url);
  const primary = captures.find(capture => (capture.finding_refs || []).some(index => linked.includes(index))) || captures.at(-1);
  const image = primary
    ? `<a class="web-map-shot" href="${escapeHTML(primary.screenshot_url)}" target="_blank" rel="noopener"><img src="${escapeHTML(primary.screenshot_url)}" alt="Recorded screenshot of ${escapeHTML(page.url)}" loading="lazy"><span>Open full screenshot ↗</span></a>`
    : '<p class="web-map-empty">No screenshot was saved for this page.</p>';
  const otherCaptures = captures.length > 1 ? `<details class="web-map-captures"><summary>All ${captures.length} saved captures</summary><ul>${captures.map((capture, index) => `<li><a href="${escapeHTML(capture.screenshot_url)}" target="_blank" rel="noopener">Capture ${index + 1} · ${escapeHTML(capture.task_id)} ↗</a>${(capture.finding_refs || []).length ? ' · cited by a finding' : ''}</li>`).join('')}</ul></details>` : '';
  const assessment = linked.length ? linked.map(index => {
    const finding = findings[index];
    const status = finding.status === 'reproduced' ? 'Reproduced' : 'Needs confirmation';
    return `<article class="web-map-finding"><span class="tag ${escapeHTML(finding.priority)}">${status}</span><h4>${escapeHTML(finding.title)}</h4><p>${escapeHTML(finding.impact || 'Impact not recorded.')}</p><button type="button" data-web-finding="${index}">Open finding ↗</button></article>`;
  }).join('') : '<p class="web-map-empty">No finding explicitly cites this page’s screenshot. This does not mean the page is safe. <a href="#findings">Review all findings ↑</a></p>';
  const oldNote = page.comment ? `<details><summary>Saved analyst note</summary><p>${escapeHTML(page.comment)}</p></details>` : '';
  return `<div class="web-map-detail"><div class="web-map-detail-head"><span class="eyebrow">Selected page</span><strong>${escapeHTML(page.url)}</strong><small>Observed by ${escapeHTML((page.task_ids || [page.task_id]).join(', '))} · <a href="/analysis?assessment=${encodeURIComponent(page.session_id)}">Open session ↗</a></small></div>${image}${otherCaptures}<div class="web-map-analysis"><h3>Analysis for this page</h3>${assessment}${oldNote}</div></div>`;
}

export function webPagesSection(data, visibleSessions, escapeHTML) {
  const {pages, sessions, observations} = currentPages(data, visibleSessions);
  if (!pages.length) return '';
  const edges = (data.web_transitions || []).filter(edge => edge.session_id === selectedSession);
  const selected = pages.find(page => pageKey(page) === selectedPage) || pages[0];
  const sessionPicker = sessions.length > 1 ? `<label>Session <select id="web-map-session">${sessions.map(id => `<option value="${escapeHTML(id)}"${id === selectedSession ? ' selected' : ''}>${escapeHTML(id)}</option>`).join('')}</select></label>` : '';
  const pagePicker = `<label>Focus page <select id="web-map-page">${pages.map((page, index) => `<option value="${index}"${pageKey(page) === selectedPage ? ' selected' : ''}>${escapeHTML(page.url)}</option>`).join('')}</select></label>`;
  const toggle = pages.length > 18 ? `<button type="button" id="web-map-toggle">${showAll ? 'Focus on nearby pages' : 'Show all pages'}</button>` : '';
  return `<section class="section exploration web-exploration" id="web-exploration"><div class="section-heading"><div><h2>Observed web navigation</h2><p>Only recorded pages and transitions are shown. Repeated visits share one node; a finding appears on a page only when it cites that page’s capture.</p></div><span class="workspace-count">${pages.length} distinct ${pages.length === 1 ? 'page' : 'pages'} · ${observations.length} ${observations.length === 1 ? 'capture' : 'captures'}</span></div><div class="web-map-controls">${sessionPicker}${pagePicker}${toggle}</div><div class="web-map-layout"><div class="web-map-primary">${mapHTML(pages, edges, data.findings || [], escapeHTML)}</div>${pageDetail(selected, data.findings || [], escapeHTML)}</div></section>`;
}

export function bindWebPages(app, data, visibleSessions, escapeHTML, openFinding) {
  const section = app.querySelector('#web-exploration');
  if (!section) return;
  const refresh = () => {
    section.outerHTML = webPagesSection(data, visibleSessions, escapeHTML);
    bindWebPages(app, data, visibleSessions, escapeHTML, openFinding);
  };
  section.querySelector('#web-map-session')?.addEventListener('change', event => {
    selectedSession = event.target.value;
    selectedPage = '';
    showAll = false;
    refresh();
  });
  section.querySelector('#web-map-page')?.addEventListener('change', event => { selectedPage = pageKey(currentPages(data, visibleSessions).pages[Number(event.target.value)]); refresh(); });
  section.querySelector('#web-map-toggle')?.addEventListener('click', () => { showAll = !showAll; refresh(); });
  section.querySelectorAll('[data-web-page-index]').forEach(button => button.addEventListener('click', () => { selectedPage = pageKey(currentPages(data, visibleSessions).pages[Number(button.dataset.webPageIndex)]); refresh(); }));
  section.querySelectorAll('[data-web-finding]').forEach(button => button.addEventListener('click', () => openFinding(Number(button.dataset.webFinding))));
}
