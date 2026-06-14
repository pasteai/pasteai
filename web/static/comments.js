(function () {
  var _docId;
  var _comments = [];
  var _pendingText = null;
  var _pendingStartChar = -1;
  var _pendingEndChar = -1;
  var _pendingRect = null;
  var _isTouchDevice = window.matchMedia('(hover: none)').matches;
  var _vpListener = null;
  var _scrollToId = null;
  var _selectionTimer = null;
  var AUTHOR_KEY = 'pasteai_comment_author';
  var MAX_QUOTE = 500; // chars; prevent wrapping entire document

  // ── Public API ────────────────────────────────────────────────────────────

  function initComments(docId) {
    _docId = docId;
    loadComments();

    var article = document.querySelector('article.markdown-body');
    if (article) {
      // selectionchange is reliable on all platforms including Android and iOS
      // (fires after selection handles settle, unlike touchend which races).
      document.addEventListener('selectionchange', function () {
        clearTimeout(_selectionTimer);
        _selectionTimer = setTimeout(onSelectionChange, 150);
      });
    }

    // pointerdown capture: reset float/toggle unless tapping the button,
    // the open popover, or the toggle while it's in "ready" state.
    document.addEventListener('pointerdown', function (e) {
      var floatBtn = document.getElementById('add-comment-float-btn');
      var popover = document.getElementById('add-comment-popover');
      var toggle = document.getElementById('comment-toggle-btn');
      var onFloatBtn = floatBtn && (floatBtn === e.target || floatBtn.contains(e.target));
      var onPopover = popover && !popover.hidden && (popover === e.target || popover.contains(e.target));
      var onReadyToggle = toggle && toggle.classList.contains('comment-toggle-btn--ready') &&
                          (toggle === e.target || toggle.contains(e.target));
      if (!onFloatBtn && !onPopover && !onReadyToggle) hideFloatBtn();
    }, true);

    // Keyboard: Escape closes form; Tab trapped inside form.
    document.addEventListener('keydown', function (e) {
      var popover = document.getElementById('add-comment-popover');
      if (!popover || popover.hidden) return;
      if (e.key === 'Escape') { cancelAddComment(); return; }
      if (e.key !== 'Tab') return;
      var focusable = popover.querySelectorAll('textarea, input, button:not([disabled])');
      if (!focusable.length) return;
      var first = focusable[0];
      var last = focusable[focusable.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault(); last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault(); first.focus();
      }
    });

    // Close sidebar on scrim tap or outside click.
    var sidebarScrim = document.getElementById('comment-sidebar-scrim');
    if (sidebarScrim) sidebarScrim.addEventListener('click', closeSidebar);
    document.addEventListener('click', function (e) {
      var sidebar = document.getElementById('comment-sidebar');
      var toggle = document.getElementById('comment-toggle-btn');
      if (sidebar && !sidebar.hidden &&
          !sidebar.contains(e.target) &&
          toggle && !toggle.contains(e.target)) {
        closeSidebar();
      }
    });
  }

  function toggleCommentSidebar() {
    var sidebar = document.getElementById('comment-sidebar');
    if (!sidebar) return;
    if (sidebar.hidden) openSidebar(); else closeSidebar();
  }

  function openSidebar() {
    var sidebar = document.getElementById('comment-sidebar');
    var scrim = document.getElementById('comment-sidebar-scrim');
    if (sidebar) sidebar.hidden = false;
    if (scrim) scrim.hidden = false;
  }

  function closeSidebar() {
    var sidebar = document.getElementById('comment-sidebar');
    var scrim = document.getElementById('comment-sidebar-scrim');
    if (sidebar) sidebar.hidden = true;
    if (scrim) scrim.hidden = true;
  }

  function showAddCommentForm() {
    hideFloatBtn();
    if (!_pendingText) return;
    var popover = document.getElementById('add-comment-popover');
    if (!popover) return;
    var scrim = document.getElementById('add-comment-scrim');

    // Show the selected quote so the user can confirm their selection.
    var preview = document.getElementById('comment-quote-preview');
    if (preview) {
      var q = _pendingText.length > 120 ? _pendingText.slice(0, 120) + '…' : _pendingText;
      preview.textContent = '“' + q + '”';
    }

    // Pre-fill author name from localStorage.
    var authorInput = document.getElementById('comment-author-input');
    if (authorInput && !authorInput.value) {
      authorInput.value = localStorage.getItem(AUTHOR_KEY) || '';
    }

    var isMobile = window.innerWidth < 640;

    if (isMobile) {
      // ── Mobile: bottom sheet ───────────────────────────────────────────
      // No body scroll lock — setting position:fixed on <body> creates a new
      // containing block on iOS Safari and makes position:fixed children invisible.
      // The scrim has touch-action:none to block scroll-through instead.
      if (scrim) scrim.hidden = false;
      popover.style.bottom = '0';
      popover.hidden = false;

      // Keep the sheet above the software keyboard via visualViewport.
      if (window.visualViewport) {
        _vpListener = function () {
          var vv = window.visualViewport;
          var kbHeight = Math.max(0, window.innerHeight - vv.offsetTop - vv.height);
          popover.style.bottom = kbHeight + 'px';
          popover.style.maxHeight = (vv.height * 0.8) + 'px';
        };
        window.visualViewport.addEventListener('resize', _vpListener);
        window.visualViewport.addEventListener('scroll', _vpListener);
        _vpListener();
      }
    } else {
      // ── Desktop: position near the selection ───────────────────────────
      popover.style.bottom = '';
      popover.style.maxHeight = '';
      popover.style.left = '-9999px';
      popover.style.top = '-9999px';
      popover.hidden = false;

      var vw = window.innerWidth;
      var vh = window.innerHeight;
      var pw = popover.offsetWidth || 300;
      var ph = popover.offsetHeight || 220;
      var rect = _pendingRect;

      if (rect) {
        var left = Math.max(8, Math.min(rect.left + rect.width / 2 - pw / 2, vw - pw - 8));
        var top = rect.bottom + 8;
        if (top + ph > vh - 8) top = Math.max(8, rect.top - ph - 8);
        popover.style.left = left + 'px';
        popover.style.top = top + 'px';
      } else {
        popover.style.left = Math.max(8, (vw - pw) / 2) + 'px';
        popover.style.top = '120px';
      }
    }

    var ta = document.getElementById('comment-body-input');
    if (ta) ta.focus();
  }

  function cancelAddComment() {
    clearTimeout(_selectionTimer);
    _pendingText = null;
    _pendingStartChar = -1;
    _pendingEndChar = -1;
    hideFloatBtn();

    var popover = document.getElementById('add-comment-popover');
    if (popover) {
      popover.hidden = true;
      popover.style.bottom = '';
      popover.style.maxHeight = '';
    }
    var scrim = document.getElementById('add-comment-scrim');
    if (scrim) scrim.hidden = true;

    if (_vpListener && window.visualViewport) {
      window.visualViewport.removeEventListener('resize', _vpListener);
      window.visualViewport.removeEventListener('scroll', _vpListener);
      _vpListener = null;
    }

    resetSubmitBtn();
    if (window.getSelection) window.getSelection().removeAllRanges();
  }

  function submitAddComment() {
    var ta = document.getElementById('comment-body-input');
    var body = ta ? ta.value.trim() : '';
    if (!body) { if (ta) ta.focus(); return; }
    if (!_pendingText) return;
    if (_pendingStartChar < 0) {
      alert('Selection is no longer valid — please select the text again.');
      cancelAddComment();
      return;
    }

    var authorInput = document.getElementById('comment-author-input');
    var author = authorInput ? authorInput.value.trim() : '';
    if (author) localStorage.setItem(AUTHOR_KEY, author);

    setSubmitLoading(true);

    fetch('/api/documents/' + _docId + '/comments', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        author: author,
        body: body,
        quoted_text: _pendingText,
        start_char: _pendingStartChar,
        end_char: _pendingEndChar
      })
    }).then(function (r) {
      if (r.ok) {
        return r.json().then(function (comment) {
          _scrollToId = comment.id;
          if (ta) ta.value = '';
          cancelAddComment();
          loadComments();
        });
      }
      if (r.status === 401) {
        alert('Please sign in to add a comment.');
        cancelAddComment();
        return;
      }
      setSubmitLoading(false);
      alert('Failed to submit comment — try again.');
    }).catch(function () {
      setSubmitLoading(false);
      alert('Network error — try again.');
    });
  }

  function resolveComment(cid, resolved) {
    fetch('/api/documents/' + _docId + '/comments/' + cid, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ resolved: resolved })
    }).then(function (r) {
      if (r.ok) { loadComments(); }
      else if (r.status === 403) { alert('Not authorised to modify this comment.'); }
      else { alert('Failed to update comment — try again.'); }
    }).catch(function () { alert('Network error — try again.'); });
  }

  function deleteComment(cid) {
    if (!confirm('Delete this comment?')) return;
    fetch('/api/documents/' + _docId + '/comments/' + cid, {
      method: 'DELETE'
    }).then(function (r) {
      if (r.status === 204) { loadComments(); }
      else if (r.status === 403) { alert('Not authorised to delete this comment.'); }
      else { alert('Failed to delete comment — try again.'); }
    }).catch(function () { alert('Network error — try again.'); });
  }

  // ── Internal ──────────────────────────────────────────────────────────────

  function loadComments() {
    fetch('/api/documents/' + _docId + '/comments')
      .then(function (r) { return r.ok ? r.json() : []; })
      .then(function (cs) { _comments = cs || []; renderAll(); })
      .catch(function () {});
  }

  function renderAll() {
    document.querySelectorAll('mark.comment-anchor').forEach(function (m) {
      var p = m.parentNode;
      while (m.firstChild) p.insertBefore(m.firstChild, m);
      p.removeChild(m);
    });

    var toggle = document.getElementById('comment-toggle-btn');
    var openCount = _comments.filter(function (c) { return !c.resolved; }).length;
    if (toggle) {
      toggle.hidden = false;
      // Don't clobber the ready state if a selection is active.
      if (!toggle.classList.contains('comment-toggle-btn--ready')) {
        if (_comments.length === 0) {
          toggle.textContent = 'Add a review';
          toggle.onclick = showAddCommentHint;
        } else {
          toggle.innerHTML = 'Reviews <span id="comment-count">' + openCount + '</span>';
          toggle.onclick = toggleCommentSidebar;
        }
      }
    }

    var list = document.getElementById('comment-list');
    if (list) list.innerHTML = '';

    _comments.forEach(function (c) {
      var range = findTextRange(c.quoted_text, c.start_char, c.end_char);
      renderAnchor(c, range);
      if (list) renderGutterEntry(list, c, range);
    });

    if (_scrollToId) {
      var id = _scrollToId;
      _scrollToId = null;
      requestAnimationFrame(function () {
        var mark = document.querySelector('mark.comment-anchor[data-cid="' + id + '"]');
        if (mark) mark.scrollIntoView({ behavior: 'smooth', block: 'center' });
      });
    }
  }

  function showAddCommentHint() {
    var hint = document.getElementById('comment-hint');
    if (hint) {
      hint.hidden = false;
      setTimeout(function () { hint.hidden = true; }, 4000);
    }
  }

  // findTextRange: locate the stored quote in the article DOM.
  // Uses stored char offsets when available to disambiguate repeated text.
  function findTextRange(text, startChar, endChar) {
    var article = document.querySelector('article.markdown-body');
    if (!article || !text) return null;

    var walker = document.createTreeWalker(article, NodeFilter.SHOW_TEXT);
    var nodes = [];
    var full = '';
    var node;
    while ((node = walker.nextNode())) {
      nodes.push({ node: node, start: full.length });
      full += node.nodeValue;
    }

    // Prefer stored offsets when they round-trip correctly.
    var idx;
    if (startChar >= 0 && endChar > startChar && full.slice(startChar, endChar) === text) {
      idx = startChar;
    } else {
      idx = full.indexOf(text);
    }
    if (idx < 0) return null;

    var end = idx + text.length;
    var range = document.createRange();
    var started = false;

    for (var i = 0; i < nodes.length; i++) {
      var n = nodes[i];
      var nEnd = n.start + n.node.nodeValue.length;
      if (!started && idx < nEnd) { range.setStart(n.node, idx - n.start); started = true; }
      if (started && end <= nEnd) { range.setEnd(n.node, end - n.start); return range; }
    }
    return null;
  }

  // rangeToCharOffsets: compute start/end char positions within the article.
  // More accurate than innerText.indexOf() for repeated text.
  function rangeToCharOffsets(range, article) {
    var walker = document.createTreeWalker(article, NodeFilter.SHOW_TEXT);
    var pos = 0;
    var startChar = -1;
    var endChar = -1;
    var node;
    while ((node = walker.nextNode())) {
      if (node === range.startContainer) startChar = pos + range.startOffset;
      if (node === range.endContainer) { endChar = pos + range.endOffset; break; }
      pos += node.nodeValue.length;
    }
    return { start: startChar, end: endChar };
  }

  function renderAnchor(c, range) {
    if (!range) return;
    var mark = document.createElement('mark');
    mark.className = 'comment-anchor' + (c.resolved ? ' comment-anchor--resolved' : '');
    mark.dataset.cid = c.id;
    try {
      range.surroundContents(mark);
    } catch (_) {
      // Cross-element range (e.g. spans <strong>, <code> boundary).
      // Extract and re-insert so the mark wraps the fragment.
      try {
        mark.appendChild(range.extractContents());
        range.insertNode(mark);
      } catch (_2) {
        return;
      }
    }
    mark.addEventListener('mouseenter', function () { highlightEntry(c.id, true); });
    mark.addEventListener('mouseleave', function () { highlightEntry(c.id, false); });
    // Clicking an anchor opens the sidebar and scrolls to the comment entry.
    mark.addEventListener('click', function () {
      openSidebar();
      requestAnimationFrame(function () {
        var entry = document.getElementById('comment-entry-' + c.id);
        if (entry) entry.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
      });
    });
  }

  function renderGutterEntry(list, c, range) {
    var isStale = !range;
    var div = document.createElement('div');
    div.id = 'comment-entry-' + c.id;
    div.className = 'comment-entry' +
      (c.resolved ? ' comment-entry--resolved' : '') +
      (isStale ? ' comment-entry--stale' : '');

    var staleHTML = isStale
      ? '<span class="comment-stale-label" title="The document was edited — this text no longer exists">⚠ Text changed</span>'
      : '';
    var resolveLabel = c.resolved ? 'Unresolve' : 'Resolve';

    // Only show management actions to the document owner.
    var canManage = !!window._pasteaiCanManageComments;
    var actionsHTML = canManage
      ? '<div class="comment-entry-actions">' +
          '<button class="comment-btn" onclick="resolveComment(\'' + c.id + '\',' + !c.resolved + ')">' + resolveLabel + '</button>' +
          '<button class="comment-btn comment-btn--danger" onclick="deleteComment(\'' + c.id + '\')">Delete</button>' +
        '</div>'
      : '';

    div.innerHTML =
      '<div class="comment-entry-header">' +
        '<span class="comment-entry-author">' + esc(c.author || 'anonymous') + '</span>' +
        staleHTML +
      '</div>' +
      '<blockquote class="comment-entry-quote" title="' + esc(c.quoted_text) + '">' +
        esc(truncate(c.quoted_text, 80)) +
      '</blockquote>' +
      '<p class="comment-entry-body">' + esc(c.body) + '</p>' +
      actionsHTML;

    div.addEventListener('mouseenter', function () { highlightAnchor(c.id, true); });
    div.addEventListener('mouseleave', function () { highlightAnchor(c.id, false); });
    list.appendChild(div);
  }

  function highlightEntry(cid, on) {
    var el = document.getElementById('comment-entry-' + cid);
    if (el) el.classList.toggle('comment-entry--active', on);
  }

  function highlightAnchor(cid, on) {
    var el = document.querySelector('mark.comment-anchor[data-cid="' + cid + '"]');
    if (el) el.classList.toggle('comment-anchor--active', on);
  }

  function onSelectionChange() {
    // Don't fire while the comment form is open — typing in the textarea
    // triggers selectionchange inside the form.
    var popover = document.getElementById('add-comment-popover');
    if (popover && !popover.hidden) return;

    var sel = window.getSelection();
    if (!sel || sel.isCollapsed || sel.rangeCount === 0) { hideFloatBtn(); return; }
    var range = sel.getRangeAt(0);
    var article = document.querySelector('article.markdown-body');
    if (!article || !article.contains(range.commonAncestorContainer)) { hideFloatBtn(); return; }
    var text = sel.toString().trim();
    if (!text || text.length > MAX_QUOTE) { hideFloatBtn(); return; }

    _pendingText = text;
    var offsets = rangeToCharOffsets(range, article);
    _pendingStartChar = offsets.start;
    _pendingEndChar = offsets.end;

    var rect = range.getBoundingClientRect();
    _pendingRect = rect;

    if (_isTouchDevice || window.innerWidth < 640) {
      // Mobile: turn the always-visible toggle button into the tap target.
      // The float button is too small and unreliable next to touch selection handles.
      var toggle = document.getElementById('comment-toggle-btn');
      if (toggle) {
        toggle.classList.add('comment-toggle-btn--ready');
        toggle.textContent = '+ Comment';
        toggle.onclick = showAddCommentForm;
        toggle.setAttribute('aria-label', 'Tap to add comment to selection');
      }
    } else {
      var btn = document.getElementById('add-comment-float-btn');
      if (!btn) return;
      var vw = window.innerWidth;
      var left = Math.max(8, Math.min(rect.left + rect.width / 2 - 60, vw - 128));
      var top = Math.min(rect.bottom + 6, window.innerHeight - 40);
      btn.style.left = left + 'px';
      btn.style.top = top + 'px';
      btn.hidden = false;
    }
  }

  function hideFloatBtn() {
    var btn = document.getElementById('add-comment-float-btn');
    if (btn) btn.hidden = true;
    resetToggleBtn();
  }

  function resetToggleBtn() {
    var toggle = document.getElementById('comment-toggle-btn');
    if (!toggle || !toggle.classList.contains('comment-toggle-btn--ready')) return;
    toggle.classList.remove('comment-toggle-btn--ready');
    toggle.setAttribute('aria-label', 'Open reviews');
    var openCount = _comments.filter(function (c) { return !c.resolved; }).length;
    if (_comments.length === 0) {
      toggle.textContent = 'Add a review';
      toggle.onclick = showAddCommentHint;
    } else {
      toggle.innerHTML = 'Reviews <span id="comment-count">' + openCount + '</span>';
      toggle.onclick = toggleCommentSidebar;
    }
  }

  function setSubmitLoading(loading) {
    var btn = document.querySelector('#add-comment-popover .add-comment-actions button:last-child');
    if (!btn) return;
    btn.disabled = loading;
    btn.textContent = loading ? 'Submitting…' : 'Submit';
  }

  function resetSubmitBtn() { setSubmitLoading(false); }

  function esc(s) {
    return String(s || '')
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function truncate(s, n) {
    s = String(s || '');
    return s.length > n ? s.slice(0, n) + '…' : s;
  }

  window.initComments = initComments;
  window.toggleCommentSidebar = toggleCommentSidebar;
  window.showAddCommentForm = showAddCommentForm;
  window.cancelAddComment = cancelAddComment;
  window.submitAddComment = submitAddComment;
  window.resolveComment = resolveComment;
  window.deleteComment = deleteComment;
})();
