const assessment = new URLSearchParams(location.search).get('assessment');
const root = `/api/v1/assessments/${encodeURIComponent(assessment || '')}/context`;
const $ = id => document.getElementById(id);
let index = null, current = null, selected = null, omissions = [];
const prettyBytes = n => n >= 1024 ? `${(n / 1024).toFixed(1)} KiB` : `${n} B`;
function error(message) { $('error').textContent = message; $('error').hidden = false; setTimeout(() => $('error').hidden = true, 7000); }
async function fetchJSON(url, options) { const response = await fetch(url, options); const body = await response.json(); if (!response.ok) throw Error(body.error || response.statusText); return body; }
function groupLabel(turn) { return turn.kind === 'coordinator' ? 'Coordinator' : turn.task; }
async function refresh() {
  if (!assessment) { error('Open this view from an assessment session.'); return; }
  try {
    index = await fetchJSON(`${root}/index`);
    const nav = $('turns'); nav.replaceChildren();
    let group = '';
    for (const turn of index.turns) {
      const label = groupLabel(turn);
      if (label !== group) { const heading = document.createElement('div'); heading.className = 'group'; heading.textContent = label; nav.append(heading); group = label; }
      const button = document.createElement('button'); button.type = 'button'; button.className = 'turn';
      button.textContent = `Turn ${turn.turn}`;
      const meta = document.createElement('small'); meta.textContent = turn.has_full_request ? 'Exact request available' : 'Packet snapshot only'; button.append(meta);
      button.onclick = () => selectTurn(turn, button);
      nav.append(button);
    }
    if (!index.turns.length) nav.textContent = 'No recorded model turns yet.';
    if (!current && index.turns.length) nav.querySelector('.turn')?.click();
  } catch (e) { error(e.message); }
}
async function selectTurn(turn, button) {
  try {
    const params = new URLSearchParams({kind: turn.kind, turn: String(turn.turn)});
    if (turn.task) params.set('task', turn.task);
    current = await fetchJSON(`${root}/item?${params}`);
    for (const item of document.querySelectorAll('.turn')) item.classList.remove('active');
    button.classList.add('active');
    $('source').textContent = groupLabel(turn);
    $('turnTitle').textContent = `Turn ${turn.turn} · ${turn.kind === 'worker' ? 'worker decision' : 'coordinator plan'}`;
    if (turn.kind === 'worker') {
      const state = await fetchJSON(`${root}/omissions?task=${encodeURIComponent(turn.task)}`);
      omissions = state.sections || [];
    } else omissions = [];
    let sections = current.sections || [];
    if (!sections.length && Array.isArray(current.messages)) {
      sections = [];
      current.messages.forEach((message, i) => {
        if (message.role === 'user' && turn.kind === 'coordinator') {
          try {
            const packet = JSON.parse(message.content);
            for (const [name, value] of Object.entries(packet)) sections.push({Name:`${i + 1}. coordinator.${name}`, Content:JSON.stringify(value, null, 2)});
            return;
          } catch (_) { /* An exact raw request remains available below. */ }
        }
        sections.push({Name:`${i + 1}. ${message.role} message`, Content:message.content || ''});
      });
    }
    const total = sections.reduce((n, s) => n + (s.Content || '').length, 0);
    $('total').textContent = `${prettyBytes(total)} visible sections`;
    $('requestDetails').hidden = !current.messages;
    $('request').textContent = current.messages ? JSON.stringify(current.messages, null, 2) : '';
    const list = $('sections'); list.replaceChildren(); selected = null;
    for (const section of sections) {
      const bytes = (section.Content || '').length;
      const item = document.createElement('button'); item.type = 'button'; item.className = 'section';
      const line = document.createElement('div'); line.className = 'line';
      const name = document.createElement('span'); name.textContent = section.Name;
      const size = document.createElement('span'); size.textContent = prettyBytes(bytes);
      line.append(name, size);
      const track = document.createElement('div'); track.className = 'track';
      const fill = document.createElement('span'); fill.className = 'fill'; fill.style.width = `${Math.max(1, total ? bytes / total * 100 : 0)}%`; track.append(fill);
      item.append(line, track); item.onclick = () => selectSection(section, item); list.append(item);
    }
    list.querySelector('.section')?.click();
  } catch (e) { error(e.message); }
}
function selectSection(section, button) {
  selected = section;
  for (const item of document.querySelectorAll('.section')) item.classList.remove('active');
  button.classList.add('active');
  $('sectionTitle').textContent = section.Name;
  $('sectionSize').textContent = prettyBytes((section.Content || '').length);
  $('content').textContent = section.Content || '(empty)';
  const editable = current.kind === 'worker' && !!index.editable_sections[section.Name];
  $('omission').hidden = !editable;
  $('omitToggle').checked = omissions.includes(section.Name);
}
$('omitToggle').onchange = async () => {
  if (!selected || current?.kind !== 'worker') return;
  const next = new Set(omissions);
  if ($('omitToggle').checked) next.add(selected.Name); else next.delete(selected.Name);
  try {
    const result = await fetchJSON(`${root}/omissions?task=${encodeURIComponent(current.task)}`, {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify({sections:[...next]})});
    omissions = result.sections || [];
  } catch (e) { $('omitToggle').checked = ! $('omitToggle').checked; error(e.message); }
};
$('refresh').onclick = refresh;
refresh();
