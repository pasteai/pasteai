import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs';

var darkThemes = new Set(['dark', 'emerald', 'catppuccin-mocha', 'catppuccin-frappe', 'arctic']);

function mermaidTheme() {
  var t = document.documentElement.getAttribute('data-theme') || 'arctic';
  return darkThemes.has(t) ? 'dark' : 'default';
}

// Save original diagram source before first render so re-renders have the raw text.
document.querySelectorAll('.mermaid').forEach(function(el) {
  el.dataset.src = el.textContent;
});

function render() {
  mermaid.initialize({ startOnLoad: false, theme: mermaidTheme() });
  document.querySelectorAll('.mermaid').forEach(function(el) {
    el.removeAttribute('data-processed');
    el.textContent = el.dataset.src;
  });
  mermaid.run({ querySelector: '.mermaid' });
}

render();

// Re-render diagrams whenever the user switches theme.
new MutationObserver(render).observe(document.documentElement, {
  attributes: true, attributeFilter: ['data-theme']
});
