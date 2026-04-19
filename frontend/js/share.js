// Share view renderer + modal logic. Depends on window.go.main.ShareService
// and the vendored qrcode global.
(function () {
  const pubList = document.getElementById('share-publications-list');
  const followList = document.getElementById('share-follows-list');
  const shareIndicator = document.getElementById('share-indicator');

  const createModal = document.getElementById('create-share-modal');
  const pickerSec = document.getElementById('create-share-picker-section');
  const resultSec = document.getElementById('create-share-result-section');
  const tagSelect = document.getElementById('create-share-tag-select');
  const stringBox = document.getElementById('create-share-string-box');
  const qrBox = document.getElementById('create-share-qr-box');
  const confirmBtn = document.getElementById('create-share-confirm-btn');
  const copyBtn = document.getElementById('create-share-copy-btn');
  const qrToggleBtn = document.getElementById('create-share-qr-toggle-btn');

  const followModal = document.getElementById('follow-share-modal');
  const followString = document.getElementById('follow-share-string');
  const followTagSec = document.getElementById('follow-share-tag-section');
  const followTagInput = document.getElementById('follow-share-local-tag');
  const followErr = document.getElementById('follow-share-error');
  const followConfirmBtn = document.getElementById('follow-share-confirm-btn');
  const followInvalid = document.getElementById('follow-share-invalid');
  const followSelf = document.getElementById('follow-share-self');
  const followConnecting = document.getElementById('follow-share-connecting');
  const followUnreachable = document.getElementById('follow-share-unreachable');
  const followUnreachableMsg = document.getElementById('follow-share-unreachable-msg');
  const followRetryBtn = document.getElementById('follow-share-retry-btn');
  const followAnywayBtn = document.getElementById('follow-share-follow-anyway-btn');

  const addShareBtn = document.getElementById('add-share-btn');
  const addFollowBtn = document.getElementById('add-follow-btn');

  function escapeHTML(s) {
    return String(s).replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
  }

  function relTime(sec) {
    const d = Math.floor(Date.now() / 1000) - sec;
    if (d < 60) return `${d}s ago`;
    if (d < 3600) return `${Math.floor(d / 60)}m ago`;
    if (d < 86400) return `${Math.floor(d / 3600)}h ago`;
    return `${Math.floor(d / 86400)}d ago`;
  }

  function updateIndicator(hasAny) {
    if (!shareIndicator) return;
    if (hasAny) shareIndicator.classList.remove('hidden');
    else shareIndicator.classList.add('hidden');
  }

  function renderPublications(shares) {
    pubList.innerHTML = '';
    shares.forEach(s => {
      const li = document.createElement('li');
      li.className = 'bg-white border border-stone-200 rounded-md px-4 py-3 flex items-center justify-between gap-3';
      const invalidBadge = s.status === 'invalid' ? ' <span class="text-amber-700">(Re-share needed)</span>' : '';
      li.innerHTML = `
        <div class="min-w-0 flex-1">
          <div class="text-sm font-medium text-stone-800 truncate">${escapeHTML(s.tag_name)}${invalidBadge}</div>
          <div class="text-[11px] text-stone-500">${s.followers} followers · ${s.clips_pushed} clips pushed · since ${relTime(s.created_at)}</div>
        </div>
        <div class="flex gap-2 shrink-0">
          <button class="share-copy-link border border-stone-200 hover:bg-stone-100 text-stone-600 text-[11px] font-medium py-1.5 px-3 rounded-md" data-id="${s.id}" data-share="${escapeHTML(s.share_string)}">Copy link</button>
          <button class="share-stop border border-stone-200 hover:bg-red-50 hover:border-red-300 text-stone-600 hover:text-red-600 text-[11px] font-medium py-1.5 px-3 rounded-md" data-tagid="${s.tag_id}">Stop</button>
        </div>`;
      pubList.appendChild(li);
    });
  }

  function renderFollows(follows) {
    followList.innerHTML = '';
    follows.forEach(f => {
      const li = document.createElement('li');
      li.className = 'bg-white border border-stone-200 rounded-md px-4 py-3 flex items-center justify-between gap-3';
      const status = f.status === 'connected' ? '<span class="inline-block w-1.5 h-1.5 rounded-full bg-emerald-500 mr-1.5"></span>Connected'
        : f.status === 'connected_relayed' ? '<span class="inline-block w-1.5 h-1.5 rounded-full bg-emerald-500 mr-1.5"></span>Connected (relayed)'
        : '<span class="inline-block w-1.5 h-1.5 rounded-full bg-stone-400 mr-1.5"></span>Offline · will resume';
      li.innerHTML = `
        <div class="min-w-0 flex-1">
          <div class="text-sm font-medium text-stone-800 truncate">${escapeHTML(f.local_tag_name)}</div>
          <div class="text-[11px] text-stone-500">${status} · ${f.clips_received} clips received · since ${relTime(f.created_at)}</div>
        </div>
        <div class="flex gap-2 shrink-0">
          <button class="share-edit-follow border border-stone-200 hover:border-stone-300 hover:bg-stone-100 text-stone-600 text-[11px] font-medium py-1.5 px-3 rounded-md" data-id="${f.id}" data-tag="${escapeHTML(f.local_tag_name)}">Edit</button>
          <button class="share-unfollow border border-stone-200 hover:bg-red-50 hover:border-red-300 text-stone-600 hover:text-red-600 text-[11px] font-medium py-1.5 px-3 rounded-md" data-id="${f.id}">Unfollow</button>
        </div>`;
      followList.appendChild(li);
    });
  }

  async function refresh() {
    try {
      const status = await window.go.main.ShareService.GetShareStatus();
      renderPublications(status.shares || []);
      renderFollows(status.follows || []);
      updateIndicator((status.shares && status.shares.length > 0) || (status.follows && status.follows.length > 0));
    } catch (e) {
      console.error('share: refresh failed', e);
    }
  }

  // Open create-share modal: fill tag picker fresh each time.
  addShareBtn.addEventListener('click', async () => {
    try {
      const tags = await window.go.main.App.GetTags();
      tagSelect.innerHTML = '';
      (tags || []).forEach(t => {
        const o = document.createElement('option');
        o.value = t.id;
        o.textContent = t.name;
        tagSelect.appendChild(o);
      });
      pickerSec.classList.remove('hidden');
      resultSec.classList.add('hidden');
      qrBox.classList.add('hidden');
      qrBox.innerHTML = '';
      createModal.classList.remove('hidden');
    } catch (e) {
      console.error(e);
    }
  });

  document.querySelectorAll('.create-share-close').forEach(b => b.addEventListener('click', () => {
    createModal.classList.add('hidden');
  }));

  confirmBtn.addEventListener('click', async () => {
    const tagID = parseInt(tagSelect.value, 10);
    if (!(tagID > 0)) {
      if (typeof showToast === 'function') showToast('Pick a tag first', 'error');
      return;
    }
    try {
      const info = await window.go.main.ShareService.StartShare(tagID);
      stringBox.textContent = info.share_string;
      pickerSec.classList.add('hidden');
      resultSec.classList.remove('hidden');
      await refresh();
    } catch (e) {
      // Surface the Go error via toast — alert() is unreliable in the
      // Wails webview on macOS and silently swallows the message.
      const msg = (e && e.message) ? e.message : String(e);
      console.error('share: StartShare failed', e);
      if (typeof showToast === 'function') showToast(msg, 'error');
    }
  });

  copyBtn.addEventListener('click', async () => {
    const text = stringBox.textContent;
    try {
      await navigator.clipboard.writeText(text);
      copyBtn.textContent = 'Copied ✓';
      setTimeout(() => copyBtn.textContent = '📋 Copy link', 1500);
    } catch (e) {
      console.error('copy failed', e);
    }
  });

  qrToggleBtn.addEventListener('click', () => {
    if (!qrBox.classList.contains('hidden')) {
      qrBox.classList.add('hidden');
      qrBox.innerHTML = '';
      qrToggleBtn.textContent = 'Show QR';
      return;
    }
    const text = stringBox.textContent;
    // typeNumber 0 + level 'L' sometimes picks a version that can't hold
    // the share string (~110+ base64 chars). Use level 'M' + auto version
    // and let the generator pick a fitting size.
    const qr = window.qrcode(0, 'M');
    qr.addData(text);
    qr.make();
    // scalable:true emits an SVG without width/height so the wrapping
    // container must size it. Give the SVG explicit cellSize so it has
    // real pixel dimensions even inside a w-fit container.
    qrBox.innerHTML = qr.createSvgTag({ cellSize: 4, margin: 2 });
    qrBox.classList.remove('hidden');
    qrToggleBtn.textContent = 'Hide QR';
  });

  // Publication list actions (delegation).
  pubList.addEventListener('click', async (e) => {
    const copy = e.target.closest('.share-copy-link');
    const stop = e.target.closest('.share-stop');
    if (copy) {
      try {
        await navigator.clipboard.writeText(copy.dataset.share);
        copy.textContent = 'Copied ✓';
        setTimeout(() => copy.textContent = 'Copy link', 1500);
      } catch (err) {
        console.error(err);
      }
      return;
    }
    if (stop) {
      const tagID = parseInt(stop.dataset.tagid, 10);
      // Two-step confirm: first click asks, second click within 4s commits.
      // Native confirm() is unreliable inside the Wails webview on macOS.
      if (stop.dataset.arm !== '1') {
        stop.dataset.arm = '1';
        const orig = stop.textContent;
        stop.textContent = 'Click again to stop';
        const timer = setTimeout(() => {
          stop.dataset.arm = '';
          stop.textContent = orig;
        }, 4000);
        stop.dataset.armTimer = String(timer);
        return;
      }
      clearTimeout(parseInt(stop.dataset.armTimer, 10));
      try {
        await window.go.main.ShareService.StopShare(tagID);
        if (typeof showToast === 'function') showToast('Share stopped');
        await refresh();
      } catch (err) {
        const msg = (err && err.message) ? err.message : String(err);
        console.error('share: StopShare failed', err);
        if (typeof showToast === 'function') showToast('Stop failed: ' + msg, 'error');
      }
    }
  });

  // --- Follow modal ---

  // Client-side format check for the pasted share string. Keeps bogus input
  // from firing a backend round-trip. The server does real validation.
  function parseShareStringClientSide(s) {
    if (!s || !s.startsWith('mp-share:v1:')) return null;
    const blob = s.substring('mp-share:v1:'.length);
    if (!/^[A-Za-z0-9_-]+$/.test(blob)) return null;
    return true;
  }

  // Translate a raw error message from the Wails boundary into a UI state.
  // Wails serializes Go errors to plain strings so we pattern-match on
  // stable substrings produced by share_manager.go.
  function mapShareError(raw) {
    const s = String((raw && raw.message) || raw || '').toLowerCase();
    if (s.includes('cannot follow your own share')) {
      return { kind: 'self', msg: "This is your own share — you can't follow yourself." };
    }
    if (s.includes('invalid share string') || s.includes('peer id')) {
      return { kind: 'invalid', msg: 'Not a valid share link.' };
    }
    if (s.includes('initial dial') || s.includes('dht find peer') ||
        s.includes('dial') || s.includes('connect')) {
      return { kind: 'unreachable', msg: "Can't reach that peer right now. They may be offline or on a different network." };
    }
    return { kind: 'generic', msg: String((raw && raw.message) || raw || 'Unknown error') };
  }

  // Modal state machine. `followAnywayArmed` is a sticky flag that lets the
  // user proceed through the tag-picker after an unreachable error.
  let followState = 'idle';
  let followAnywayArmed = false;
  let latestReqID = 0;         // race guard — stale TestFollowConnection results are discarded
  let connectDebounceTimer = null;
  let autocompleteHandle = null;

  function hide(...els) { els.forEach(el => el && el.classList.add('hidden')); }
  function show(...els) { els.forEach(el => el && el.classList.remove('hidden')); }

  function updateConfirmEnabled() {
    const tagOK = window.TagAutocomplete && window.TagAutocomplete.isCreatableName(followTagInput.value);
    const canCommit = (followState === 'connected' || (followState === 'unreachable' && followAnywayArmed));
    followConfirmBtn.disabled = !(canCommit && tagOK);
  }

  function setFollowState(s, ctx) {
    followState = s;
    hide(followInvalid, followSelf, followConnecting, followUnreachable, followTagSec, followErr);
    followAnywayArmed = false;

    switch (s) {
      case 'idle':
        break;
      case 'invalidFormat':
        show(followInvalid);
        break;
      case 'connecting':
        show(followConnecting);
        break;
      case 'connected':
        show(followTagSec);
        // Focus the tag input so the user can start typing immediately.
        // RAF ensures the element is visible before the browser processes focus.
        requestAnimationFrame(() => followTagInput.focus());
        break;
      case 'selfFollow':
        show(followSelf);
        break;
      case 'unreachable':
        if (ctx && ctx.msg) followUnreachableMsg.textContent = ctx.msg;
        show(followUnreachable);
        // followAnywayArmed is set separately by the click handler; when armed
        // we also reveal the tag section.
        break;
      case 'committing':
        // Visible blocks stay as-is; just lock the inputs.
        break;
    }
    updateConfirmEnabled();
  }

  function armFollowAnyway() {
    if (followState !== 'unreachable') return;
    followAnywayArmed = true;
    show(followTagSec);
    requestAnimationFrame(() => followTagInput.focus());
    updateConfirmEnabled();
  }

  function resetFollowModal() {
    latestReqID++;      // invalidate any pending dial
    clearTimeout(connectDebounceTimer);
    followString.value = '';
    followTagInput.value = '';
    followAnywayArmed = false;
    followConfirmBtn.textContent = 'Follow';
    setFollowState('idle');
    if (autocompleteHandle) { autocompleteHandle.destroy(); autocompleteHandle = null; }
  }

  async function runConnect(shareStr) {
    const my = ++latestReqID;
    setFollowState('connecting');
    try {
      await window.go.main.ShareService.TestFollowConnection(shareStr);
      if (my !== latestReqID) return;
      setFollowState('connected');
    } catch (e) {
      if (my !== latestReqID) return;
      const mapped = mapShareError(e);
      if (mapped.kind === 'self') setFollowState('selfFollow');
      else if (mapped.kind === 'invalid') setFollowState('invalidFormat');
      else setFollowState('unreachable', { msg: mapped.msg });
    }
  }

  // --- Listeners ---

  addFollowBtn.addEventListener('click', () => {
    resetFollowModal();
    followModal.classList.remove('hidden');
    // Attach autocomplete once per open — it destroys on close.
    if (window.TagAutocomplete && !autocompleteHandle) {
      autocompleteHandle = window.TagAutocomplete.attach(followTagInput, {
        getTags: async () => window.go.main.App.GetTags(),
        onSelect: (val) => {
          followTagInput.value = val;
          updateConfirmEnabled();
        },
      });
    }
  });

  document.querySelectorAll('.follow-share-close').forEach(b => b.addEventListener('click', () => {
    followModal.classList.add('hidden');
    resetFollowModal();
  }));

  followString.addEventListener('input', () => {
    // Bump immediately so any in-flight dial is discarded the moment the user edits.
    latestReqID++;
    clearTimeout(connectDebounceTimer);

    const val = followString.value.trim();
    if (!val) { setFollowState('idle'); return; }
    if (!parseShareStringClientSide(val)) { setFollowState('invalidFormat'); return; }

    // 400ms debounce before hitting the backend — pastes land as a single
    // input event so the delay only matters if the user keeps typing.
    connectDebounceTimer = setTimeout(() => runConnect(val), 400);
  });

  followTagInput.addEventListener('input', updateConfirmEnabled);

  followRetryBtn.addEventListener('click', () => {
    const val = followString.value.trim();
    if (!val || !parseShareStringClientSide(val)) return;
    runConnect(val);
  });

  followAnywayBtn.addEventListener('click', () => {
    armFollowAnyway();
  });

  followConfirmBtn.addEventListener('click', async () => {
    const s = followString.value.trim();
    const tagName = followTagInput.value.trim();
    if (!tagName) {
      followErr.textContent = 'Local tag name required';
      show(followErr);
      return;
    }
    const usingFallback = (followState === 'unreachable' && followAnywayArmed);
    const prevState = followState;
    setFollowState('committing');
    followConfirmBtn.disabled = true;
    const origLabel = followConfirmBtn.textContent;
    followConfirmBtn.textContent = 'Following…';
    try {
      if (usingFallback) {
        await window.go.main.ShareService.FollowWithoutDial(s, tagName);
      } else {
        await window.go.main.ShareService.Follow(s, tagName);
      }
      followModal.classList.add('hidden');
      resetFollowModal();
      await refresh();
    } catch (e) {
      // Restore state so the user can adjust input and retry. Show the raw
      // (mapped) error inline; if it's an unreachable kind, also re-reveal
      // the unreachable block with Retry/Follow-anyway.
      const mapped = mapShareError(e);
      setFollowState(prevState);
      followConfirmBtn.textContent = origLabel;
      if (mapped.kind === 'unreachable') {
        setFollowState('unreachable', { msg: mapped.msg });
      } else {
        followErr.textContent = mapped.msg;
        show(followErr);
      }
      updateConfirmEnabled();
    }
  });

  followList.addEventListener('click', async (e) => {
    const edit = e.target.closest('.share-edit-follow');
    if (edit) {
      openEditFollowModal(parseInt(edit.dataset.id, 10), edit.dataset.tag || '');
      return;
    }
    const un = e.target.closest('.share-unfollow');
    if (!un) return;
    const id = parseInt(un.dataset.id, 10);
    // Same two-step confirm as share-stop: first click arms, second commits.
    if (un.dataset.arm !== '1') {
      un.dataset.arm = '1';
      const orig = un.textContent;
      un.textContent = 'Click again to unfollow';
      const timer = setTimeout(() => {
        un.dataset.arm = '';
        un.textContent = orig;
      }, 4000);
      un.dataset.armTimer = String(timer);
      return;
    }
    clearTimeout(parseInt(un.dataset.armTimer, 10));
    try {
      await window.go.main.ShareService.Unfollow(id);
      if (typeof showToast === 'function') showToast('Unfollowed');
      await refresh();
    } catch (err) {
      const msg = (err && err.message) ? err.message : String(err);
      console.error('share: Unfollow failed', err);
      if (typeof showToast === 'function') showToast('Unfollow failed: ' + msg, 'error');
    }
  });

  // --- Edit follow tag modal ---

  const editFollowModal = document.getElementById('edit-follow-modal');
  const editFollowInput = document.getElementById('edit-follow-tag-input');
  const editFollowErr = document.getElementById('edit-follow-error');
  const editFollowSaveBtn = document.getElementById('edit-follow-save-btn');

  let editFollowID = null;
  let editFollowOriginalTag = '';
  let editFollowAutocomplete = null;

  function openEditFollowModal(id, currentTag) {
    editFollowID = id;
    editFollowOriginalTag = currentTag || '';
    editFollowInput.value = editFollowOriginalTag;
    editFollowErr.classList.add('hidden');
    editFollowErr.textContent = '';
    editFollowSaveBtn.textContent = 'Save';
    updateEditSaveEnabled();
    editFollowModal.classList.remove('hidden');
    if (window.TagAutocomplete && !editFollowAutocomplete) {
      editFollowAutocomplete = window.TagAutocomplete.attach(editFollowInput, {
        getTags: async () => window.go.main.App.GetTags(),
        onSelect: (val) => {
          editFollowInput.value = val;
          updateEditSaveEnabled();
        },
      });
    }
    requestAnimationFrame(() => editFollowInput.focus());
  }

  function closeEditFollowModal() {
    editFollowModal.classList.add('hidden');
    if (editFollowAutocomplete) { editFollowAutocomplete.destroy(); editFollowAutocomplete = null; }
    editFollowID = null;
    editFollowOriginalTag = '';
  }

  function updateEditSaveEnabled() {
    const v = editFollowInput.value.trim();
    const tagOK = window.TagAutocomplete && window.TagAutocomplete.isCreatableName(v);
    // Disable if invalid (empty or empty path segments) or unchanged.
    editFollowSaveBtn.disabled = !tagOK || v === editFollowOriginalTag;
  }

  document.querySelectorAll('.edit-follow-close').forEach(b => b.addEventListener('click', closeEditFollowModal));
  editFollowInput.addEventListener('input', updateEditSaveEnabled);

  editFollowSaveBtn.addEventListener('click', async () => {
    if (editFollowID == null) return;
    const newTag = editFollowInput.value.trim();
    if (!newTag) return;
    editFollowSaveBtn.disabled = true;
    const orig = editFollowSaveBtn.textContent;
    editFollowSaveBtn.textContent = 'Saving…';
    try {
      await window.go.main.ShareService.UpdateFollowTag(editFollowID, newTag);
      if (typeof showToast === 'function') showToast('Tag updated');
      closeEditFollowModal();
      await refresh();
    } catch (e) {
      const msg = (e && e.message) ? e.message : String(e);
      console.error('share: UpdateFollowTag failed', e);
      editFollowErr.textContent = msg;
      editFollowErr.classList.remove('hidden');
      editFollowSaveBtn.textContent = orig;
      updateEditSaveEnabled();
    }
  });

  // Re-render on backend events.
  if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('share:publication-updated', refresh);
    window.runtime.EventsOn('share:publication-removed', refresh);
    window.runtime.EventsOn('share:follow-updated', refresh);
    window.runtime.EventsOn('share:follow-removed', refresh);

    // A clip arrived from a followed share — refresh both the share
    // view (bumps clips_received on the follow row) and the clips
    // gallery so the new clip shows up without a manual reload. The
    // refresh re-fetches in-place; current scroll position and focus
    // are preserved by loadClips's implementation.
    window.runtime.EventsOn('share:clip-received', () => {
      refresh();
      if (typeof loadClips === 'function') loadClips();
      if (typeof showToast === 'function') showToast('Received shared clip');
    });
  }

  // Expose for view switcher.
  window.ShareView = { refresh };

  // Back button behaves like other views — flip to clips.
  const backBtn = document.getElementById('share-back-btn');
  if (backBtn) backBtn.addEventListener('click', () => {
    if (typeof switchView === 'function') switchView('clips');
  });
})();
