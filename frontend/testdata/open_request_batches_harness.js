// Node harness for open_request_batches_test.go: loads open-requests.js with
// stubs and hands handleOpenRequests batches of opened files. Prints a JSON
// object of scenario -> { uploads: [[path...]...], toasts: [message...] }.
'use strict';
const fs = require('fs');
const vm = require('vm');

const source = fs.readFileSync(process.argv[2], 'utf8');
const MB = 1024 * 1024;

async function run(batch) {
    const state = { uploads: [], toasts: [] };
    const sandbox = {
        window: {},
        isViewingArchive: false,
        uploadFilesFromPaths: async paths => { state.uploads.push(paths); },
        showToast: msg => state.toasts.push(msg),
        console: { error() {}, log() {} },
        Object, Number, Promise,
    };
    vm.createContext(sandbox);
    vm.runInContext(source, sandbox);
    await sandbox.window.__testHelpers.handleOpenRequests(batch);
    return state;
}

(async () => {
    const names = n => Array.from({ length: n }, (_, i) => `/in/f${i}`);
    const out = {
        // Small files: only the file count bounds a batch.
        small: await run({ files: names(70), file_sizes: Array(70).fill(1000) }),
        // 100 MB each: three would pass the byte budget.
        medium: await run({ files: names(5), file_sizes: Array(5).fill(100 * MB) }),
        // A file over the budget is imported alone, order kept.
        large: await run({ files: names(4), file_sizes: [MB, 700 * MB, MB, MB] }),
        // An older backend sends no sizes: batches of 32 as before.
        noSizes: await run({ files: names(33) }),
        tooLarge: await run({ files: ['/in/ok.txt'], file_sizes: [3], too_large: ['/v/big.mov'] }),
        both: await run({ skipped: ['/somewhere/Folder'], too_large: ['/v/a.mov', 'C:\\v\\b.mkv'] }),
    };
    process.stdout.write(JSON.stringify(out));
})().catch(err => { console.error(err); process.exit(1); });
