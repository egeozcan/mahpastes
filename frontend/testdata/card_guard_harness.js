// Node harness for server_image_guard_test.go. Extracts individual functions
// from ui.js (which cannot be loaded whole outside a browser) and runs them
// against stubs. Prints a JSON object of named results.
'use strict';
const fs = require('fs');
const vm = require('vm');

const source = fs.readFileSync(process.argv[2], 'utf8');

// Source text of a top-level function declaration, by brace matching from the
// body's opening brace.
function extract(name) {
    const re = new RegExp(`^(async\\s+)?function\\s+${name}\\s*\\(`, 'm');
    const m = re.exec(source);
    if (!m) throw new Error(`function ${name} not found in ui.js`);
    const close = source.indexOf(')', m.index);
    const open = source.indexOf('{', close);
    let depth = 0;
    for (let i = open; i < source.length; i++) {
        if (source[i] === '{') depth++;
        else if (source[i] === '}' && --depth === 0) return source.slice(m.index, i + 1);
    }
    throw new Error(`unbalanced function ${name}`);
}

// Minimal element: class-selector queries only (".a" or ".a, .b").
function makeEl(classes, text = '') {
    return {
        classes: new Set(classes), textContent: text, attrs: {}, dataset: {}, innerHTML: '',
        setAttribute(k, v) { this.attrs[k] = v; },
        getAttribute(k) { return this.attrs[k]; },
    };
}
function makeCard(children) {
    const match = (el, sel) => sel.split(',').map(s => s.trim()).some(s => /^\.[\w-]+$/.test(s) && el.classes.has(s.slice(1)));
    return {
        dataset: {}, attrs: {},
        setAttribute(k, v) { this.attrs[k] = v; },
        querySelector(sel) { return children.find(c => match(c, sel)) || null; },
        querySelectorAll(sel) { return children.filter(c => match(c, sel)); },
    };
}

function friendly(contentType, filename) {
    const ext = (filename || '').split('.').pop();
    return ext ? ext.toUpperCase() : 'FILE';
}

async function main() {
    const results = {};

    // --- server image guard ---------------------------------------------------
    const fetches = [];
    let fetchReply = null;
    const sandbox = {
        window: { mahpastesMode: 'server', location: '' },
        gallery: { querySelector: () => null },
        mediaRevisions: new Map(),
        fetch: async (url) => { fetches.push(url); return fetchReply(url); },
        Number, Math, Error, String, JSON, Promise,
    };
    vm.createContext(sandbox);
    const guardSrc = [
        'const SERVER_IMAGE_MAX_INLINE = 64 * 1024 * 1024;',
        source.includes('function serverClipSizeForPreview') ? extract('serverClipSizeForPreview') : '',
        extract('getImageDataUrl'),
        'this.getImageDataUrl = getImageDataUrl;',
    ].join('\n');
    vm.runInContext(guardSrc, sandbox);

    const attempt = async () => {
        try { return { url: await sandbox.getImageDataUrl(7) }; }
        catch (e) { return { error: String(e.message || e) }; }
    };

    // No card, server says 100 MB.
    fetchReply = () => ({ ok: true, status: 200, json: async () => ({ id: 7, size: 100 * 1024 * 1024 }) });
    results.noCardOversized = await attempt();
    // No card, metadata request fails.
    fetchReply = () => ({ ok: false, status: 500, json: async () => ({}) });
    results.noCardMetaFails = await attempt();
    // No card, small image.
    fetchReply = () => ({ ok: true, status: 200, json: async () => ({ id: 7, size: 1024 }) });
    results.noCardSmall = await attempt();
    // Card present with a size: no metadata request.
    fetches.length = 0;
    sandbox.gallery.querySelector = () => ({ dataset: { size: String(2 * 1024) } });
    results.cardSmall = await attempt();
    results.cardSmallFetches = fetches.length;
    sandbox.gallery.querySelector = () => ({ dataset: { size: String(65 * 1024 * 1024) } });
    results.cardOversized = await attempt();

    // --- rename refreshes every filename-derived type label --------------------
    // The generic-file preview's centre label: take its classes from the markup
    // createClipCard actually renders.
    const createSrc = extract('createClipCard');
    const spanMatch = /<span class="([^"]*)">\$\{escapeHTML\(getFriendlyFileType\(clip\.content_type, clip\.filename\)\)\}<\/span>\s*<\/div>`;\s*\}/.exec(createSrc);
    if (!spanMatch) throw new Error('generic-file preview label not found in createClipCard');
    const centre = makeEl(spanMatch[1].split(/\s+/), 'BIN');
    const footer = makeEl(['clip-type-label'], 'BIN');
    const name = makeEl(['clip-filename']);
    const card = makeCard([centre, footer, name]);
    const sb2 = { getFriendlyFileType: friendly, escapeHTML: s => String(s ?? ''), String };
    vm.createContext(sb2);
    vm.runInContext(extract('renderCardFilename') + '\nthis.renderCardFilename = renderCardFilename;', sb2);
    sb2.renderCardFilename(card, { filename: 'a.dat', content_type: 'application/octet-stream' });
    results.footerLabel = footer.textContent;
    results.centreLabel = centre.textContent;

    process.stdout.write(JSON.stringify(results));
}
main().catch(e => { console.error(e); process.exit(1); });
