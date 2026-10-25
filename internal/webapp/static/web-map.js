let selectedRun = '';
let selectedURL = '';
let showAll = false;

const shortPath = raw => {
  try {
    const url = new URL(raw);
    return url.pathname === '/' ? url.host + '/' : url.pathname;
  } catch { return raw; }
};

const runLabel = (data, id, index) => {
  const finished = (data.sessions || []).find(session => session.id === id)?.updated_at;
  const date = typeof finished === 'string' ? finished.slice(0, 10) : '';
  return date ? `${date} · Run ${index + 1}` : `Run ${index + 1}`;
};

function mapData(data, visibleSessions) {
  const eligible = (data.web_pages || []).filter(page => !visibleSessions || visibleSessions.includes(page.session_id));
  if (!eligible.length) return {pages: [], observations: [], runs: [], edges: [], links: []};
  const runs = [...new Set(eligible.map(page => page.session_id))];
  const project = data.kind === 'customer' && runs.length > 1;
  if (!selectedRun || (selectedRun !== 'all' && !runs.includes(selectedRun)) || (!project && selectedRun === 'all')) {
    selectedRun = project ? 'all' : runs.at(-1);
  }
  const visible = entry => selectedRun === 'all' ? runs.includes(entry.session_id) : entry.session_id === selectedRun;
  const observations = eligible.filter(visible);
  const byURL = new Map();
  for (const capture of observations) {
    const page = byURL.get(capture.url);
    if (!page) {
      byURL.set(capture.url, {...capture, run_ids: [capture.session_id], finding_refs: [...(capture.finding_refs || [])], captures: [capture]});
      continue;
    }
    if (!page.run_ids.includes(capture.session_id)) page.run_ids.push(capture.session_id);
    if (capture.screenshot_url) page.screenshot_url = capture.screenshot_url;
    page.links_captured ||= capture.links_captured;
    page.finding_refs = [...new Set([...page.finding_refs, ...(capture.finding_refs || [])])];
    page.captures.push(capture);
  }
  const pages = [...byURL.values()];
  const links = (data.web_links || []).filter(link => visible(link) && byURL.has(link.from));
  const byPair = new Map();
  const addEdge = (entry, kind) => {
    if (!visible(entry) || !byURL.has(entry.from) || !byURL.has(entry.to) || entry.from === entry.to) return;
    const key = entry.from + '\n' + entry.to;
    let edge = byPair.get(key);
    if (!edge) {
      edge = {from: entry.from, to: entry.to, link: false, navigation: false};
      byPair.set(key, edge);
    }
    edge[kind] = true;
  };
  for (const link of links) addEdge(link, 'link');
  for (const transition of data.web_transitions || []) addEdge(transition, 'navigation');
  const edges = [...byPair.values()];
  if (!byURL.has(selectedURL)) {
    const degree = new Map(pages.map(page => [page.url, 0]));
    for (const edge of edges) {
      degree.set(edge.from, degree.get(edge.from) + 1);
      degree.set(edge.to, degree.get(edge.to) + 1);
    }
    selectedURL = pages.reduce((best, page) => degree.get(page.url) > degree.get(best.url) ? page : best, pages[0]).url;
  }
  return {pages, observations, runs, edges, links, project};
}

function visiblePages(pages, edges) {
  if (showAll || pages.length <= 18) return pages;
  const neighbors = new Map(pages.map(page => [page.url, new Set()]));
  for (const edge of edges) {
    neighbors.get(edge.from)?.add(edge.to);
    neighbors.get(edge.to)?.add(edge.from);
  }
  const shown = new Set([selectedURL]);
  let frontier = [selectedURL];
  for (let distance = 0; distance < 2 && shown.size < 18; distance++) {
    const next = [];
    for (const url of frontier) {
      for (const neighbor of neighbors.get(url) || []) {
        if (shown.size >= 18) break;
        if (!shown.has(neighbor)) { shown.add(neighbor); next.push(neighbor); }
      }
    }
    frontier = next;
  }
  for (const page of pages) {
    if (shown.size >= 18) break;
    shown.add(page.url);
  }
  return pages.filter(page => shown.has(page.url));
}

