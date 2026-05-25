// app.js — limit modal lifecycle and POST /api/documents 422 handling.

// Intercept fetch calls to POST /api/documents and handle 422 limit responses.
(function () {
  var origFetch = window.fetch;
  window.fetch = function (input, init) {
    var url = typeof input === 'string' ? input : (input && input.url) || '';
    var method = (init && init.method) ? init.method.toUpperCase() : 'GET';
    if (url === '/api/documents' && method === 'POST') {
      return origFetch.call(this, input, init).then(function (resp) {
        if (resp.status === 422) {
          // Clone so the caller can still read the body.
          return resp.clone().json().then(function (body) {
            var vis = (typeof body.error === 'string' && body.error.indexOf('private') !== -1)
              ? 'private'
              : 'public';
            var upgradeURL = (typeof body.upgrade_url === 'string') ? body.upgrade_url : '/profile';
            sessionStorage.setItem('limit_reached', JSON.stringify({ type: vis, url: upgradeURL }));
            return resp;
          }, function () {
            // JSON parse failed — store a safe default and return the response.
            sessionStorage.setItem('limit_reached', JSON.stringify({ type: 'public', url: '/profile' }));
            return resp;
          });
        }
        return resp;
      });
    }
    return origFetch.apply(this, arguments);
  };
})();

// On page load, show a modal if limit_reached is set in sessionStorage.
document.addEventListener('DOMContentLoaded', function () {
  if (window.location.pathname.indexOf('/profile') === 0) {
    return;
  }

  var raw = sessionStorage.getItem('limit_reached');
  if (!raw) {
    return;
  }

  var stored;
  try {
    stored = JSON.parse(raw);
  } catch (_) {
    return;
  }

  var isPrivate = stored.type === 'private';
  var upgradeURL = (typeof stored.url === 'string' && stored.url) ? stored.url : '/profile';

  var limitLabel = isPrivate ? ‘private’ : ‘public’;
  var limitCount = isPrivate ? ‘50’ : ‘100’;
  var msg = isCheckout
    ? "You’ve reached your " + limitLabel + " document limit (" + limitCount + " docs). Upgrade to Pro for unlimited documents."
    : "You’ve reached your " + limitLabel + " document limit (" + limitCount + " docs on the free plan). Remove some documents to free up space.";

  var isCheckout = upgradeURL.indexOf('checkout') !== -1;
  var ctaText = isCheckout ? 'Upgrade to Pro — $5/month' : 'Manage documents';

  // Build overlay.
  var overlay = document.createElement('div');
  overlay.id = 'limit-modal-overlay';
  overlay.className = 'limit-modal-overlay';

  // Build card.
  var card = document.createElement('div');
  card.className = 'limit-modal';

  // Dismiss button.
  var dismiss = document.createElement('button');
  dismiss.className = 'limit-modal-dismiss';
  dismiss.setAttribute('aria-label', 'Dismiss');
  dismiss.textContent = '×';
  dismiss.addEventListener('click', function () {
    sessionStorage.removeItem('limit_reached');
    document.body.removeChild(overlay);
  });

  // Message.
  var p = document.createElement('p');
  p.className = 'limit-modal-msg';
  p.textContent = msg;

  // CTA — use a link when the URL is not a checkout endpoint, form POST when it is.
  var ctaContainer;
  if (isCheckout) {
    ctaContainer = document.createElement('form');
    ctaContainer.method = 'POST';
    ctaContainer.action = upgradeURL;
    var btn = document.createElement('button');
    btn.type = 'submit';
    btn.className = 'limit-modal-cta';
    btn.textContent = ctaText;
    ctaContainer.appendChild(btn);
  } else {
    ctaContainer = document.createElement('a');
    ctaContainer.href = upgradeURL;
    ctaContainer.className = 'limit-modal-cta';
    ctaContainer.textContent = ctaText;
  }

  card.appendChild(dismiss);
  card.appendChild(p);
  card.appendChild(ctaContainer);
  overlay.appendChild(card);
  document.body.appendChild(overlay);
});
