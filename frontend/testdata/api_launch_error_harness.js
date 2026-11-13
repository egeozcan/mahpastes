// Node harness for api_launch_error_test.go: loads api-settings.js with a stub
// DOM, opens the API modal and reports what the status line shows. Prints a
// JSON object of scenario -> { text, html, classes }.
'use strict';
const fs = require('fs');
const vm = require('vm');

const source = fs.readFileSync(process.argv[2], 'utf8');
const tick = () => new Promise(r => setTimeout(r, 0));

// service: overrides for the stub APIService (GetAPILaunchError left out
// stands for server mode, whose APIService has no such method).
async function run(service) {
    const elements = {};
    const element = id => {
        if (!elements[id]) {
            const handlers = {};
            const classes = new Set();
            elements[id] = {
                id, value: '', checked: false, disabled: false, textContent: '', innerHTML: '', classes,
                classList: {
                    add: (...c) => c.forEach(x => classes.add(x)),
                    remove: (...c) => c.forEach(x => classes.delete(x)),
                    toggle() {}, contains: c => classes.has(c),
                },
                addEventListener(type, fn) { handlers[type] = fn; },
                fire: type => handlers[type] && handlers[type]({ target: elements[id] }),
                querySelector: () => element(id + '>child'),
                removeAttribute() {}, setAttribute() {},
            };
        }
        return elements[id];
    };
    const APIService = Object.assign({
        GetAPIStatus: async () => ({ running: false }),
        GetAPIAutostart: async () => true,
        GetAPIPort: async () => 45871,
        ListAPIKeys: async () => [],
    }, service);
    const sandbox = {
        window: { go: { main: { APIService, App: { GetTags: async () => [] } } } },
        document: { getElementById: element },
        navigator: {},
        console: { error() {}, log() {} },
        createModalFocus: () => ({ open() {}, close() {} }),
        showToast() {},
        errText: e => (e && e.message) || String(e),
        setTimeout, clearTimeout, Promise, Error, Number, String, JSON,
    };
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);

    const status = element('api-status-text');
    const snapshot = () => ({ text: status.textContent, html: status.innerHTML, classes: [...status.classes] });
    const open = async () => {
        element('open-api-btn').fire('click');
        for (let i = 0; i < 10; i++) await tick();
        return snapshot();
    };
    return { open, APIService };
}

(async () => {
    const out = {};
    const failed = '<b>port 45871</b> is already in use';
    out.failed = await (await run({ GetAPILaunchError: async () => failed })).open();
    out.none = await (await run({ GetAPILaunchError: async () => '' })).open();
    out.serverMode = await (await run({})).open();
    out.unreadable = await (await run({ GetAPILaunchError: async () => { throw new Error('boom'); } })).open();
    out.running = await (await run({
        GetAPIStatus: async () => ({ running: true, port: 45871, url: 'http://127.0.0.1:45871', request_count: 0 }),
        GetAPILaunchError: async () => failed,
    })).open();

    // The reason goes away once the backend has forgotten it (a manual start
    // or stop), and the warning colour with it.
    let reason = failed;
    const later = await run({ GetAPILaunchError: async () => reason });
    await later.open();
    reason = '';
    out.cleared = await later.open();
    process.stdout.write(JSON.stringify(out));
})().catch(e => { console.error(e); process.exit(1); });