function layout(pages, edges) {
  const keys = new Set(pages.map(page => page.url));
  const neighbors = new Map(pages.map(page => [page.url, new Set()]));
  for (const edge of edges) {
    if (!keys.has(edge.from) || !keys.has(edge.to)) continue;
    neighbors.get(edge.from).add(edge.to);
    neighbors.get(edge.to).add(edge.from);
  }
  const depth = new Map([[selectedURL, 0]]);
  const queue = [selectedURL];
  for (let i = 0; i < queue.length; i++) {
    for (const neighbor of neighbors.get(queue[i]) || []) {
      if (!depth.has(neighbor)) {
        depth.set(neighbor, depth.get(queue[i]) + 1);
        queue.push(neighbor);
      }
    }
  }
  const positions = new Map();
  const rows = new Map();
  for (const page of pages) {
    if (!depth.has(page.url)) continue;
    const column = depth.get(page.url), row = rows.get(column) || 0;
    positions.set(page.url, {x: 20 + column * 252, y: 20 + row * 110});
    rows.set(column, row + 1);
  }
  const connectedHeight = 20 + Math.max(0, ...rows.values()) * 110;
  const disconnected = pages.filter(page => !depth.has(page.url));
  disconnected.forEach((page, index) => positions.set(page.url, {x: 20 + (index % 3) * 252, y: connectedHeight + 20 + Math.floor(index / 3) * 110}));
  return {
    positions,
    width: Math.max(250 + Math.max(0, ...depth.values()) * 252, disconnected.length ? 754 : 0),
    height: Math.max(130, connectedHeight + (disconnected.length ? 20 + Math.ceil(disconnected.length / 3) * 110 : 0))
  };
}

function graphHTML(pages, edges, findings, escapeHTML) {
  const shown = visiblePages(pages, edges);
  const {positions, width, height} = layout(shown, edges);
  let linkCount = 0, navigationCount = 0;
  const paths = [];
  for (const edge of edges) {
    const from = positions.get(edge.from), to = positions.get(edge.to);
    if (!from || !to) continue;
    if (edge.link) linkCount++;
    if (edge.navigation) navigationCount++;
    const kind = edge.navigation ? 'navigation' : 'link-only';
    if (to.x !== from.x) {
      const rightward = to.x > from.x;
      const x1 = from.x + (rightward ? 218 : -4), y1 = from.y + 45;
      const x2 = to.x + (rightward ? -4 : 222), y2 = to.y + 45;
      const bend = (x2 - x1) / 2;
      paths.push(`<path class="${kind}" d="M${x1} ${y1} C${x1 + bend} ${y1},${x2 - bend} ${y2},${x2} ${y2}" marker-end="url(#web-arrow)"/>`);
    } else {
      const downward = from.y <= to.y;
      const x1 = from.x + 109, x2 = to.x + 109;
      const y1 = from.y + (downward ? 90 : 0), y2 = to.y + (downward ? 0 : 90);
      const bend = downward ? 34 : -34;
      paths.push(`<path class="${kind}" d="M${x1} ${y1} C${x1} ${y1 + bend},${x2} ${y2 - bend},${x2} ${y2}" marker-end="url(#web-arrow)"/>`);
    }
  }
  const nodes = shown.map(page => {
    const position = positions.get(page.url);
    const count = (page.finding_refs || []).filter(index => findings[index]).length;
    const image = page.screenshot_url ? `<img src="${escapeHTML(page.screenshot_url)}" alt="" loading="lazy">` : '<span class="web-map-no-image">No capture</span>';
    return `<button type="button" class="web-map-node${page.url === selectedURL ? ' selected' : ''}${count ? ' has-finding' : ''}" data-web-page-index="${pages.indexOf(page)}" style="left:${position.x}px;top:${position.y}px" aria-pressed="${page.url === selectedURL}" title="${escapeHTML(page.url)}">${image}<span class="web-map-node-text"><strong>${escapeHTML(shortPath(page.url))}</strong><small>${count ? count + ' linked finding' + (count === 1 ? '' : 's') : 'Opened page'}</small></span></button>`;
  });
  const missing = shown.filter(page => !page.links_captured).length;
  return `<div class="web-map-scroll" role="region" aria-label="Observed page map" tabindex="0"><div class="web-map-canvas" style="width:${width}px;height:${height}px"><svg class="web-map-edges" width="${width}" height="${height}" aria-hidden="true"><defs><marker id="web-arrow" markerWidth="7" markerHeight="7" refX="6" refY="3.5" orient="auto"><path d="M0 0 L7 3.5 L0 7"/></marker></defs>${paths.join('')}</svg>${nodes.join('')}</div></div><p class="web-map-count">Showing ${shown.length} of ${pages.length} opened pages · ${navigationCount} recorded ${navigationCount === 1 ? 'navigation' : 'navigations'} · ${linkCount} observed ${linkCount === 1 ? 'link' : 'links'} between opened pages${missing ? ` · Link data unavailable for ${missing} ${missing === 1 ? 'page' : 'pages'}; missing lines do not mean there is no link.` : ''}</p>`;
}

