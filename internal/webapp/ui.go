package webapp

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>BirdHackBot assessment console</title>
  <style>
    :root { color-scheme: dark; font-family: Inter, ui-sans-serif, system-ui, sans-serif; background: #0b1020; color: #e8edf7; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; background: radial-gradient(circle at 20% 0%, #1a2b4d, #0b1020 45%); }
    header { padding: 28px clamp(20px, 6vw, 90px) 20px; border-bottom: 1px solid #2a3855; }
    h1 { margin: 0 0 8px; font-size: clamp(1.5rem, 3vw, 2.25rem); letter-spacing: -0.03em; }
    header p { max-width: 760px; margin: 0; color: #aebbd0; }
    main { display: grid; grid-template-columns: minmax(300px, 0.8fr) minmax(420px, 1.5fr); gap: 18px; max-width: 1500px; margin: 24px auto; padding: 0 20px 40px; }
    section { background: rgba(17, 27, 48, .9); border: 1px solid #2a3855; border-radius: 14px; padding: 18px; box-shadow: 0 16px 40px rgba(0,0,0,.18); }
    h2 { font-size: 1rem; margin: 0 0 14px; color: #94c5ff; }
    label { display: block; margin: 14px 0 6px; color: #aebbd0; font-size: .88rem; }
    textarea, input { width: 100%; border: 1px solid #3b4d6d; border-radius: 8px; background: #0d1629; color: #f2f6fc; padding: 10px; font: inherit; }
    textarea { min-height: 100px; resize: vertical; }
    button { border: 0; border-radius: 8px; background: #4d9cff; color: #061020; font-weight: 700; padding: 10px 14px; cursor: pointer; }
    button.secondary { background: #263752; color: #e8edf7; }
    button.danger { background: #d86666; color: #1c0808; }
    button:disabled { opacity: .5; cursor: wait; }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; margin-top: 14px; }
    .notice { border-left: 3px solid #e0ad5c; padding: 9px 11px; margin-bottom: 15px; color: #e8d8b9; background: #2a2418; font-size: .86rem; }
    .meta { display: grid; grid-template-columns: auto 1fr; gap: 6px 12px; font-size: .88rem; color: #b9c7db; }
    .meta strong { color: #eef4ff; }
    #events { height: 360px; overflow: auto; background: #09101d; border: 1px solid #25344e; border-radius: 8px; padding: 12px; font: .82rem ui-monospace, SFMono-Regular, Menlo, monospace; white-space: pre-wrap; }
    .event { padding: 7px 0; border-bottom: 1px solid #1b2940; }
    .event:last-child { border-bottom: 0; }
    .event time { color: #7890ae; margin-right: 8px; }
    .pending { margin-top: 14px; display: grid; gap: 10px; }
    .pending-card { border: 1px solid #765d2f; border-radius: 8px; padding: 11px; background: #211d16; }
    .pending-card code { display: block; white-space: pre-wrap; overflow-wrap: anywhere; margin: 8px 0; color: #f6ddb1; }
    .session-list, .finding-list { display: grid; gap: 8px; margin-top: 12px; }
    .session-card, .finding-card { border: 1px solid #2f4262; border-radius: 8px; padding: 10px; background: #101d32; }
    .session-card strong, .finding-card strong { color: #eef4ff; }
    .session-card p, .finding-card p { margin: 6px 0 0; color: #b9c7db; font-size: .86rem; }
    .session-card a { color: #8fc4ff; }
    .muted { color: #8395af; font-size: .84rem; }
    .hidden { display: none !important; }
    .status { color: #93e0b0; }
    .status.bad { color: #ff9b9b; }
    @media (max-width: 900px) { main { grid-template-columns: 1fr; } }
  </style>
</head>
<body>
<header>
  <h1>BirdHackBot assessment console</h1>
  <p>Browser control surface for the shared orchestrator. Define exact scope, review every action, and keep the evidence-backed run visible.</p>
</header>
<main>
  <section>
    <div class="notice">Lab preview: this server has no authentication. Bind it to loopback and use only an authorized environment. Browser controls never call tools directly.</div>
    <h2>Start an assessment</h2>
    <form id="create">
      <label for="customer">Customer workspace</label>
      <input id="customer" required pattern="[A-Za-z0-9_-]+" maxlength="80" placeholder="customer-id">
      <label for="goal">Objective</label>
      <textarea id="goal" required placeholder="What should the assessment establish?"></textarea>
      <label for="scope">Exact scope and permissions</label>
      <textarea id="scope" required placeholder="Targets, allowed actions, exclusions, and evidence limits"></textarea>
      <div class="actions"><button type="submit">Create review</button></div>
    </form>
    <div id="review" class="hidden">
      <h2>Review</h2>
      <div id="reviewText" class="meta"></div>
      <div class="actions"><button id="start">Start assessment</button><button id="reset" class="secondary">Discard draft</button></div>
    </div>
    <div id="runMeta" class="hidden">
      <h2>Assessment status</h2>
      <div class="meta">
        <strong>Status</strong><span id="status" class="status">draft</span>
        <strong>Model</strong><span id="model">—</span>
        <strong>Plans</strong><span id="plans">0</span>
        <strong>Model calls</strong><span id="calls">0</span>
      </div>
      <div class="actions"><button id="stop" class="danger">Stop assessment</button><a id="report" class="button secondary" href="#" target="_blank">Open report</a></div>
    </div>
    <div id="customerSummary" class="hidden">
      <h2>Customer workspace</h2>
      <div id="customerSummaryMeta" class="meta"></div>
      <a id="customerReport" class="button secondary" href="#" target="_blank">Open unified customer report</a>
      <h2 style="margin-top:18px">Sessions</h2>
      <div id="customerSessions" class="session-list"></div>
      <h2 style="margin-top:18px">Findings across sessions</h2>
      <div id="customerFindings" class="finding-list"></div>
    </div>
    <div id="pending" class="pending"></div>
    <div id="messageBox" class="hidden">
      <label for="message">Talk to the coordinator</label>
      <textarea id="message" placeholder="Ask about progress or suggest the next investigation step"></textarea>
      <div class="actions"><button id="sendMessage">Send message</button></div>
    </div>
  </section>
  <section>
    <h2>Conversation and activity</h2>
    <div id="events" aria-live="polite">Create a scoped assessment to begin.</div>
  </section>
</main>
<script>
(() => {
  let id = null, after = 0, customer = null;
  const $ = (name) => document.getElementById(name);
  const json = (url, options = {}) => fetch(url, {headers: {'Content-Type': 'application/json'}, ...options}).then(async response => {
    const body = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(body.error || response.statusText);
    return body;
  });
  const showError = (error) => { $('events').textContent = 'Error: ' + error.message; };
  const addEvent = (record) => {
    const event = record.event || {};
    const line = document.createElement('div'); line.className = 'event';
    const time = document.createElement('time'); time.textContent = new Date(record.at).toLocaleTimeString();
    const text = document.createElement('span'); text.textContent = (event.task_id ? '[' + event.task_id + '] ' : '') + event.kind + (event.message ? ': ' + event.message : '') + (event.action ? ' — ' + event.action : '');
    line.append(time, text); $('events').append(line); $('events').scrollTop = $('events').scrollHeight;
  };
  const renderPending = (view) => {
    const box = $('pending'); box.replaceChildren();
    (view.pending_approvals || []).forEach(item => {
      const card = document.createElement('div'); card.className = 'pending-card';
      const title = document.createElement('strong'); title.textContent = 'Approval required · ' + item.task_id; card.append(title);
      const code = document.createElement('code'); code.textContent = item.command + '\n' + item.cwd; card.append(code);
      const actions = document.createElement('div'); actions.className = 'actions';
      [['approved_once','Approve once',''],['denied','Deny','danger']].forEach(([decision,label,kind]) => { const button=document.createElement('button'); button.textContent=label; if(kind) button.className=kind; button.onclick=()=>json('/api/v1/assessments/'+id+'/approvals/'+encodeURIComponent(item.id), {method:'POST', body:JSON.stringify({decision})}).then(refresh).catch(showError); actions.append(button); });
      card.append(actions); box.append(card);
    });
    (view.pending_questions || []).forEach(item => {
      const card = document.createElement('div'); card.className = 'pending-card';
      const title = document.createElement('strong'); title.textContent = 'Question · ' + item.task_id; card.append(title);
      const prompt = document.createElement('p'); prompt.textContent = item.text; card.append(prompt);
      const input = document.createElement('input'); input.placeholder = 'Answer'; const button = document.createElement('button'); button.textContent='Answer'; button.onclick=()=>json('/api/v1/assessments/'+id+'/questions/'+encodeURIComponent(item.id), {method:'POST', body:JSON.stringify({text:input.value})}).then(refresh).catch(showError); const actions=document.createElement('div'); actions.className='actions'; actions.append(input,button); card.append(actions); box.append(card);
    });
  };
  const renderCustomer = (view) => {
    if (!view || !view.id) return;
    $('customerSummary').classList.remove('hidden');
    $('customerReport').href = '/api/v1/customers/' + encodeURIComponent(view.id) + '/report';
    const meta = $('customerSummaryMeta'); meta.replaceChildren();
    [['Customer', view.id], ['Status', view.status], ['Sessions', String((view.sessions || []).length)], ['Findings', String((view.findings || []).length)]].forEach(([label, value]) => {
      const strong = document.createElement('strong'); strong.textContent = label;
      const text = document.createElement('span'); text.textContent = value; meta.append(strong, text);
    });
    const sessions = $('customerSessions'); sessions.replaceChildren();
    (view.sessions || []).forEach(session => {
      const card = document.createElement('div'); card.className = 'session-card';
      const title = document.createElement('strong'); title.textContent = session.status + ' · ' + session.id; card.append(title);
      const detail = document.createElement('p'); detail.textContent = session.goal + ' · ' + (session.model || 'not started'); card.append(detail);
      const link = document.createElement('a'); link.href = session.report_url; link.target = '_blank'; link.textContent = 'Open session report'; card.append(link);
      sessions.append(card);
    });
    if (!view.sessions || view.sessions.length === 0) {
      const empty = document.createElement('div'); empty.className = 'muted'; empty.textContent = 'No assessment sessions yet.'; sessions.append(empty);
    }
    const findings = $('customerFindings'); findings.replaceChildren();
    (view.findings || []).forEach(item => {
      const card = document.createElement('div'); card.className = 'finding-card';
      const title = document.createElement('strong'); title.textContent = item.finding.title + ' · ' + item.finding.status; card.append(title);
      const detail = document.createElement('p'); detail.textContent = 'Session ' + item.session_id + ': ' + item.finding.impact; card.append(detail);
      findings.append(card);
    });
    if (!view.findings || view.findings.length === 0) {
      const empty = document.createElement('div'); empty.className = 'muted'; empty.textContent = 'No model-authored findings have been recorded.'; findings.append(empty);
    }
  };
  const refreshCustomer = () => customer ? json('/api/v1/customers/'+encodeURIComponent(customer)).then(renderCustomer).catch(() => {}) : Promise.resolve();
  const render = (view) => {
    if (view.customer) customer = view.customer;
    $('runMeta').classList.remove('hidden'); $('messageBox').classList.toggle('hidden', !['running','starting'].includes(view.status)); $('status').textContent=view.status; $('model').textContent=view.model || 'not started'; $('plans').textContent=view.plans; $('calls').textContent=view.usage.calls; $('report').href=view.report_url; $('stop').disabled=!['running','starting'].includes(view.status); renderPending(view);
    if (after === 0 && (!view.events || view.events.length === 0)) $('events').textContent = 'Assessment draft created. Review the scope and start when ready.';
    (view.events || []).forEach(record => { if(record.sequence > after) { after=record.sequence; addEvent(record); } });
    refreshCustomer();
  };
  const refresh = () => id ? json('/api/v1/assessments/'+encodeURIComponent(id)+'?after='+after).then(render).catch(showError) : Promise.resolve();
  $('create').onsubmit = (event) => { event.preventDefault(); json('/api/v1/assessments', {method:'POST', body:JSON.stringify({customer:$('customer').value, goal:$('goal').value, scope:$('scope').value})}).then(view => { id=view.id; customer=view.customer; $('create').classList.add('hidden'); $('review').classList.remove('hidden'); const review=$('reviewText'); review.replaceChildren(); [['Customer', view.customer], ['Objective', view.goal], ['Scope', view.scope]].forEach(([label, value]) => { const strong=document.createElement('strong'); strong.textContent=label; const text=document.createElement('span'); text.textContent=value; review.append(strong,text); }); render(view); }).catch(showError); };
  $('start').onclick = () => json('/api/v1/assessments/'+id+'/start', {method:'POST'}).then(view => { $('review').classList.add('hidden'); render(view); }).catch(showError);
  $('reset').onclick = () => location.reload();
  $('stop').onclick = () => json('/api/v1/assessments/'+id+'/stop', {method:'POST'}).then(render).catch(showError);
  $('sendMessage').onclick = () => { const text=$('message').value.trim(); if(!text) return; json('/api/v1/assessments/'+id+'/messages', {method:'POST', body:JSON.stringify({text})}).then(view => { $('message').value=''; render(view); }).catch(showError); };
  setInterval(refresh, 1000);
})();
</script>
</body>
</html>`
