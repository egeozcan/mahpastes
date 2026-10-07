// Node harness for patch_plugin_side_effects_test.go: loads wails-api.js with
// stubbed DOM/Wails globals and checks that each in-place gallery patch path
// (archive, delete, rename/tag/expiry via refreshClipInPlace) reloads instead
// of patching when a plugin handler changed the library inside the mutation
// (GetPluginLibraryWrites moved), and still patches when it did not.
// Prints a JSON object of scenario -> outcome.
'use strict';
const fs = require('fs');
const vm = require('vm');

const source = fs.readFileSync(process.argv[2], 'utf8');
const tick = () => new Promise(r => setTimeout(r, 0));

function makeSandbox() {
    const state = { pluginWrites: 0, pluginWritesThrows: false, loads: 0, removed: [] };
    const clip = { id: 5, filename: 'a.txt', content_type: 'text/plain', content_hash: 'h', is_archived: false, tags: [] };
    const card = { _clip: clip, dataset: { id: '5' }, contains: () => false, remove() {}, style: {},
        querySelector: () => ({ getAttribute: () => 'a.txt' }) };
    const gallery = {
        querySelector: sel => (sel.includes('data-id="5"') ? card : null),
        querySelectorAll: () => [card],
    };
    const App = {
        GetPluginLibraryWrites: async () => {
            if (state.pluginWritesThrows) throw new Error('403');
            return state.pluginWrites;
        },
        GetLibraryVersion: async () => 1,
        GetClipPreview: async () => ({ ...clip, is_archived: !!state.archived }),
        ToggleArchive: async () => { state.archived = !state.archived; if (state.pluginSideEffect) state.pluginWrites++; },
        DeleteClip: async () => { if (state.pluginSideEffect) state.pluginWrites++; },
        RenameClip: async () => { if (state.pluginSideEffect) state.pluginWrites++; },
    };
    const sandbox = {
        window: { go: { main: { App } } },
        document: { activeElement: null },
        console: { error() {}, log() {} },
        gallery,
        isViewingArchive: false,
        currentSortField: 'date',
        allTags: [],
        selectedIds: new Set(),
        showToast() {},
        showConfirmDialog: (_t, _m, onOk) => onOk(),
        showPromptDialog: (_t, _v, onOk) => onOk('b.txt'),
        videoFrameCacheDelete() {},
        renderCardFilename() {}, renderCardExpiry() {}, renderCardTags() {},
        clipCardSignature: () => 'sig',
        setTimeout, clearTimeout, Promise, Map, Set, Array, Math, String, Number, Error, JSON, Date,
    };
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);
    // The listing on screen: a plain "all" view whose load read counter 0.
    vm.runInContext(`
        _galleryView = { key: 'k', request: { mode: 'all', tag_ids: [], hidden_tag_ids: [] }, loaded: 1, total: 1, hasMore: false };
        _galleryPluginWrites = 0;
    `, sandbox);
    sandbox.loadClips = () => { state.loads++; };
    sandbox.removeClipCardInPlace = id => { state.removed.push(Number(id)); return true; };
    sandbox.afterGalleryPatch = () => {};
    return { sandbox, state };
}

async function scenario(run, setup) {
    const { sandbox, state } = makeSandbox();
    setup(state);
    const result = await run(sandbox);
    for (let i = 0; i < 5; i++) await tick();
    return { loads: state.loads, removed: state.removed.length, result };
}

(async () => {
    const out = {};
    const archive = s => s.toggleArchiveClip(5);
    const del = s => s.deleteClip(5);
    const rename = s => s.renameClip(5);
    const refresh = s => s.refreshClipInPlace(5);
    out.archivePlugin = await scenario(archive, st => { st.pluginSideEffect = true; });
    out.archiveQuiet = await scenario(archive, () => {});
    out.archiveUnknown = await scenario(archive, st => { st.pluginWritesThrows = true; });
    out.deletePlugin = await scenario(del, st => { st.pluginSideEffect = true; });
    out.deleteQuiet = await scenario(del, () => {});
    out.renamePlugin = await scenario(rename, st => { st.pluginSideEffect = true; });
    out.renameQuiet = await scenario(rename, () => {});
    out.refreshMoved = await scenario(refresh, st => { st.pluginWrites = 1; });
    out.refreshQuiet = await scenario(refresh, () => {});
    process.stdout.write(JSON.stringify(out));
})().catch(e => { console.error(e); process.exit(1); });