function detailHTML(page, pages, links, findings, runs, data, escapeHTML) {
  const linked = (page.finding_refs || []).filter(index => findings[index]);
  const captures = (page.captures || [page]).filter(capture => capture.screenshot_url);
  const primary = captures.find(capture => (capture.finding_refs || []).some(index => linked.includes(index))) || captures.at(-1);
  const image = primary
    ? `<a class="web-map-shot" href="${escapeHTML(primary.screenshot_url)}" target="_blank" rel="noopener"><img src="${escapeHTML(primary.screenshot_url)}" alt="Recorded screenshot of ${escapeHTML(page.url)}" loading="lazy"><span>Open full screenshot ↗</span></a>`
    : '<p class="web-map-empty">No screenshot was saved for this page.</p>';
  const otherCaptures = captures.length > 1 ? `<details class="web-map-captures"><summary>All ${captures.length} saved captures</summary><ul>${captures.map((capture, index) => `<li><a href="${escapeHTML(capture.screenshot_url)}" target="_blank" rel="noopener">Capture ${index + 1} · ${escapeHTML(capture.task_id)} ↗</a>${(capture.finding_refs || []).length ? ' · cited by a finding' : ''}</li>`).join('')}</ul></details>` : '';
  const findingsHTML = linked.length ? linked.map(index => {
    const finding = findings[index];
    const status = finding.status === 'reproduced' ? 'Reproduced' : finding.status === 'confirmed_exposure' ? 'Affected software; exploit not tested' : 'Needs confirmation';
    return `<article class="web-map-finding"><span class="tag ${escapeHTML(finding.priority)}">${status}</span><h4>${escapeHTML(finding.title)}</h4><p>${escapeHTML(finding.impact || 'Impact not recorded.')}</p><button type="button" data-web-finding="${index}">Open finding ↗</button></article>`;
  }).join('') : '<p class="web-map-empty">No finding cites a screenshot of this page. Other recorded findings may still concern it. <a href="#findings">Review all findings ↑</a></p>';
  const outgoing = [...new Set(links.filter(link => link.from === page.url).map(link => link.to))];
  const linkList = outgoing.length ? `<ul class="web-map-links">${outgoing.slice(0, 12).map(target => {
    const index = pages.findIndex(candidate => candidate.url === target);
    const label = `<span>${escapeHTML(shortPath(target))}</span>`;
    return `<li>${index >= 0 ? `<button type="button" data-web-page-index="${index}" title="${escapeHTML(target)}">${label} <small>Focus page ↗</small></button>` : `${label} <small>Not opened</small>`}</li>`;
  }).join('')}</ul>${outgoing.length > 12 ? `<p class="web-map-count">${outgoing.length - 12} more links omitted from this compact view.</p>` : ''}`
    : `<p class="web-map-empty">${page.links_captured ? 'No same-site links were recorded on this page.' : 'This capture did not save a link inventory.'}</p>`;
  const runLinks = (page.run_ids || [page.session_id]).map((id, index) => {
    const runIndex = runs.indexOf(id);
    return `<a href="/analysis?assessment=${encodeURIComponent(id)}" title="${escapeHTML(id)}">${escapeHTML(runLabel(data, id, runIndex >= 0 ? runIndex : index))} ↗</a>`;
  }).join(' · ');
  const note = page.comment ? `<details><summary>Saved analyst note</summary><p>${escapeHTML(page.comment)}</p></details>` : '';
  return `<div class="web-map-detail"><div class="web-map-detail-head"><span class="eyebrow">Selected page</span><strong>${escapeHTML(page.url)}</strong><small>Opened in ${(page.run_ids || [page.session_id]).length} run(s) · ${runLinks}</small></div>${image}${otherCaptures}<div class="web-map-analysis"><h3>Links from this page</h3><p class="web-map-hint">Links were observed in the page, not necessarily opened or tested.</p>${linkList}<h3>Findings linked to this page</h3>${findingsHTML}${note}</div></div>`;
}

