// Node harness for menu_action_parity_test.go: loads the real shortcuts.js
// and app-info.js with a stub DOM, registers the menu-mapped actions with the
// contexts app.js gives them (parsed from its source) and recording callbacks,
// then clicks every menu id in each app state through __handleMenuAction.
// Prints JSON: state -> { ran: [menu ids whose action ran], switched: [views
// switchView was called with], drawerClosed }.
'use strict';
const fs = require('fs');
const path = require('path');
const vm = require('vm');

const jsDir = process.argv[2];
const read = name => fs.readFileSync(path.join(jsDir, name), 'utf8');
const shortcutsSrc = read('shortcuts.js');
const appSrc = read('app.js');

const MENU = {
    'import-files': 'upload-clip', 'import-folder': 'open-import', 'settings': 'open-settings',
    'archive': 'toggle-archive', 'folder-mode': 'toggle-folder', 'watch': 'open-watch',
    'serve': 'open-serve', 'plugins': 'open-plugins', 'shortcuts': 'show-cheatsheet',
};

function el(id, classes = []) {
    const set = new Set(classes);
    return {
        id, parentElement: null,
        classList: { contains: c => set.has(c), add: c => set.add(c), remove: c => set.delete(c) },
        hasAttribute: () => false,
        compareDocumentPosition: () => 0,
    };
}

// setup(env) puts the app into the state under test.
function run(setup) {
    const els = {
        'watch-view': el('watch-view', ['hidden']),
        'serve-view': el('serve-view', ['hidden']),
        'share-view': el('share-view', ['hidden']),
        'nav-drawer': el('nav-drawer', ['translate-x-full']),
        'lightbox': el('lightbox'),
        'editor-modal': el('editor-modal'),
        'comparison-modal': el('comparison-modal'),
        'import-wizard-modal': el('import-wizard-modal', ['opacity-0']),
        'bulk-toolbar': el('bulk-toolbar'),
    };
    const env = { els, dialogs: [], switched: [], drawerClosed: 0, folderMode: false, isViewingArchive: false };
    const document = {
        readyState: 'complete', activeElement: null,
        getElementById: id => els[id] || null,
        querySelectorAll: sel => (sel === '[aria-modal="true"]' ? env.dialogs : []),
        addEventListener() {},
    };
    const showView = view => {
        for (const v of ['watch', 'serve', 'share']) els[v + '-view'].classList.add('hidden');
        if (view !== 'clips') els[view + '-view'].classList.remove('hidden');
    };
    const sandbox = {
        document, window: {}, console,
        navigator: { userAgent: 'Macintosh' },
        Node: { DOCUMENT_POSITION_FOLLOWING: 4 },
        getComputedStyle: () => ({ zIndex: '0' }),
        switchView: view => { env.switched.push(view); showView(view); },
        closeDrawer: () => { env.drawerClosed++; els['nav-drawer'].classList.add('translate-x-full'); },
    };
    vm.createContext(sandbox);
    vm.runInContext('let folderMode = false; let isViewingArchive = false;', sandbox);
    vm.runInContext(shortcutsSrc + '\n;globalThis.ShortcutManager = ShortcutManager;', sandbox);
    vm.runInContext(read('app-info.js'), sandbox);

    const ran = [];
    let current = null;
    for (const [menuId, actionId] of Object.entries(MENU)) {
        const m = new RegExp("id: '" + actionId + "'[^}]*?context: '([a-z-]+)'").exec(appSrc + shortcutsSrc);
        if (!m) throw new Error('no registration found for ' + actionId);
        sandbox.ShortcutManager.register({ id: actionId, context: m[1], callback: () => ran.push(current) });
    }

    const out = { ran: [], switched: {}, drawerClosed: 0 };
    for (const menuId of Object.keys(MENU)) {
        // Every click starts from the same state.
        showView('clips');
        els['nav-drawer'].classList.add('translate-x-full');
        env.dialogs = [];
        env.switched = [];
        setup(env, showView);
        vm.runInContext(`folderMode = ${!!env.folderMode}; isViewingArchive = ${!!env.isViewingArchive};`, sandbox);
        ran.length = 0;
        current = menuId;
        sandbox.window.__handleMenuAction(menuId);
        if (ran.length) out.ran.push(menuId);
        if (env.switched.length) out.switched[menuId] = env.switched.slice();
    }
    out.drawerClosed = env.drawerClosed;
    return out;
}

const out = {
    gallery: run(() => {}),
    serve: run((env, show) => show('serve')),
    watch: run((env, show) => show('watch')),
    share: run((env, show) => show('share')),
    drawer: run(env => env.els['nav-drawer'].classList.remove('translate-x-full')),
    watchAlreadyInModes: run((env, show) => { show('watch'); env.folderMode = true; env.isViewingArchive = true; }),
    dialog: run((env, show) => { show('serve'); env.dialogs = [el('confirm-dialog')]; }),
    dialogOverDrawer: run(env => {
        env.els['nav-drawer'].classList.remove('translate-x-full');
        env.dialogs = [el('confirm-dialog')];
    }),
    lightbox: run(env => env.els['lightbox'].classList.add('active')),
    editor: run(env => env.els['editor-modal'].classList.add('active')),
    comparison: run(env => env.els['comparison-modal'].classList.add('active')),
    wizard: run(env => env.els['import-wizard-modal'].classList.remove('opacity-0')),
};
process.stdout.write(JSON.stringify(out));
