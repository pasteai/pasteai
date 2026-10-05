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
  var _filter = 'open';  // sidebar view: open | resolved | mine
  var _pendingHashId = null; // comment named in the URL fragment, opened once loaded
  var _editing = false; // detail popout is in edit mode
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


  // ── Dialogs ───────────────────────────────────────────────────────────────
  // The page already has a styled modal and a toast; native alert()/confirm()
  // look foreign next to them and block the page.

  var _toastTimer = null;
  function showToast(message) {
    var hint = document.getElementById('comment-hint');
    if (!hint) return;
    hint.textContent = message;
    hint.hidden = false;
    clearTimeout(_toastTimer);
    _toastTimer = setTimeout(function () { hint.hidden = true; }, 4000);
  }

  // confirmAction opens the comment confirmation modal and calls onConfirm if
  // the user accepts. Falls back to window.confirm if the modal is absent.
  function confirmAction(message, confirmLabel, onConfirm) {
    var overlay = document.getElementById('comment-confirm-overlay');
    var msg = document.getElementById('comment-confirm-msg');
    var ok = document.getElementById('comment-confirm-ok');
    var cancel = document.getElementById('comment-confirm-cancel');
    if (!overlay || !msg || !ok || !cancel) {
      if (window.confirm(message)) onConfirm();
      return;
    }
    msg.textContent = message;
    ok.textContent = confirmLabel;

    function close() {
      overlay.classList.remove('open');
      ok.onclick = null;
      cancel.onclick = null;
      document.removeEventListener('keydown', onKey);
    }
    function onKey(e) {
      if (e.key === 'Escape') { e.stopPropagation(); close(); }
    }
    ok.onclick = function (e) { e.stopPropagation(); close(); onConfirm(); };
    cancel.onclick = function (e) { e.stopPropagation(); close(); };
    overlay.classList.add('open');
    document.addEventListener('keydown', onKey);
    cancel.focus();
  }

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
    _pendingHashId = commentIdFromHash();
    loadComments();

    // Someone following a link to a comment expects to land on it even if it
    // is resolved, which the default view hides.
    window.addEventListener('hashchange', function () {
      var id = commentIdFromHash();
      if (id) { _pendingHashId = id; renderAll(); }
    });

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
      // A synchronous re-render (click-to-edit) detaches the clicked node before
      // this fires, so the containment check below would wrongly call it an
      // outside click. Anything already detached came from our own UI.
      if (e.target && e.target.isConnected === false) return;
      // Clicks inside the detail popout must not close the sidebar behind it.
      if (detail && !detail.hidden && (detail === e.target || detail.contains(e.target))) return;
      if (sidebar && !sidebar.hidden &&
          !sidebar.contains(e.target) &&
          toggle && !toggle.contains(e.target)) {
        closeSidebar();
      }
    });
  }

  // showAddCommentHint backs the toggle button while a document has no
  // comments yet. Opening an empty sidebar says nothing, so point at the one
  // thing that starts a comment instead.
  function showAddCommentHint() {
    showToast('Select some text in the document to comment on it.');
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
      showToast('That selection is no longer valid — select the text again.');
      cancelAddComment();
      return;
    }

    setSubmitLoading(true);

    // No author field: the server attributes the comment to the signed-in user.
    postComment({
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
        showToast('Please sign in to comment.');
        onError();
        return;
      }
      showToast('Could not save that comment — try again.');
      onError();
    }).catch(function () {
      showToast('Network error — try again.');
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
      else if (r.status === 403) { showToast('You cannot change that comment.'); }
      else { showToast('Could not update the comment — try again.'); }
    }).catch(function () { showToast('Network error — try again.'); });
  }

  function deleteComment(cid) {
    var replyCount = repliesOf(cid).length;
    var msg = replyCount
      ? 'Delete this comment and its ' + replyCount + ' repl' + (replyCount === 1 ? 'y' : 'ies') + '?'
      : 'Delete this comment?';

    confirmAction(msg, 'Delete', function () {
      // Replies go first so no orphans remain if the parent delete fails.
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
          showToast('You cannot delete that comment.');
        } else {
          showToast('Could not delete the comment — try again.');
        }
      }).catch(function () { showToast('Network error — try again.'); });
    });
  }

  // ── Detail popout ─────────────────────────────────────────────────────────

  // openCommentDetail shows a comment, its replies and the reply form.
  // Called from the sidebar; also scrolls the document to the anchor.
  // openComment is the single way into a comment. The highlight in the document
  // and the sidebar entry both land here, so clicking either gives the same
  // result: sidebar open, popout open, document scrolled to the anchor and the
  // entry brought into view in a long list.
  function openComment(cid) {
    openSidebar();
    openCommentDetail(cid);
    requestAnimationFrame(function () {
      var entry = document.getElementById('comment-entry-' + cid);
      if (entry) entry.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    });
  }

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
      var mark = document.querySelector('mark.comment-anchor[data-cids~="' + c.id + '"]');
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
        '<button class="comment-btn comment-btn--link" onclick="copyCommentLink(\'' + c.id + '\')" ' +
          'title="Copy a link to this comment">Link</button>' +
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
    if (canModerate(c)) {
      actions += '<button class="comment-btn" onclick="resolveComment(\'' + c.id + '\',' + !c.resolved + ')">' +
                 (c.resolved ? 'Unresolve' : 'Resolve') + '</button>' +
                 '<button class="comment-btn comment-btn--danger" onclick="deleteComment(\'' + c.id + '\')">Delete</button>';
    }
    // The author edits by clicking their own text — no separate Edit button.
    var editable = canEdit(c);
    return '<p class="comment-detail-body' + (editable ? ' comment-detail-body--editable' : '') + '"' +
             (editable ? ' onclick="startEditComment()" title="Click to edit"' : '') + '>' +
             esc(c.body) +
           '</p>' +
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
    return '<div class="comment-reply-form">' +
             '<textarea id="comment-reply-input" rows="2" placeholder="Add a reply…"></textarea>' +
             '<div class="add-comment-actions">' +
               '<button id="comment-reply-btn" onclick="submitReply(\'' + c.id + '\')">Reply</button>' +
             '</div>' +
           '</div>';
  }

  // copyCommentLink puts a deep link to one comment on the clipboard, so review
  // feedback can be pointed at directly.
  function copyCommentLink(cid) {
    var url = window.location.origin + window.location.pathname + '#comment-' + cid;
    var done = function () { showToast('Link to this comment copied.'); };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(url).then(done, function () { showToast(url); });
      return;
    }
    showToast(url);
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

    var btn = document.getElementById('comment-reply-btn');
    if (btn) { btn.disabled = true; btn.textContent = 'Replying…'; }

    postComment({
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
      p.normalize();
    });

    var flat = flattenArticle();
    var placed = [];
    rootComments().forEach(function (c) {
      var at = flat ? locateQuote(flat, c) : null;
      placed.push({ c: c, at: at });
    });

    // Document order, the way every comparable tool lists comments. Stale ones
    // have no position, so they sink to the bottom rather than sorting randomly.
    placed.sort(function (a, b) {
      if (!a.at && !b.at) return 0;
      if (!a.at) return 1;
      if (!b.at) return -1;
      return a.at.start - b.at.start;
    });

    paintAnchors(placed);
    renderToggle(placed);
    renderSidebar(placed);

    // Keep the detail popout in sync after an edit, reply or resolve.
    if (_activeId) {
      if (byId(_activeId)) { setActiveComment(_activeId); renderDetail(); }
      else closeCommentDetail();
    }

    if (_pendingHashId) {
      openPendingHashComment();
      return;
    }

    if (_scrollToId) {
      var id = _scrollToId;
      _scrollToId = null;
      requestAnimationFrame(function () { scrollToAnchor(id); });
    }
  }

  // commentIdFromHash reads a #comment-<id> deep link.
  function commentIdFromHash() {
    var m = /^#comment-(.+)$/.exec(window.location.hash || '');
    return m ? m[1] : null;
  }

  // openPendingHashComment honours a deep link after the comments have loaded,
  // switching the filter if the target is hidden by the current view.
  function openPendingHashComment() {
    var id = _pendingHashId;
    _pendingHashId = null;
    var c = byId(id);
    if (!c) return;
    if (!matchesFilter(c)) {
      _filter = c.resolved ? 'resolved' : 'open';
      renderAll();
    }
    openComment(id);
  }

  // ── Anchoring ─────────────────────────────────────────────────────────────

  // flattenArticle concatenates the article's text nodes so quotes can be
  // located by offset. Wrapping anchors never changes this text, only the node
  // boundaries, so offsets stay valid while anchors are painted.
  function flattenArticle() {
    var article = document.querySelector('article.markdown-body');
    if (!article) return null;
    var walker = document.createTreeWalker(article, NodeFilter.SHOW_TEXT);
    var nodes = [];
    var full = '';
    var node;
    while ((node = walker.nextNode())) {
      nodes.push({ node: node, start: full.length });
      full += node.nodeValue;
    }
    return { article: article, nodes: nodes, full: full };
  }

  // locateQuote finds where a comment's quoted text sits now. `exact` is false
  // when the stored offsets no longer line up AND the text appears more than
  // once — we are then guessing which occurrence was meant, and the UI says so
  // rather than presenting the guess as certain.
  function locateQuote(flat, c) {
    var text = c.quoted_text;
    if (!text) return null;
    if (c.start_char >= 0 && c.end_char > c.start_char &&
        flat.full.slice(c.start_char, c.end_char) === text) {
      return { start: c.start_char, end: c.end_char, exact: true };
    }
    var idx = flat.full.indexOf(text);
    if (idx < 0) return null;
    return { start: idx, end: idx + text.length, exact: flat.full.indexOf(text, idx + 1) < 0 };
  }

  // paintAnchors wraps every located quote in a <mark>.
  //
  // Overlapping comments are split at their boundaries first, so one anchor can
  // never tear another's <mark> apart. A segment covered by several comments
  // carries all their ids, and lookups use [data-cids~="id"] because a single
  // comment may now span more than one element.
  function paintAnchors(placed) {
    var located = placed.filter(function (p) { return p.at; });
    if (!located.length) return;

    var bounds = {};
    located.forEach(function (p) { bounds[p.at.start] = true; bounds[p.at.end] = true; });
    var edges = Object.keys(bounds).map(Number).sort(function (a, b) { return a - b; });

    var segments = [];
    for (var i = 0; i < edges.length - 1; i++) {
      var from = edges[i];
      var to = edges[i + 1];
      var covering = located.filter(function (p) { return p.at.start <= from && p.at.end >= to; });
      if (covering.length) segments.push({ start: from, end: to, covering: covering });
    }

    // Back to front: wrapping mutates the DOM, and later segments must be
    // placed before the node boundaries they depend on are rewritten.
    segments.sort(function (a, b) { return b.start - a.start; });
    segments.forEach(wrapSegment);
  }

  function wrapSegment(seg) {
    var flat = flattenArticle();
    if (!flat) return;
    var range = rangeFromOffsets(flat, seg.start, seg.end);
    if (!range) return;

    var comments = seg.covering.map(function (p) { return p.c; });
    var allResolved = comments.every(function (c) { return c.resolved; });
    var anyFuzzy = seg.covering.some(function (p) { return !p.at.exact; });

    var mark = document.createElement('mark');
    mark.className = 'comment-anchor' +
      (allResolved ? ' comment-anchor--resolved' : '') +
      (anyFuzzy ? ' comment-anchor--fuzzy' : '');
    mark.dataset.cid = comments[0].id;
    mark.dataset.cids = comments.map(function (c) { return c.id; }).join(' ');
    if (anyFuzzy) mark.title = 'The document changed — this may not be the passage that was commented on';

    try {
      range.surroundContents(mark);
    } catch (_) {
      // Cross-element range (spans a <strong> or <code> boundary).
      try {
        mark.appendChild(range.extractContents());
        range.insertNode(mark);
      } catch (_2) {
        return;
      }
    }

    var cid = comments[0].id;
    mark.addEventListener('mouseenter', function () { highlightEntry(cid, true); });
    mark.addEventListener('mouseleave', function () { highlightEntry(cid, false); });
    mark.addEventListener('click', function (e) {
      e.stopPropagation();
      openComment(cid);
    });
  }

  function rangeFromOffsets(flat, start, end) {
    var range = document.createRange();
    var started = false;
    for (var i = 0; i < flat.nodes.length; i++) {
      var n = flat.nodes[i];
      var nEnd = n.start + n.node.nodeValue.length;
      if (!started && start < nEnd) { range.setStart(n.node, start - n.start); started = true; }
      if (started && end <= nEnd) { range.setEnd(n.node, end - n.start); return range; }
    }
    return null;
  }

  // rangeToCharOffsets is the inverse of rangeFromOffsets: where a live
  // selection sits in the flattened article text, so the offsets can be stored
  // with the comment and the quote re-found on a later visit.
  //
  // Measured with a second range rather than by walking flat.nodes, because a
  // selection boundary may land on an element (dragging past the end of a
  // paragraph gives an element container and a child index, not a text node).
  // Range.toString() concatenates exactly the text nodes flattenArticle walks,
  // so the two agree on what an offset counts.
  //
  // The caller trims the quote before storing it, so the offsets are trimmed to
  // match — otherwise locateQuote's exact-offset check fails on any selection
  // with whitespace at an edge and it falls back to searching by text.
  function rangeToCharOffsets(range, article) {
    var before = document.createRange();
    before.selectNodeContents(article);
    before.setEnd(range.startContainer, range.startOffset);

    var raw = range.toString();
    var lead = raw.length - raw.replace(/^\s+/, '').length;
    var trail = raw.length - raw.replace(/\s+$/, '').length;

    var start = before.toString().length + lead;
    return { start: start, end: start + (raw.length - lead - trail) };
  }

  // ── Sidebar ───────────────────────────────────────────────────────────────

  // renderToggle updates the floating button's label, count and action.
  function renderToggle(placed) {
    var toggle = document.getElementById('comment-toggle-btn');
    if (!toggle) return;
    // Nothing to show and nothing to add: keep the button out of the way.
    if (placed.length === 0 && !canComment()) { toggle.hidden = true; return; }
    toggle.hidden = false;
    // Don't clobber the ready state if a selection is active.
    if (toggle.classList.contains('comment-toggle-btn--ready')) return;

    var openCount = placed.filter(function (p) { return !p.c.resolved; }).length;
    if (placed.length === 0) {
      toggle.textContent = 'Add a comment';
      toggle.onclick = showAddCommentHint;
    } else {
      toggle.innerHTML = 'Comments <span id="comment-count">' + openCount + '</span>';
      toggle.onclick = toggleCommentSidebar;
    }
  }

  // matchesFilter decides whether a comment belongs in the current view.
  // Resolved comments drop out of the default view, which is what makes
  // resolving feel like it did something.
  function matchesFilter(c) {
    if (_filter === 'resolved') return !!c.resolved;
    if (_filter === 'mine') return !!c.is_mine;
    return !c.resolved;
  }

  function renderSidebar(placed) {
    var list = document.getElementById('comment-list');
    if (!list) return;
    list.innerHTML = '';

    renderFilters(placed);

    var shown = placed.filter(function (p) { return matchesFilter(p.c); });
    if (!shown.length) {
      list.innerHTML = '<p class="comment-empty">' + esc(emptyMessage()) + '</p>';
      return;
    }
    shown.forEach(function (p) { renderGutterEntry(list, p.c, p.at); });
  }

  function emptyMessage() {
    if (_filter === 'resolved') return 'No resolved comments.';
    if (_filter === 'mine') return 'You have not commented on this document.';
    return 'No open comments.';
  }

  function renderFilters(placed) {
    var bar = document.getElementById('comment-filters');
    if (!bar) return;
    var counts = {
      open: placed.filter(function (p) { return !p.c.resolved; }).length,
      resolved: placed.filter(function (p) { return p.c.resolved; }).length,
      mine: placed.filter(function (p) { return p.c.is_mine; }).length
    };
    var chips = [['open', 'Open'], ['resolved', 'Resolved'], ['mine', 'Mine']];
    bar.innerHTML = chips.map(function (f) {
      var key = f[0];
      return '<button class="comment-filter' + (_filter === key ? ' comment-filter--on' : '') +
             '" data-filter="' + key + '">' + f[1] + ' ' + counts[key] + '</button>';
    }).join('');
    bar.querySelectorAll('.comment-filter').forEach(function (btn) {
      btn.addEventListener('click', function (e) {
        e.stopPropagation();
        _filter = btn.getAttribute('data-filter');
        renderAll();
      });
    });
  }

  function renderGutterEntry(list, c, at) {
    var isStale = !at;
    var isFuzzy = !!at && !at.exact;
    var div = document.createElement('div');
    div.id = 'comment-entry-' + c.id;
    div.className = 'comment-entry' +
      (c.resolved ? ' comment-entry--resolved' : '') +
      (isStale ? ' comment-entry--stale' : '') +
      (c.id === _activeId ? ' comment-entry--selected' : '');
    div.setAttribute('role', 'button');
    div.setAttribute('tabindex', '0');

    div.innerHTML =
      '<div class="comment-entry-header">' +
        '<span class="comment-entry-author">' + esc(c.author || 'anonymous') + '</span>' +
        '<span class="comment-entry-time" title="' + esc(c.created_at || '') + '">' +
          esc(relativeTime(c.created_at)) +
        '</span>' +
        anchorStateHTML(c, isStale, isFuzzy) +
      '</div>' +
      '<blockquote class="comment-entry-quote" title="' + esc(c.quoted_text) + '">' +
        esc(truncate(c.quoted_text, 80)) +
      '</blockquote>' +
      '<p class="comment-entry-body">' + esc(c.body) + '</p>' +
      replyPreviewHTML(c);

    div.addEventListener('mouseenter', function () { highlightAnchor(c.id, true); });
    div.addEventListener('mouseleave', function () { highlightAnchor(c.id, false); });
    div.addEventListener('click', function (e) {
      e.stopPropagation();
      openComment(c.id);
    });
    div.addEventListener('keydown', function (e) {
      if (e.key !== 'Enter' && e.key !== ' ') return;
      e.preventDefault();
      openComment(c.id);
    });
    list.appendChild(div);
  }

  // anchorStateHTML explains a lost or uncertain anchor, naming the version the
  // comment was written against so it is a lead rather than a dead end.
  function anchorStateHTML(c, isStale, isFuzzy) {
    if (isStale) {
      var since = c.revision_num ? ' since v' + c.revision_num : '';
      var label = '⚠ Text changed' + since;
      return '<span class="comment-stale-label" title="The passage this comment quoted is no longer in the document">' +
             esc(label) + revisionLinkHTML(c) + '</span>';
    }
    if (isFuzzy) {
      return '<span class="comment-stale-label comment-stale-label--fuzzy" ' +
             'title="The document changed and this text appears more than once — the highlight may be on the wrong one">' +
             '≈ Moved?</span>';
    }
    return '';
  }

  function revisionLinkHTML(c) {
    if (!window._pasteaiShowRevisions) return '';
    return ' <a class="comment-revision-link" href="/d/' + esc(_docId) + '/revisions" ' +
           'onclick="event.stopPropagation()">history</a>';
  }

  // replyPreviewHTML shows the start of the thread in the sidebar, so reading a
  // short reply does not need a second click.
  function replyPreviewHTML(c) {
    var replies = repliesOf(c.id);
    if (!replies.length) return '';
    // Tally first: only two replies are previewed, so without it a thread of
    // ten reads as a thread of two.
    var tally = '<p class="comment-reply-tally">' + replies.length +
                ' repl' + (replies.length === 1 ? 'y' : 'ies') + '</p>';
    var shown = replies.slice(0, 2).map(function (r) {
      return '<div class="comment-reply-preview">' +
               '<span class="comment-entry-author">' + esc(r.author || 'anonymous') + '</span> ' +
               esc(truncate(r.body, 70)) +
             '</div>';
    }).join('');
    var more = replies.length > 2
      ? '<span class="comment-reply-count">' + (replies.length - 2) + ' more</span>'
      : '';
    return tally + shown + more;
  }

  // relativeTime renders an ISO timestamp the way every comparable tool does.
  function relativeTime(iso) {
    if (!iso) return '';
    var then = Date.parse(iso);
    if (isNaN(then)) return '';
    var secs = Math.floor((Date.now() - then) / 1000);
    if (secs < 60) return 'just now';
    var mins = Math.floor(secs / 60);
    if (mins < 60) return mins + 'm ago';
    var hours = Math.floor(mins / 60);
    if (hours < 24) return hours + 'h ago';
    var days = Math.floor(hours / 24);
    if (days < 30) return days + 'd ago';
    return new Date(then).toLocaleDateString();
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
    document.querySelectorAll('mark.comment-anchor[data-cids~="' + cid + '"]').forEach(function (m) {
      m.classList.add('comment-anchor--selected');
    });
  }

  function scrollToAnchor(cid) {
    var mark = document.querySelector('mark.comment-anchor[data-cids~="' + cid + '"]');
    if (mark) mark.scrollIntoView({ behavior: 'smooth', block: 'center' });
  }

  function highlightEntry(cid, on) {
    var el = document.getElementById('comment-entry-' + cid);
    if (el) el.classList.toggle('comment-entry--active', on);
  }

  function highlightAnchor(cid, on) {
    document.querySelectorAll('mark.comment-anchor[data-cids~="' + cid + '"]').forEach(function (el) {
      el.classList.toggle('comment-anchor--active', on);
    });
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
    if (!text) { hideFloatBtn(); return; }
    if (text.length > MAX_QUOTE) {
      // Silently offering nothing reads as "commenting is broken".
      hideFloatBtn();
      warnSelectionTooLong();
      return;
    }

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

  // warnSelectionTooLong explains the cap, at most once every few seconds —
  // selectionchange fires continuously while dragging.
  var _lastLongWarning = 0;
  function warnSelectionTooLong() {
    var now = Date.now();
    if (now - _lastLongWarning < 5000) return;
    _lastLongWarning = now;
    showToast('That selection is too long — comment on up to ' + MAX_QUOTE + ' characters.');
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
    toggle.setAttribute('aria-label', 'Open comments');
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
    if (top + ph > vh - 8) top = rect.top - ph - 8;

    // rect can be outside the viewport entirely — the page scrolled between
    // selecting and clicking, or the quote sits past the fold — and both
    // branches above then place the popover where its buttons cannot be
    // reached. Clamp last so it is always on screen, and cap the height so a
    // tall popover scrolls inside itself rather than off the bottom.
    popover.style.maxHeight = (vh - 16) + 'px';
    ph = Math.min(ph, vh - 16);
    top = Math.max(8, Math.min(top, vh - ph - 8));


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
  window.copyCommentLink = copyCommentLink;
})();