export function webPagesSection(data, visibleSessions, escapeHTML) {
  const {pages, observations, runs, edges, links, project} = mapData(data, visibleSessions);
  if (!pages.length) return '';
  const selected = pages.find(page => page.url === selectedURL) || pages[0];
  const runPicker = runs.length > 1 ? `<label>Runs <select id="web-map-session">${project ? `<option value="all"${selectedRun === 'all' ? ' selected' : ''}>All runs</option>` : ''}${runs.map((id, index) => `<option value="${escapeHTML(id)}"${id === selectedRun ? ' selected' : ''}>${escapeHTML(runLabel(data, id, index))}</option>`).join('')}</select></label>` : '';
  const pagePicker = `<label>Focus page <select id="web-map-page">${pages.map((page, index) => `<option value="${index}"${page.url === selectedURL ? ' selected' : ''}>${escapeHTML(page.url)}</option>`).join('')}</select></label>`;
  const toggle = pages.length > 18 ? `<button type="button" id="web-map-toggle">${showAll ? 'Focus on nearby pages' : 'Show all pages'}</button>` : '';
  return `<section class="section exploration web-exploration" id="web-exploration"><div class="section-heading"><div><h2>Observed web pages</h2><p>Solid lines show navigation that happened. Dashed lines show links found on pages. The map only draws pages that were opened; older runs may lack link records.</p></div><span class="workspace-count">${pages.length} opened ${pages.length === 1 ? 'page' : 'pages'} · ${observations.length} ${observations.length === 1 ? 'capture' : 'captures'}</span></div><div class="web-map-controls">${runPicker}${pagePicker}${toggle}</div><div class="web-map-layout"><div class="web-map-primary">${graphHTML(pages, edges, data.findings || [], escapeHTML)}</div>${detailHTML(selected, pages, links, data.findings || [], runs, data, escapeHTML)}</div></section>`;
}

export function bindWebPages(app, data, visibleSessions, escapeHTML, openFinding) {
  const section = app.querySelector('#web-exploration');
  if (!section) return;
  const refresh = () => {
    section.outerHTML = webPagesSection(data, visibleSessions, escapeHTML);
    bindWebPages(app, data, visibleSessions, escapeHTML, openFinding);
  };
  section.querySelector('#web-map-session')?.addEventListener('change', event => {
    selectedRun = event.target.value;
    selectedURL = '';
    showAll = false;
    refresh();
  });
  section.querySelector('#web-map-page')?.addEventListener('change', event => { selectedURL = mapData(data, visibleSessions).pages[Number(event.target.value)]?.url || ''; refresh(); });
  section.querySelector('#web-map-toggle')?.addEventListener('click', () => { showAll = !showAll; refresh(); });
  section.querySelectorAll('[data-web-page-index]').forEach(button => button.addEventListener('click', () => { selectedURL = mapData(data, visibleSessions).pages[Number(button.dataset.webPageIndex)]?.url || ''; refresh(); }));
  section.querySelectorAll('[data-web-finding]').forEach(button => button.addEventListener('click', () => openFinding(Number(button.dataset.webFinding))));
}
