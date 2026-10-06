// Node harness for markdown_preview_pool_test.go: loads markdown-preview.js
// with stubbed DOM/Wails globals and counts concurrent GetLocalImage calls
// across superseded renders. Prints {"maxInFlight":N,"calls":N} as JSON.
'use strict';
const fs = require('fs');
const vm = require('vm');

const source = fs.readFileSync(process.argv[2], 'utf8');

function el() {
    return {
        children: [], dataset: {}, textContent: '',
        appendChild(c) { this.children.push(c); return c; },
        append(...c) { this.children.push(...c); },
        replaceChildren() { this.children = []; },
        replaceWith() {}, setAttribute() {}, removeAttribute() {}, addEventListener() {},
    };
}

let inFlight = 0;
let maxInFlight = 0;
let calls = 0;
const pending = [];
const tick = () => new Promise(r => setImmediate(r));

const service = {
    ResolveReferences: async (_id, refs) => refs.map((ref, i) => ({
        status: 'unique', candidates: [{ clip_id: Number(ref.replace(/\D/g, '')) }],
    })),
    GetLocalImage: clipID => {
        calls++;
        inFlight++;
        maxInFlight = Math.max(maxInFlight, inFlight);
        return new Promise((resolve, reject) => {
            pending.push(() => { inFlight--; reject(new Error('gone ' + clipID)); });
        });
    },
};

const sandbox = {
    window: { go: { main: { MarkdownService: service, App: { GetLibraryVersion: async () => 1 } } } },
    document: { createElement: el },
    console: { error() {}, log: console.log },
    URL: { createObjectURL: () => 'blob:x', revokeObjectURL() {} },
    Blob: class {}, atob: s => s, setImmediate, Promise, Map, Set, Array, Math, String, Number, Error,
    TextEncoder, crypto: globalThis.crypto,
};
vm.createContext(sandbox);
vm.runInContext(source + '\nthis.MarkdownPreview = MarkdownPreview;', sandbox);
const MP = sandbox.MarkdownPreview;

function container(prefix, count) {
    const images = [];
    for (let i = 0; i < count; i++) images.push({ source: `${prefix}${i}.png`, alt: '', placeholder: el() });
    return { querySelectorAll: () => [], markdownImages: images };
}

(async () => {
    MP.open(1);
    // Three renders, each with different references, each superseding the
    // last while its reads are still in flight.
    for (const [prefix, base] of [['a', 100], ['b', 200], ['c', 300]]) {
        MP.beginRender();
        const c = container('img', 6);
        c.markdownImages.forEach((d, i) => { d.source = `img${base + i}.png`; });
        MP.enhance(c);
        for (let i = 0; i < 20; i++) await tick();
    }
    // Drain: settle every outstanding read, letting queued work start.
    for (let round = 0; round < 50 && (pending.length || inFlight); round++) {
        pending.splice(0).forEach(f => f());
        for (let i = 0; i < 10; i++) await tick();
    }
    process.stdout.write(JSON.stringify({ maxInFlight, calls, inFlight }));
})().catch(e => { console.error(e); process.exit(1); });
