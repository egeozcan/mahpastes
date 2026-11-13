// Node harness for api_port_field_test.go: loads api-settings.js with a stub
// DOM and drives the "Start on launch" switch, the port field's change event
// and the Start button with various port texts. Prints a JSON object of
// scenario -> { saves, starts, toasts, checked }.
'use strict';
const fs = require('fs');
const vm = require('vm');

const source = fs.readFileSync(process.argv[2], 'utf8');
const tick = () => new Promise(r => setTimeout(r, 0));

function makeSandbox() {
    const state = { saves: [], starts: [], toasts: [] };
    const elements = {};
    const element = id => {
        if (!elements[id]) {
            const handlers = {};
            elements[id] = {
                id, value: '', checked: false, disabled: false, textContent: '', innerHTML: '',
                classList: { add() {}, remove() {}, toggle() {}, contains: () => false },
                addEventListener(type, fn) { handlers[type] = fn; },
                fire: type => handlers[type] && handlers[type]({ target: elements[id] }),
                querySelector: () => element(id + '>child'),
                removeAttribute() {}, setAttribute() {},
            };
        }
        return elements[id];
    };
    const APIService = {
        SetAPIAutostart: async (enabled, port) => {
            if (enabled && !(port >= 1 && port <= 65535)) throw new Error(`invalid port ${port}: must be between 1 and 65535`);
            state.saves.push([enabled, port]);
        },
        StartAPI: async (port, bindAll) => { state.starts.push([port, bindAll]); },
        StopAPI: async () => {},
        GetAPIStatus: async () => ({ running: false }),
        GetAPIAutostart: async () => element('api-autostart-toggle').checked,
        GetAPIPort: async () => 44557,
        ListAPIKeys: async () => [],
    };
    const sandbox = {
        window: { go: { main: { APIService, App: { GetTags: async () => [] } } } },
        document: { getElementById: element },
        navigator: {},
        console: { error() {}, log() {} },
        createModalFocus: () => ({ open() {}, close() {} }),
        showToast: (msg, type) => state.toasts.push([msg, type]),
        errText: e => (e && e.message) || String(e),
        setTimeout, clearTimeout, Promise, Error, Number, String, JSON,
    };
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);
    return { state, element };
}

async function run(text, action) {
    const { state, element } = makeSandbox();
    const port = element('api-port-input');
    const toggle = element('api-autostart-toggle');
    port.value = text;
    if (action === 'switchOn') {
        toggle.checked = true;
        toggle.fire('change');
    } else if (action === 'switchOff') {
        toggle.checked = false;
        toggle.fire('change');
    } else if (action === 'editWhileOn') {
        toggle.checked = true;
        port.fire('change');
    } else if (action === 'start') {
        element('api-toggle-btn').fire('click');
    }
    for (let i = 0; i < 10; i++) await tick();
    return { saves: state.saves, starts: state.starts, toasts: state.toasts.map(t => t[0]), checked: toggle.checked };
}

(async () => {
    const out = {};
    for (const text of ['45123', '0', '', '1e3', '80.5', '70000', '-5']) {
        for (const action of ['switchOn', 'editWhileOn', 'start']) {
            out[`${action}:${text}`] = await run(text, action);
        }
    }
    out['switchOff:'] = await run('', 'switchOff');
    process.stdout.write(JSON.stringify(out));
})().catch(e => { console.error(e); process.exit(1); });
