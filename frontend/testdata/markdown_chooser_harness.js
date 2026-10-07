// Node harness for markdown_chooser_pool_test.go: loads markdown-preview.js
// with stubbed DOM/Wails globals, renders one ambiguous local image and four
// unique ones (whose reads stay pending), then picks a candidate in the
// ambiguous image's chooser. Prints {"maxInFlight","calls","inFlight",
// "chosenLoaded","staleCalls"} as JSON.
'use strict';
const fs = require('fs');
const vm = require('vm');

const source = fs.readFileSync(process.argv[2], 'utf8');

function el(tag) {
    const node = {
        tag, children: [], dataset: {}, textContent: '', listeners: {}, parentElement: null,
        appendChild(c) { this.children.push(c); c.parentElement = this; return c; },
        append(...c) { c.forEach(x => this.appendChild(typeof x === 'object' ? x : { text: x })); },
        replaceChildren() { this.children = []; },
        replaceWith() { this.replaced = true; },
        remove() {
            const p = this.parentElement;
            if (p) p.children = p.children.filter(c => c !== this);
            this.parentElement = null;
        },
        insertAdjacentElement(_where, other) { this.parentElement.appendChild(other); },
        querySelector() { return null; },
        setAttribute() {}, removeAttribute() {},
        addEventListener(type, fn) { (this.listeners[type] ||= []).push(fn); },
    };
    return node;
}

function findButton(root, text) {
    for (const c of root.children || []) {
        if (c.tag === 'button' && (text === null || String(c.textContent).startsWith(text))) return c;
        const nested = findButton(c, text);
        if (nested) return nested;
    }
    return null;
}

let inFlight = 0;
let maxInFlight = 0;
let calls = 0;
const requested = [];
const pending = [];
const tick = () => new Promise(r => setImmediate(r));

const service = {
    ResolveReferences: async (_id, refs) => refs.map(ref => {
        if (ref.startsWith('dup')) {
            return {
                status: 'ambiguous',
                candidates: [
                    { clip_id: 900, filename: 'dup.png', matched_tag_paths: ['a'] },
                    { clip_id: 901, filename: 'dup.png', matched_tag_paths: ['b'] },
                ],
            };
        }
        return { status: 'unique', candidates: [{ clip_id: Number(ref.replace(/\D/g, '')) }] };
    }),
    GetLocalImage: clipID => {
        calls++;
        inFlight++;
        requested.push(clipID);
        maxInFlight = Math.max(maxInFlight, inFlight);
        return new Promise((_resolve, reject) => {
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

function render(sources) {
    const root = el('div');
    const images = sources.map(src => {
        const placeholder = el('span');
        root.appendChild(placeholder);
        return { source: src, alt: '', placeholder };
    });
    MP.beginRender();
    MP.enhance({ querySelectorAll: () => [], markdownImages: images });
    return images;
}

async function chooseFirst(descriptor) {
    const choose = findButton(descriptor.placeholder, 'Choose Image');
    if (!choose) throw new Error('no Choose Image button');
    choose.listeners.click.forEach(fn => fn());
    const option = findButton(descriptor.placeholder.parentElement, 'dup.png');
    if (!option) throw new Error('no chooser option');
    option.listeners.click.forEach(fn => fn());
    option.listeners.click.forEach(fn => fn()); // a double click must not queue a second read
}

async function drain() {
    for (let round = 0; round < 50 && (pending.length || inFlight); round++) {
        pending.splice(0).forEach(f => f());
        for (let i = 0; i < 10; i++) await tick();
    }
}

(async () => {
    MP.open(1);
    // The ambiguous image enhances first (no read: it waits for a pick),
    // then four automatic reads hold every pool slot.
    const images = render(['dup.png', 'img101.png', 'img102.png', 'img103.png', 'img104.png']);
    for (let i = 0; i < 20; i++) await tick();
    await chooseFirst(images[0]);
    for (let i = 0; i < 20; i++) await tick();

    await drain();
    const chosenLoaded = requested.filter(id => id === 900).length;

    // A pick on a superseded render's placeholder must start no read.
    const stale = render(['dup.png']);
    for (let i = 0; i < 20; i++) await tick();
    const choose = findButton(stale[0].placeholder, 'Choose Image');
    const option = (() => {
        choose.listeners.click.forEach(fn => fn());
        return findButton(stale[0].placeholder.parentElement, 'dup.png');
    })();
    render(['other.png']); // supersedes `stale`; other.png resolves to clip 0
    for (let i = 0; i < 20; i++) await tick();
    const before = requested.filter(id => id === 900).length;
    option.listeners.click.forEach(fn => fn());
    for (let i = 0; i < 20; i++) await tick();
    const staleCalls = requested.filter(id => id === 900).length - before;

    await drain();
    process.stdout.write(JSON.stringify({ maxInFlight, calls, inFlight, chosenLoaded, staleCalls }));
})().catch(e => { console.error(e); process.exit(1); });
