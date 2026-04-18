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
    try {
      const info = await window.go.main.ShareService.StartShare(tagID);
      stringBox.textContent = info.share_string;
      pickerSec.classList.add('hidden');
      resultSec.classList.remove('hidden');
      await refresh();
    } catch (e) {
      alert('Failed to start share: ' + e);
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
      if (!confirm('Stop sharing? Existing followers will disconnect and the link will stop working.')) return;
      try {
        await window.go.main.ShareService.StopShare(tagID);
        await refresh();
      } catch (err) {
        alert('Stop failed: ' + err);
      }
    }
  });

  // Follow modal
  addFollowBtn.addEventListener('click', () => {
    followString.value = '';
    followTagInput.value = '';
    followErr.classList.add('hidden');
    followTagSec.classList.add('hidden');
    followConfirmBtn.disabled = true;
    followModal.classList.remove('hidden');
  });
  document.querySelectorAll('.follow-share-close').forEach(b => b.addEventListener('click', () => {
    followModal.classList.add('hidden');
  }));

  function parseShareStringClientSide(s) {
    if (!s || !s.startsWith('mp-share:v1:')) return null;
    const blob = s.substring('mp-share:v1:'.length);
    if (!/^[A-Za-z0-9_-]+$/.test(blob)) return null;
    return true;
  }

  followString.addEventListener('input', () => {
    const ok = parseShareStringClientSide(followString.value.trim());
    if (ok) {
      followTagSec.classList.remove('hidden');
      followErr.classList.add('hidden');
      followConfirmBtn.disabled = false;
    } else {
      followTagSec.classList.add('hidden');
      followConfirmBtn.disabled = true;
      if (followString.value.trim().length > 0) {
        followErr.textContent = 'Not a valid share link';
        followErr.classList.remove('hidden');
      } else {
        followErr.classList.add('hidden');
      }
    }
  });

  followConfirmBtn.addEventListener('click', async () => {
    const s = followString.value.trim();
    const tagName = followTagInput.value.trim();
    if (!tagName) {
      followErr.textContent = 'Local tag name required';
      followErr.classList.remove('hidden');
      return;
    }
    try {
      await window.go.main.ShareService.Follow(s, tagName);
      followModal.classList.add('hidden');
      await refresh();
    } catch (e) {
      followErr.textContent = String(e);
      followErr.classList.remove('hidden');
    }
  });

  followList.addEventListener('click', async (e) => {
    const un = e.target.closest('.share-unfollow');
    if (!un) return;
    const id = parseInt(un.dataset.id, 10);
    if (!confirm('Unfollow this share? Already-received clips stay.')) return;
    try {
      await window.go.main.ShareService.Unfollow(id);
      await refresh();
    } catch (err) {
      alert('Unfollow failed: ' + err);
    }
  });

  // Re-render on backend events.
  if (window.runtime && window.runtime.EventsOn) {
    window.runtime.EventsOn('share:publication-updated', refresh);
    window.runtime.EventsOn('share:publication-removed', refresh);
    window.runtime.EventsOn('share:follow-updated', refresh);
    window.runtime.EventsOn('share:follow-removed', refresh);
  }

  // Expose for view switcher.
  window.ShareView = { refresh };

  // Back button behaves like other views — flip to clips.
  const backBtn = document.getElementById('share-back-btn');
  if (backBtn) backBtn.addEventListener('click', () => {
    if (typeof switchView === 'function') switchView('clips');
  });
})();
