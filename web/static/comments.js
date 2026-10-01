(function () {
  var _docId;
  var _comments = [];
  var _pendingText = null;
  var _pendingStartChar = -1;
  var _pendingEndChar = -1;
  var _pendingRect = null;
  var _isTouchDevice = window.matchMedia('(hover: none)').matches;
  var _vpListener = null;
  var _detailVpListener = null;
  var _scrollToId = null;
  var _selectionTimer = null;
  var _activeId = null; // comment currently shown in the detail popout
  var _editing = false; // detail popout is in edit mode
  var AUTHOR_KEY = 'pasteai_comment_author';
  var MAX_QUOTE = 500; // chars; prevent wrapping entire document

  // ── Permissions ───────────────────────────────────────────────────────────
  // Both flags are injected by the template. In OSS/self-hosted mode there is
  // no auth, so CanComment is always true and every comment reports is_mine.

  function canComment() { return !!window._pasteaiCanComment; }
  function canManage() { return !!window._pasteaiCanManageComments; }

  // canEdit: only the comment's author may change its text. Gated on
  // canComment so a read-only viewer never sees mutation controls, whatever
  // the server reports for is_mine.
  function canEdit(c) { return canComment() && !!c.is_mine; }

  // canModerate: the author or the document owner may resolve/delete.
  function canModerate(c) { return canComment() && (!!c.is_mine || canManage()); }

  // ── Comment tree helpers ──────────────────────────────────────────────────

  function byId(cid) {
    for (var i = 0; i < _comments.length; i++) {
      if (_comments[i].id === cid) return _comments[i];
    }
    return null;
  }

  function rootComments() {
    return _comments.filter(function (c) { return !c.parent_id; });
  }

  function repliesOf(cid) {
    return _comments.filter(function (c) { return c.parent_id === cid; });
  }

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

    // Keyboard: Escape closes the detail popout, then the add form;
    // Tab is trapped inside whichever dialog is open.
    document.addEventListener('keydown', function (e) {
      var detail = document.getElementById('comment-detail-popover');
      var popover = document.getElementById('add-comment-popover');
      var dialog = (detail && !detail.hidden) ? detail
                 : (popover && !popover.hidden) ? popover
                 : null;
      if (!dialog) return;
      if (e.key === 'Escape') {
        if (dialog === detail) closeCommentDetail(); else cancelAddComment();
        return;
      }
      if (e.key !== 'Tab') return;
      var focusable = dialog.querySelectorAll('textarea, input, button:not([disabled])');
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
      var detail = document.getElementById('comment-detail-popover');
      // Clicks inside the detail popout must not close the sidebar behind it.
      if (detail && !detail.hidden && (detail === e.target || detail.contains(e.target))) return;
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

    if (isMobile()) {
      if (scrim) scrim.hidden = false;
      openAsSheet(popover);
    } else {
      openAsPopover(popover, _pendingRect);
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

    _vpListener = detachViewport(_vpListener);

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

    postComment({
      author: author,
      body: body,
      quoted_text: _pendingText,
      start_char: _pendingStartChar,
      end_char: _pendingEndChar
    }, function (comment) {
      _scrollToId = comment.id;
      if (ta) ta.value = '';
      cancelAddComment();
      loadComments();
    }, function () {
      setSubmitLoading(false);
    });
  }

  // postComment POSTs a new comment (top-level or reply) and reports the result.
  function postComment(payload, onSuccess, onError) {
    fetch('/api/documents/' + _docId + '/comments', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    }).then(function (r) {
      if (r.ok) return r.json().then(onSuccess);
      if (r.status === 401 || r.status === 403) {
        alert('Please sign in to comment.');
        onError();
        return;
      }
      alert('Failed to submit comment — try again.');
      onError();
    }).catch(function () {
      alert('Network error — try again.');
      onError();
    });
  }

  function resolveComment(cid, resolved) {
    patchComment(cid, { resolved: resolved });
  }

  // patchComment sends a PATCH and reloads on success.
  function patchComment(cid, payload) {
    fetch('/api/documents/' + _docId + '/comments/' + cid, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload)
    }).then(function (r) {
      if (r.ok) { _editing = false; loadComments(); }
      else if (r.status === 403) { alert('Not authorised to modify this comment.'); }
      else { alert('Failed to update comment — try again.'); }
    }).catch(function () { alert('Network error — try again.'); });
  }

  function deleteComment(cid) {
    var replyCount = repliesOf(cid).length;
    var msg = replyCount
      ? 'Delete this comment and its ' + replyCount + ' repl' + (replyCount === 1 ? 'y' : 'ies') + '?'
      : 'Delete this comment?';
    if (!confirm(msg)) return;

    // Replies are deleted first so no orphans remain if the parent delete fails.
    var replies = repliesOf(cid).map(function (c) { return c.id; });
    var chain = Promise.resolve();
    replies.forEach(function (rid) {
      chain = chain.then(function () {
        return fetch('/api/documents/' + _docId + '/comments/' + rid, { method: 'DELETE' });
      });
    });

    chain.then(function () {
      return fetch('/api/documents/' + _docId + '/comments/' + cid, { method: 'DELETE' });
    }).then(function (r) {
      if (r.status === 204) {
        if (_activeId === cid) closeCommentDetail();
        loadComments();
      } else if (r.status === 403) {
        alert('Not authorised to delete this comment.');
      } else {
        alert('Failed to delete comment — try again.');
      }
    }).catch(function () { alert('Network error — try again.'); });
  }

  // ── Detail popout ─────────────────────────────────────────────────────────

  // openCommentDetail shows a comment, its replies and the reply form.
  // Called from the sidebar; also scrolls the document to the anchor.
  function openCommentDetail(cid) {
    var c = byId(cid);
    if (!c) return;
    _activeId = cid;
    _editing = false;
    setActiveComment(cid);
    scrollToAnchor(cid);
    renderDetail();
  }

  function closeCommentDetail() {
    _activeId = null;
    _editing = false;
    var popover = document.getElementById('comment-detail-popover');
    if (popover) {
      popover.hidden = true;
      popover.style.bottom = '';
      popover.style.maxHeight = '';
    }
    var scrim = document.getElementById('comment-detail-scrim');
    if (scrim) scrim.hidden = true;
    _detailVpListener = detachViewport(_detailVpListener);
    setActiveComment(null);
  }

  // renderDetail rebuilds the detail popout for _activeId and positions it.
  function renderDetail() {
    var popover = document.getElementById('comment-detail-popover');
    var content = document.getElementById('comment-detail-content');
    if (!popover || !content) return;
    var c = _activeId ? byId(_activeId) : null;
    if (!c) { closeCommentDetail(); return; }

    content.innerHTML = detailHTML(c);

    var scrim = document.getElementById('comment-detail-scrim');
    if (isMobile()) {
      if (scrim) scrim.hidden = false;
      openAsSheet(popover);
    } else {
      var mark = document.querySelector('mark.comment-anchor[data-cid="' + c.id + '"]');
      openAsPopover(popover, mark ? mark.getBoundingClientRect() : null);
    }

    if (_editing) {
      var edit = document.getElementById('comment-edit-input');
      if (edit) { edit.focus(); edit.setSelectionRange(edit.value.length, edit.value.length); }
    }
  }

  // detailHTML renders the comment body (or edit form), actions and replies.
  function detailHTML(c) {
    var replies = repliesOf(c.id);
    return '' +
      '<div class="comment-detail-header">' +
        '<span class="comment-entry-author">' + esc(c.author || 'anonymous') + '</span>' +
        (c.resolved ? '<span class="comment-resolved-label">Resolved</span>' : '') +
        '<button class="comment-sidebar-close" onclick="closeCommentDetail()" aria-label="Close comment">×</button>' +
      '</div>' +
      '<blockquote class="comment-entry-quote">' + esc(truncate(c.quoted_text, 160)) + '</blockquote>' +
      (_editing ? editFormHTML(c) : bodyAndActionsHTML(c)) +
      repliesHTML(replies) +
      replyFormHTML(c);
  }

  function editFormHTML(c) {
    return '' +
      '<div class="add-comment-field">' +
        '<label for="comment-edit-input">Edit comment</label>' +
        '<textarea id="comment-edit-input" rows="3">' + esc(c.body) + '</textarea>' +
      '</div>' +
      '<div class="add-comment-actions">' +
        '<button onclick="cancelEditComment()">Cancel</button>' +
        '<button onclick="saveEditComment()">Save</button>' +
      '</div>';
  }

  function bodyAndActionsHTML(c) {
    var actions = '';
    if (canEdit(c)) {
      actions += '<button class="comment-btn" onclick="startEditComment()">Edit</button>';
    }
    if (canModerate(c)) {
      actions += '<button class="comment-btn" onclick="resolveComment(\'' + c.id + '\',' + !c.resolved + ')">' +
                 (c.resolved ? 'Unresolve' : 'Resolve') + '</button>' +
                 '<button class="comment-btn comment-btn--danger" onclick="deleteComment(\'' + c.id + '\')">Delete</button>';
    }
    return '<p class="comment-detail-body">' + esc(c.body) + '</p>' +
           (actions ? '<div class="comment-entry-actions">' + actions + '</div>' : '');
  }

  function repliesHTML(replies) {
    if (!replies.length) return '';
    var items = replies.map(function (r) {
      var del = canModerate(r)
        ? '<button class="comment-btn comment-btn--danger" onclick="deleteComment(\'' + r.id + '\')">Delete</button>'
        : '';
      return '<div class="comment-reply">' +
               '<div class="comment-entry-header">' +
                 '<span class="comment-entry-author">' + esc(r.author || 'anonymous') + '</span>' +
               '</div>' +
               '<p class="comment-entry-body">' + esc(r.body) + '</p>' +
               (del ? '<div class="comment-entry-actions">' + del + '</div>' : '') +
             '</div>';
    }).join('');
    return '<div class="comment-replies">' +
             '<p class="comment-replies-title">' + replies.length + ' repl' + (replies.length === 1 ? 'y' : 'ies') + '</p>' +
             items +
           '</div>';
  }

  function replyFormHTML(c) {
    if (!canComment()) return '';
    var name = esc(localStorage.getItem(AUTHOR_KEY) || '');
    return '<div class="comment-reply-form">' +
             '<div class="add-comment-field">' +
               '<label for="comment-reply-input">Reply</label>' +
               '<textarea id="comment-reply-input" rows="2" placeholder="Add a reply…"></textarea>' +
             '</div>' +
             '<div class="add-comment-field">' +
               '<input id="comment-reply-author" type="text" placeholder="Your name (optional)" value="' + name + '">' +
             '</div>' +
             '<div class="add-comment-actions">' +
               '<button id="comment-reply-btn" onclick="submitReply(\'' + c.id + '\')">Reply</button>' +
             '</div>' +
           '</div>';
  }

  function startEditComment() {
    _editing = true;
    renderDetail();
  }

  function cancelEditComment() {
    _editing = false;
    renderDetail();
  }

  function saveEditComment() {
    var ta = document.getElementById('comment-edit-input');
    var body = ta ? ta.value.trim() : '';
    if (!body) { if (ta) ta.focus(); return; }
    if (!_activeId) return;
    patchComment(_activeId, { body: body });
  }

  // submitReply posts a one-level reply. The server requires an anchor on every
  // comment, so the reply inherits the parent's quoted text and offsets.
  function submitReply(parentId) {
    var ta = document.getElementById('comment-reply-input');
    var body = ta ? ta.value.trim() : '';
    if (!body) { if (ta) ta.focus(); return; }
    var parent = byId(parentId);
    if (!parent) return;

    var authorInput = document.getElementById('comment-reply-author');
    var author = authorInput ? authorInput.value.trim() : '';
    if (author) localStorage.setItem(AUTHOR_KEY, author);

    var btn = document.getElementById('comment-reply-btn');
    if (btn) { btn.disabled = true; btn.textContent = 'Replying…'; }

    postComment({
      author: author,
      body: body,
      quoted_text: parent.quoted_text,
      start_char: parent.start_char,
      end_char: parent.end_char,
      parent_id: parentId
    }, function () {
      loadComments();
    }, function () {
      if (btn) { btn.disabled = false; btn.textContent = 'Reply'; }
    });
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

    var roots = rootComments();
    renderToggle(roots);

    var list = document.getElementById('comment-list');
    if (list) list.innerHTML = '';

    roots.forEach(function (c) {
      var range = findTextRange(c.quoted_text, c.start_char, c.end_char);
      renderAnchor(c, range);
      if (list) renderGutterEntry(list, c, range);
    });

    // Keep the detail popout in sync after an edit, reply or resolve.
    if (_activeId) {
      if (byId(_activeId)) { setActiveComment(_activeId); renderDetail(); }
      else closeCommentDetail();
    }

    if (_scrollToId) {
      var id = _scrollToId;
      _scrollToId = null;
      requestAnimationFrame(function () { scrollToAnchor(id); });
    }
  }

  // renderToggle updates the floating button's label, count and action.
  function renderToggle(roots) {
    var toggle = document.getElementById('comment-toggle-btn');
    if (!toggle) return;
    // Nothing to show and nothing to add: keep the button out of the way.
    if (roots.length === 0 && !canComment()) { toggle.hidden = true; return; }
    toggle.hidden = false;
    // Don't clobber the ready state if a selection is active.
    if (toggle.classList.contains('comment-toggle-btn--ready')) return;

    var openCount = roots.filter(function (c) { return !c.resolved; }).length;
    if (roots.length === 0) {
      toggle.textContent = 'Add a review';
      toggle.onclick = showAddCommentHint;
    } else {
      toggle.innerHTML = 'Reviews <span id="comment-count">' + openCount + '</span>';
      toggle.onclick = toggleCommentSidebar;
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
    // Clicking an anchor opens the sidebar and marks the matching entry.
    mark.addEventListener('click', function (e) {
      e.stopPropagation();
      openSidebar();
      setActiveComment(c.id);
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
      (isStale ? ' comment-entry--stale' : '') +
      (c.id === _activeId ? ' comment-entry--selected' : '');
    div.setAttribute('role', 'button');
    div.setAttribute('tabindex', '0');

    var staleHTML = isStale
      ? '<span class="comment-stale-label" title="The document was edited — this text no longer exists">⚠ Text changed</span>'
      : '';
    var replyCount = repliesOf(c.id).length;
    var replyHTML = replyCount
      ? '<span class="comment-reply-count">' + replyCount + ' repl' + (replyCount === 1 ? 'y' : 'ies') + '</span>'
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
      replyHTML;

    div.addEventListener('mouseenter', function () { highlightAnchor(c.id, true); });
    div.addEventListener('mouseleave', function () { highlightAnchor(c.id, false); });
    div.addEventListener('click', function (e) {
      e.stopPropagation();
      openCommentDetail(c.id);
    });
    div.addEventListener('keydown', function (e) {
      if (e.key !== 'Enter' && e.key !== ' ') return;
      e.preventDefault();
      openCommentDetail(c.id);
    });
    list.appendChild(div);
  }

  // setActiveComment marks one comment as selected in both the document
  // and the sidebar, clearing any previous selection. Pass null to clear.
  function setActiveComment(cid) {
    document.querySelectorAll('.comment-entry--selected').forEach(function (el) {
      el.classList.remove('comment-entry--selected');
    });
    document.querySelectorAll('mark.comment-anchor--selected').forEach(function (el) {
      el.classList.remove('comment-anchor--selected');
    });
    if (!cid) return;
    var entry = document.getElementById('comment-entry-' + cid);
    if (entry) entry.classList.add('comment-entry--selected');
    var mark = document.querySelector('mark.comment-anchor[data-cid="' + cid + '"]');
    if (mark) mark.classList.add('comment-anchor--selected');
  }

  function scrollToAnchor(cid) {
    var mark = document.querySelector('mark.comment-anchor[data-cid="' + cid + '"]');
    if (mark) mark.scrollIntoView({ behavior: 'smooth', block: 'center' });
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
    // Unauthenticated visitors can read comments but not create them.
    if (!canComment()) return;
    // Don't fire while a dialog is open — typing in a textarea inside the
    // add form or the detail popout also triggers selectionchange.
    if (isDialogOpen()) return;

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

    if (_isTouchDevice || isMobile()) {
      // Mobile: turn the always-visible toggle button into the tap target.
      // The float button is too small and unreliable next to touch selection handles.
      var toggle = document.getElementById('comment-toggle-btn');
      if (toggle) {
        toggle.hidden = false;
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

  function isDialogOpen() {
    var add = document.getElementById('add-comment-popover');
    var detail = document.getElementById('comment-detail-popover');
    return (add && !add.hidden) || (detail && !detail.hidden);
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
    renderToggle(rootComments());
  }

  // ── Popover placement ─────────────────────────────────────────────────────

  function isMobile() { return window.innerWidth < 640; }

  // openAsSheet shows a popover as a mobile bottom sheet, keeping it above the
  // software keyboard. No body scroll lock — setting position:fixed on <body>
  // creates a new containing block on iOS Safari and hides fixed children.
  // The scrim has touch-action:none to block scroll-through instead.
  function openAsSheet(popover) {
    popover.style.bottom = '0';
    popover.hidden = false;
    if (!window.visualViewport) return null;
    var listener = function () {
      var vv = window.visualViewport;
      var kbHeight = Math.max(0, window.innerHeight - vv.offsetTop - vv.height);
      popover.style.bottom = kbHeight + 'px';
      popover.style.maxHeight = (vv.height * 0.8) + 'px';
    };
    window.visualViewport.addEventListener('resize', listener);
    window.visualViewport.addEventListener('scroll', listener);
    listener();
    if (popover.id === 'add-comment-popover') _vpListener = listener;
    else _detailVpListener = listener;
    return listener;
  }

  // openAsPopover positions a popover near rect, flipping above when it would
  // overflow the bottom of the viewport.
  function openAsPopover(popover, rect) {
    popover.style.bottom = '';
    popover.style.maxHeight = '';
    popover.style.left = '-9999px';
    popover.style.top = '-9999px';
    popover.hidden = false;

    var vw = window.innerWidth;
    var vh = window.innerHeight;
    var pw = popover.offsetWidth || 300;
    var ph = popover.offsetHeight || 220;

    if (!rect) {
      popover.style.left = Math.max(8, (vw - pw) / 2) + 'px';
      popover.style.top = '120px';
      return;
    }
    var left = Math.max(8, Math.min(rect.left + rect.width / 2 - pw / 2, vw - pw - 8));
    var top = rect.bottom + 8;
    if (top + ph > vh - 8) top = Math.max(8, rect.top - ph - 8);
    popover.style.left = left + 'px';
    popover.style.top = top + 'px';
  }

  // detachViewport removes a visualViewport listener and returns null so the
  // caller can clear its handle in one statement.
  function detachViewport(listener) {
    if (listener && window.visualViewport) {
      window.visualViewport.removeEventListener('resize', listener);
      window.visualViewport.removeEventListener('scroll', listener);
    }
    return null;
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
  window.openCommentDetail = openCommentDetail;
  window.closeCommentDetail = closeCommentDetail;
  window.startEditComment = startEditComment;
  window.cancelEditComment = cancelEditComment;
  window.saveEditComment = saveEditComment;
  window.submitReply = submitReply;
})();
