// Anti-FOUC: apply the saved theme immediately, before first paint.
// This script is loaded synchronously in <head> so it runs before styles are applied.
(function(){var t=localStorage.getItem('pasteai-theme')||'arctic';document.documentElement.setAttribute('data-theme',t);})();

var themeLabels = {
  'light':             'Light',
  'dark':              'Dark',
  'emerald':           'Emerald',
  'catppuccin-mocha':  'Mocha',
  'catppuccin-latte':  'Latte',
  'catppuccin-frappe': 'Frappé',
  'arctic':            'Arctic'
};
var themeSwatches = {
  'light':             'linear-gradient(135deg,#ef4b58,#008f87)',
  'dark':              'linear-gradient(135deg,#ff4d4d,#00e5cc)',
  'emerald':           'linear-gradient(135deg,#50c878,#3ecfcf)',
  'catppuccin-mocha':  'linear-gradient(135deg,#cba6f7,#89dceb)',
  'catppuccin-latte':  'linear-gradient(135deg,#8839ef,#04a5e5)',
  'catppuccin-frappe': 'linear-gradient(135deg,#ca9ee6,#85c1dc)',
  'arctic':            'linear-gradient(135deg,#5aacf7,#38d9f5)'
};

function setTheme(theme) {
  document.documentElement.setAttribute('data-theme', theme);
  localStorage.setItem('pasteai-theme', theme);
  updateThemeBtn(theme);
  document.querySelectorAll('.theme-option').forEach(function(opt) {
    opt.classList.toggle('active', opt.dataset.themeVal === theme);
  });
}

function updateThemeBtn(theme) {
  var label  = document.getElementById('theme-label');
  var swatch = document.getElementById('theme-swatch');
  if (label)  label.textContent       = themeLabels[theme]  || theme;
  if (swatch) swatch.style.background = themeSwatches[theme] || '';
}

function toggleThemePicker() {
  var menu = document.getElementById('theme-menu');
  var btn  = document.getElementById('theme-btn');
  var open = menu.hasAttribute('hidden');
  if (open) {
    menu.removeAttribute('hidden');
    btn.setAttribute('aria-expanded', 'true');
  } else {
    menu.setAttribute('hidden', '');
    btn.setAttribute('aria-expanded', 'false');
  }
}

function closeThemePicker() {
  document.getElementById('theme-menu').setAttribute('hidden', '');
  document.getElementById('theme-btn').setAttribute('aria-expanded', 'false');
}

// Wire up all DOM-dependent event listeners after the document is ready.
document.addEventListener('DOMContentLoaded', function() {
  var t = localStorage.getItem('pasteai-theme') || 'arctic';
  updateThemeBtn(t);
  document.querySelectorAll('.theme-option').forEach(function(opt) {
    opt.classList.toggle('active', opt.dataset.themeVal === t);
  });
  // Enable theme transitions only after initial paint to prevent FOUC.
  document.documentElement.classList.add('theme-loaded');

  var themeBtn = document.getElementById('theme-btn');
  if (themeBtn) {
    themeBtn.addEventListener('click', toggleThemePicker);
  }

  document.querySelectorAll('.theme-option').forEach(function(btn) {
    btn.addEventListener('click', function() {
      setTheme(this.dataset.themeVal);
      closeThemePicker();
    });
  });

  document.addEventListener('click', function(e) {
    var picker = document.getElementById('theme-picker');
    if (picker && !picker.contains(e.target)) { closeThemePicker(); }
  });

  document.addEventListener('keydown', function(e) {
    if (e.key === 'Escape') { closeThemePicker(); }
  });
});
