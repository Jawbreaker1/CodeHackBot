const text = value => String(value ?? '');

function entriesFor(findings) {
  const entries = [];
  findings.forEach((finding, findingIndex) => {
    (finding.source_locations || []).forEach(source => entries.push({finding, findingIndex, source}));
  });
  return entries.sort((a, b) =>
    text(a.source.repository).localeCompare(text(b.source.repository)) ||
    text(a.source.revision).localeCompare(text(b.source.revision)) ||
    text(a.source.path).localeCompare(text(b.source.path)) ||
    a.source.start_line - b.source.start_line);
}

export function codeReviewSection(findings, escapeHTML, selectedIndex) {
  const entries = entriesFor(findings);
  if (!entries.length) return '';
  const selected = entries[Math.min(selectedIndex, entries.length - 1)];
  const groups = new Map();
  entries.forEach((entry, index) => {
    const key = [entry.source.repository, entry.source.revision, entry.source.path].join('\n');
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key).push({entry, index});
  });
  const picker = [...groups.values()].map(group => {
    const source = group[0].entry.source;
    return '<div class="code-file-group"><h3>' + escapeHTML(source.path) + '</h3><p>' + escapeHTML(source.repository) + ' · ' + escapeHTML(source.revision) + '</p>' +
      group.map(({entry, index}) => '<button type="button" class="code-select' + (selected === entry ? ' selected' : '') + '" data-code-index="' + index + '" aria-pressed="' + (selected === entry) + '"><strong>' + escapeHTML(entry.finding.title) + '</strong><span>Line ' + escapeHTML(entry.source.start_line) + ' · ' + escapeHTML(entry.finding.status === 'reproduced' ? 'Target check recorded' : 'Source lead to verify') + '</span></button>').join('') + '</div>';
  }).join('');
  const {finding, findingIndex, source} = selected;
  const range = source.end_line && source.end_line !== source.start_line ? source.start_line + '–' + source.end_line : source.start_line;
  const lines = source.lines?.length
    ? '<pre class="code-excerpt"><code>' + source.lines.map(line => '<span class="code-line' + (line.highlight ? ' highlighted' : '') + '"><span class="code-line-number">' + line.number + '</span><span>' + escapeHTML(line.text) + '</span></span>').join('') + '</code></pre>'
    : '<p class="code-unavailable">No bounded code preview is available from the registered source file. Review the saved evidence.</p>';
  const artifact = source.artifact_url ? '<a href="' + escapeHTML(source.artifact_url) + '" target="_blank" rel="noopener">Open recorded source ↗</a>' : '';
  const fix = finding.remediation?.length ? '<ol>' + finding.remediation.map(step => '<li>' + escapeHTML(step) + '</li>').join('') + '</ol>' : '<p>No specific correction was recorded.</p>';
  return '<details class="analysis-more code-review" id="code-review"><summary>Code review <span>' + entries.length + ' source location' + (entries.length === 1 ? '' : 's') + ' ↘</span></summary>' +
    '<div class="code-review-body"><p class="code-intro">Recorded source locations are review leads. A matching deployed version and vulnerable behavior require their own checks.</p><div class="code-review-layout"><nav class="code-picker" aria-label="Source findings">' + picker + '</nav>' +
    '<article class="code-inspector"><div class="code-inspector-head"><span class="tag ' + escapeHTML(finding.priority) + '">' + escapeHTML(finding.status === 'reproduced' ? 'Target check recorded' : 'Needs target verification') + '</span><button type="button" data-code-finding="' + findingIndex + '">Open full finding ↗</button></div><h3>' + escapeHTML(finding.title) + '</h3><p class="code-location">' + escapeHTML(source.path) + ':' + escapeHTML(range) + ' · recorded revision ' + escapeHTML(source.revision) + '</p>' +
    lines + '<div class="code-explanation"><div><h4>Why this matters</h4><p>' + escapeHTML(finding.impact || 'No impact was recorded.') + '</p></div><div><h4>How to correct it</h4>' + fix + '</div></div>' + artifact + '</article></div></div></details>';
}
