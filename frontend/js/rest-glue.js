(function () {
    if (window.mahpastesMode !== 'server') return;

    const api = '/api/v1';
    const jsonHeaders = { 'Content-Type': 'application/json' };

    async function fetchJSON(url, opts = {}) {
        const res = await fetch(url, { ...opts, credentials: 'same-origin' });
        if (res.status === 401) {
            window.location = '/login.html';
            throw new Error('Unauthorized');
        }
        if (!res.ok) {
            let message = `${res.status} ${res.statusText}`;
            try {
                const body = await res.json();
                if (body.error) message = body.error;
            } catch {}
            throw new Error(message);
        }
        if (res.status === 204) return null;
        const text = await res.text();
        return text ? JSON.parse(text) : null;
    }

    const postJSON = (url, body) => fetchJSON(url, { method: 'POST', headers: jsonHeaders, body: JSON.stringify(body || {}) });
    const putJSON = (url, body) => fetchJSON(url, { method: 'PUT', headers: jsonHeaders, body: JSON.stringify(body || {}) });
    const patchJSON = (url, body) => fetchJSON(url, { method: 'PATCH', headers: jsonHeaders, body: JSON.stringify(body || {}) });
    const del = (url) => fetchJSON(url, { method: 'DELETE' });

    function clipQuery(archived, tagIds, hiddenIds, sort, dir) {
        const q = new URLSearchParams();
        q.set('archived', String(!!archived));
        for (const id of tagIds || []) q.append('tag', String(id));
        for (const id of hiddenIds || []) q.append('hidden', String(id));
        if (sort) q.set('sort', sort);
        if (dir) q.set('dir', dir);
        return q.toString();
    }

    function base64ToBlob(base64, contentType) {
        const binary = atob(base64);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        return new Blob([bytes], { type: contentType || 'application/octet-stream' });
    }

    async function downloadBlob(url, filename, opts = {}) {
        const res = await fetch(url, { ...opts, credentials: 'same-origin' });
        if (res.status === 401) {
            window.location = '/login.html';
            throw new Error('Unauthorized');
        }
        if (!res.ok) {
            let message = `${res.status} ${res.statusText}`;
            try {
                const body = await res.json();
                if (body.error) message = body.error;
            } catch {}
            throw new Error(message);
        }
        const blob = await res.blob();
        const objectURL = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = objectURL;
        link.download = filename;
        document.body.appendChild(link);
        link.click();
        link.remove();
        setTimeout(() => URL.revokeObjectURL(objectURL), 1000);
    }

    async function uploadOne(fileData, autoTagID) {
        const fd = new FormData();
        fd.append('file', base64ToBlob(fileData.data, fileData.content_type), fileData.name || 'clip');
        const params = new URLSearchParams();
        if (fileData.name) params.set('filename', fileData.name);
        const clip = await fetchJSON(`${api}/clips?${params.toString()}`, { method: 'POST', body: fd });
        if (autoTagID && clip && clip.id) {
            await putJSON(`${api}/clips/${clip.id}/tags/${autoTagID}`, {});
        }
        return clip;
    }

    window.go = { main: {} };
    window.go.main.App = {
        GetClips: async (archived, tagIds, hiddenIds, sort, dir) => (await fetchJSON(`${api}/clips?${clipQuery(archived, tagIds, hiddenIds, sort, dir)}`)).clips || [],
        GetFolderClips: async (archived, tagID, hiddenIds, sort, dir) => {
            const q = `${clipQuery(archived, [], hiddenIds, sort, dir)}&folder_tag=${encodeURIComponent(tagID)}`;
            return (await fetchJSON(`${api}/clips?${q}`)).clips || [];
        },
        GetUntaggedClips: async (archived, hiddenIds, sort, dir) => {
            const q = `${clipQuery(archived, [], hiddenIds, sort, dir)}&untagged=true`;
            return (await fetchJSON(`${api}/clips?${q}`)).clips || [];
        },
        GetClipsDirect: async (archived, tagIds, hiddenIds, sort, dir) => (await fetchJSON(`${api}/clips?${clipQuery(archived, tagIds, hiddenIds, sort, dir)}`)).clips || [],
        GetClipData: async (id) => {
            const res = await fetch(`${api}/clips/${id}/data`, { credentials: 'same-origin' });
            if (res.status === 401) window.location = '/login.html';
            if (!res.ok) throw new Error(res.statusText);
            const blob = await res.blob();
            // The desktop API returns base64; we mimic that shape. base64 inflates
            // ~33% and the data-URL conversion materializes the whole clip as a
            // string, so an oversized clip can freeze/OOM the tab. Cap it and tell
            // the caller to download instead of previewing inline.
            const MAX_INLINE = 64 * 1024 * 1024; // 64 MB
            if (blob.size > MAX_INLINE) {
                throw new Error(`clip is too large to preview in the browser (${Math.round(blob.size / 1048576)} MB) — download it instead`);
            }
            const data = await new Promise((resolve, reject) => {
                const reader = new FileReader();
                reader.onload = () => resolve(String(reader.result).split(',')[1] || '');
                reader.onerror = () => reject(reader.error || new Error('failed to read clip data'));
                reader.readAsDataURL(blob);
            });
            return { id, content_type: blob.type || 'application/octet-stream', data, filename: '' };
        },
        UploadFiles: async (files, minutes, autoTagID) => Promise.all((files || []).map((f) => uploadOne(f, autoTagID))),
        UploadFileAndGetID: async (file) => {
            const clip = await uploadOne(file, 0);
            return clip.id;
        },
        UpdateClipData: (id, contentType, data, filename) => putJSON(`${api}/clips/${id}/data`, { content_type: contentType, data, filename }),
        DeleteClip: (id) => del(`${api}/clips/${id}`),
        RenameClip: (id, filename) => patchJSON(`${api}/clips/${id}`, { filename }),
        ToggleArchive: async (id) => putJSON(`${api}/clips/${id}/archive`, {}),
        BulkDelete: (ids) => postJSON(`${api}/clips/bulk/delete`, { ids }),
        BulkArchive: (ids) => postJSON(`${api}/clips/bulk/archive`, { ids }),
        BulkUnarchive: (ids) => postJSON(`${api}/clips/bulk/unarchive`, { ids }),
        BulkSetExpiration: (ids, minutes) => postJSON(`${api}/clips/bulk/expire`, { ids, minutes }),
        BulkCancelExpiration: (ids) => postJSON(`${api}/clips/bulk/cancel-expire`, { ids }),
        BulkAddTag: (ids, tagID) => postJSON(`${api}/clips/bulk/tag`, { ids, tag_id: tagID }),
        BulkRemoveTag: (ids, tagID) => postJSON(`${api}/clips/bulk/untag`, { ids, tag_id: tagID }),
        BulkDownloadToFile: (ids) => downloadBlob(`${api}/clips/bulk/download`, 'clips.zip', { method: 'POST', headers: jsonHeaders, body: JSON.stringify({ ids }) }),
        SetExpiration: (id, minutes) => putJSON(`${api}/clips/${id}/expiration`, { minutes }),
        CancelExpiration: (id) => del(`${api}/clips/${id}/expiration`),
        GetTags: () => fetchJSON(`${api}/tags`),
        CreateTag: (name) => postJSON(`${api}/tags`, { name }),
        UpdateTag: (id, name, color) => putJSON(`${api}/tags/${id}`, { name, color }),
        DeleteTag: (id) => del(`${api}/tags/${id}`),
        MergeTag: (sourceID, destID) => postJSON(`${api}/tags/${sourceID}/merge`, { dest_id: destID }),
        PreviewMergeTag: (sourceID, destID) => postJSON(`${api}/tags/${sourceID}/merge-preview`, { dest_id: destID }),
        AddTagToClip: (clipID, tagID) => putJSON(`${api}/clips/${clipID}/tags/${tagID}`, {}),
        RemoveTagFromClip: (clipID, tagID) => del(`${api}/clips/${clipID}/tags/${tagID}`),
        GetChildTags: (id) => fetchJSON(`${api}/tags/${id}/children`),
        GetTopLevelTags: () => fetchJSON(`${api}/tags`),
        GetDescendantClipCount: async () => 0,
        GetHiddenTags: async () => (await fetchJSON(`${api}/tags/hidden`)).ids || [],
        SetHiddenTags: (ids) => putJSON(`${api}/tags/hidden`, { ids }),
        GetClipMetadata: (id) => fetchJSON(`${api}/clips/${id}/metadata`),
        SetClipMetadataBulk: (id, metadata) => putJSON(`${api}/clips/${id}/metadata`, metadata),
        GetSetting: async (key) => (await fetchJSON(`${api}/settings/${encodeURIComponent(key)}`)).value || '',
        SetSetting: (key, value) => putJSON(`${api}/settings/${encodeURIComponent(key)}`, { value }),
        GetDuplicateGroups: async () => (await fetchJSON(`${api}/dedup`)).groups || [],
        DeduplicateAll: () => postJSON(`${api}/dedup/all`, {}),
        MergeDuplicates: (clipID) => postJSON(`${api}/dedup/${clipID}/merge`, {}),
        GetRemovableEmptyTags: () => fetchJSON(`${api}/maintenance/empty-tags`),
        RemoveEmptyTags: () => postJSON(`${api}/maintenance/empty-tags`, {}),
        GetDatabaseSize: async () => (await fetchJSON(`${api}/maintenance/database`)).size || 0,
        CompactDatabase: () => postJSON(`${api}/maintenance/database`, {}),
        GetStaleFiles: () => fetchJSON(`${api}/maintenance/stale-files`),
        CleanStaleFiles: () => postJSON(`${api}/maintenance/stale-files/clean`, {}),
        GetOrphanDBRows: () => fetchJSON(`${api}/maintenance/orphan-rows`),
        CleanOrphanDBRows: () => postJSON(`${api}/maintenance/orphan-rows/clean`, {}),
        FindClipsByFilenameAndTag: (filenames, tagID) => postJSON(`${api}/clips/find`, { filenames, tag_id: tagID }),
        GetImageDiff: (a, b, threshold) => postJSON(`${api}/clips/diff`, { clip_id_a: a, clip_id_b: b, threshold }),
        GetWatchedFolders: async () => (await fetchJSON(`${api}/watch`)).folders || [],
        GetWatchStatus: () => fetchJSON(`${api}/watch/status`),
        GetGlobalWatchPaused: async () => (await fetchJSON(`${api}/watch/status`)).global_paused || false,
        SetGlobalWatchPaused: (paused) => paused ? putJSON(`${api}/watch/global-pause`, {}) : del(`${api}/watch/global-pause`),
        AddWatchedFolder: (config) => postJSON(`${api}/watch`, config),
        RemoveWatchedFolder: (id) => del(`${api}/watch/${id}`),
        UpdateWatchedFolder: (id, config) => putJSON(`${api}/watch/${id}`, config),
        SetFolderPaused: (id, paused) => paused ? putJSON(`${api}/watch/${id}/pause`, {}) : del(`${api}/watch/${id}/pause`),
        ProcessExistingFilesInFolder: (id) => postJSON(`${api}/watch/${id}/process`, {}),
        RefreshWatches: () => postJSON(`${api}/watch/refresh`, {}),
        CreateTempFile: async (id) => `${window.location.origin}${api}/clips/${id}/data`,
        DeleteAllTempFiles: () => del(`${api}/temp`),
        SelectFolder: async () => { throw new Error('folder picker is desktop-only in server mode'); },
        IsDirectory: async () => false,
        SaveClipToFile: async (id) => {
            const clip = await fetchJSON(`${api}/clips/${id}`);
            await downloadBlob(`${api}/clips/${id}/data`, clip.filename || `clip_${id}`);
        },
        CopyToClipboard: async (text) => navigator.clipboard ? navigator.clipboard.writeText(text) : undefined,
        ChooseApplication: async () => { throw new Error('application picker is desktop-only in server mode'); },
        OpenClipWithDefaultApp: async (id) => window.open(`${api}/clips/${id}/data`, '_blank', 'noopener'),
        OpenClipWithApp: async () => { throw new Error('open-with is desktop-only in server mode'); },
        ShowCreateBackupDialog: () => downloadBlob(`${api}/backup`, 'mahpastes-backup.zip'),
        ShowRestoreBackupDialog: async () => null,
        BackupInspect: async () => null,
        ConfirmRestoreBackup: async () => null,
    };

    window.go.main.ServeService = {
        GetServeStatus: async () => (await fetchJSON(`${api}/serve`)).servers || [],
        StartServing: (tagID, port, bindAll, apiAccess) => postJSON(`${api}/serve`, { tag_id: tagID, port, bind_all: bindAll, api_access: apiAccess }),
        StopServing: (tagID) => del(`${api}/serve/${tagID}`),
        GetRandomPort: async () => (await fetchJSON(`${api}/serve/random-port`)).port,
    };
    window.go.main.ShareService = {
        GetShareStatus: () => fetchJSON(`${api}/share`),
        StartShare: (tagID) => postJSON(`${api}/share/publish`, { tag_id: tagID }),
        StopShare: (tagID) => del(`${api}/share/publish/${tagID}`),
        PauseShare: (tagID) => putJSON(`${api}/share/publish/${tagID}/pause`, {}),
        ResumeShare: (tagID) => del(`${api}/share/publish/${tagID}/pause`),
        Follow: (shareString, localTagName) => postJSON(`${api}/share/follow`, { share_string: shareString, local_tag_name: localTagName }),
        FollowWithoutDial: (shareString, localTagName) => postJSON(`${api}/share/follow-direct`, { share_string: shareString, local_tag_name: localTagName }),
        TestFollowConnection: (shareString) => postJSON(`${api}/share/test-follow`, { share_string: shareString }),
        Unfollow: (id) => del(`${api}/share/follow/${id}`),
        ReconnectFollow: (id) => postJSON(`${api}/share/follow/${id}/reconnect`, {}),
        PauseFollow: (id) => putJSON(`${api}/share/follow/${id}/pause`, {}),
        ResumeFollow: (id) => del(`${api}/share/follow/${id}/pause`),
        UpdateFollowTag: (id, localTagName) => putJSON(`${api}/share/follow/${id}/tag`, { local_tag_name: localTagName }),
        GetShareLogs: (followID, publicationID) => fetchJSON(`${api}/share/logs?follow_id=${followID || 0}&publication_id=${publicationID || 0}`),
    };
    window.go.main.LinkService = {
        CreateShareLink: (clipID, name, expiresInSeconds, maxDownloads) => postJSON(`${api}/links`, {
            clip_id: clipID,
            name: name || '',
            expires_in_seconds: expiresInSeconds || 0,
            max_downloads: maxDownloads || 0,
        }),
        ListShareLinks: () => fetchJSON(`${api}/links`),
        RevokeShareLink: (id) => del(`${api}/links/${id}`),
    };
    window.go.main.PluginService = {
        GetPlugins: async () => (await fetchJSON(`${api}/plugins`)).plugins || [],
        GetPluginUIActions: () => fetchJSON(`${api}/plugins/actions`),
        ExecutePluginAction: (pluginID, actionID, clipIDs, options, context) => postJSON(`${api}/plugins/${pluginID}/actions/${actionID}`, { clip_ids: clipIDs, options, context }),
        ConfirmPluginInstall: (source) => postJSON(`${api}/plugins/confirm`, { source }),
        PreviewPluginFromURL: (source) => postJSON(`${api}/plugins/preview`, { source }),
        RemovePlugin: (id) => del(`${api}/plugins/${id}`),
        EnablePlugin: (id) => putJSON(`${api}/plugins/${id}/enable`, {}),
        DisablePlugin: (id) => putJSON(`${api}/plugins/${id}/disable`, {}),
        UpdatePlugin: (id) => postJSON(`${api}/plugins/${id}/update`, {}),
        ConfirmPluginUpdate: async () => { throw new Error('plugin update confirmation is unavailable in server mode'); },
        GetPluginPermissions: (id) => fetchJSON(`${api}/plugins/${id}/permissions`),
        RevokePluginPermission: (id, type, path) => postJSON(`${api}/plugins/${id}/permissions/revoke`, { type, path }),
        IsPluginURLAllowed: async (id, url, method) => (await postJSON(`${api}/plugins/${id}/url-check`, { url, method })).allowed,
        GetAllPluginStorage: (id) => fetchJSON(`${api}/plugins/${id}/storage`),
        SetPluginStorage: (id, key, value) => putJSON(`${api}/plugins/${id}/storage/${encodeURIComponent(key)}`, { value }),
        TryAcquireModalGuard: async () => true,
        ImportPlugin: () => new Promise((resolve, reject) => {
            // Headless equivalent of the desktop file dialog: let the browser pick a
            // local .lua file, upload it, and return the permission preview. The caller
            // then reviews it and calls ConfirmPluginInstall(preview.source).
            const input = document.createElement('input');
            input.type = 'file';
            input.accept = '.lua';
            input.style.display = 'none';
            let settled = false;
            const finish = (fn, arg) => { if (!settled) { settled = true; input.remove(); fn(arg); } };
            input.addEventListener('cancel', () => finish(resolve, null));
            input.addEventListener('change', async () => {
                const file = input.files && input.files[0];
                if (!file) { finish(resolve, null); return; }
                try {
                    const fd = new FormData();
                    fd.append('file', file, file.name);
                    const preview = await fetchJSON(`${api}/plugins/upload`, { method: 'POST', body: fd });
                    finish(resolve, preview);
                } catch (err) {
                    finish(reject, err);
                }
            });
            document.body.appendChild(input);
            input.click();
        }),
        GetUpdateCheckInterval: () => window.go.main.App.GetSetting('plugin_update_interval'),
        SetUpdateCheckInterval: (value) => window.go.main.App.SetSetting('plugin_update_interval', value),
    };
    window.go.main.APIService = {
        GetAPIStatus: () => fetchJSON(`${api}/status`),
        CreateAPIKey: (name, role, scopedTagID) => postJSON(`${api}/keys`, { name, role, scoped_tag_id: scopedTagID }),
        ListAPIKeys: () => fetchJSON(`${api}/keys`),
        RevokeAPIKey: (id) => del(`${api}/keys/${id}`),
        StartAPI: async () => { throw new Error('the headless server owns the API listener'); },
        StopAPI: async () => { throw new Error('the headless server owns the API listener'); },
    };
    // Clipboard copy targets the host's clipboard, which is meaningless for a
    // remote browser user — desktop-only, like drag-out transfers. The UI
    // surfaces that call these are already hidden in server mode.
    window.go.main.ClipboardService = new Proxy({}, {
        get: () => async () => { throw new Error('clipboard service is desktop-only in server mode'); },
    });
    window.go.main.TransferService = new Proxy({}, {
        get: () => async () => { throw new Error('transfer service is desktop-only in server mode'); },
    });

    const source = new EventSource(`${api}/events`);
    // The browser can't push upstream over SSE, so the desktop "upstream" events
    // the app emits are mapped to dedicated REST calls in server mode.
    const upstreamRoutes = {
        'plugin:modal:acked': `${api}/plugins/modal/ack`,
        'plugin:modal:closed': `${api}/plugins/modal/close`,
    };
    // The event stream is unavailable to tag-scoped keys (403) and rejected for
    // an expired session (401); in both cases EventSource closes without
    // reconnecting. Distinguish the two so an expired session forces re-login
    // (matching fetchJSON), while a valid-but-scoped session simply goes without
    // live updates instead of looping.
    source.onerror = () => {
        if (source.readyState !== EventSource.CLOSED) return; // transient: it will retry
        fetch(`${api}/tags`, { credentials: 'same-origin' })
            .then((res) => { if (res.status === 401) window.location = '/login.html'; })
            .catch(() => {});
    };
    const listeners = new Map();
    window.runtime = {
        EventsOn(name, cb) {
            const wrapped = (event) => cb(JSON.parse(event.data));
            listeners.set(cb, { name, wrapped });
            source.addEventListener(name, wrapped);
        },
        EventsOff(name) {
            for (const [cb, info] of listeners) {
                if (!name || info.name === name) {
                    source.removeEventListener(info.name, info.wrapped);
                    listeners.delete(cb);
                }
            }
        },
        EventsEmit(name) {
            const url = upstreamRoutes[name];
            if (url) postJSON(url, {}).catch(() => {});
        },
        OnFileDrop() { return () => {}; },
    };
})();
