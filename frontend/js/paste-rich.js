// Rich (HTML) paste capture.
//
// Copying from a browser, Mail, Notes or a word processor puts the selection
// on the clipboard twice: as plain text and as HTML carrying its links and
// formatting. A paste has always kept only the plain text. Settings → Rich
// Text Paste can keep the HTML instead, as an .html clip. Off by default, so
// existing pastes behave exactly as before.
//   text — keep the plain text (default)
//   html — keep the HTML flavour as an .html clip whenever there is one
const PASTE_HTML_BEHAVIORS = ['text', 'html'];
let pasteHtmlBehavior = 'text';

// The paste listener is live from page load; a paste during startup waits for
// the stored preference instead of silently using the default.
let markPasteHtmlBehaviorReady;
const pasteHtmlBehaviorReady = new Promise(resolve => { markPasteHtmlBehaviorReady = resolve; });

window.getPasteHtmlBehavior = () => pasteHtmlBehavior;

// Resolves to the current preference, which Settings may have changed since load.
window.whenPasteHtmlBehaviorReady = async () => {
    await pasteHtmlBehaviorReady;
    return pasteHtmlBehavior;
};

window.loadPasteHtmlBehavior = async () => {
    try {
        // Unlike pasted paths this works in the browser UI too: the HTML comes
        // from the viewer's own clipboard. The key is viewer-readable.
        const stored = await window.go.main.App.GetSetting('paste_html_behavior');
        if (PASTE_HTML_BEHAVIORS.includes(stored)) pasteHtmlBehavior = stored;
    } catch (error) {
        console.error('Failed to load rich text paste setting:', error);
    } finally {
        // Always settle, so a paste waiting on this can never hang.
        markPasteHtmlBehaviorReady();
    }
    return pasteHtmlBehavior;
};

window.addEventListener('load', () => { window.loadPasteHtmlBehavior(); });

window.setPasteHtmlBehavior = async (behavior) => {
    if (!PASTE_HTML_BEHAVIORS.includes(behavior)) return false;
    pasteHtmlBehavior = behavior;
    try {
        await window.go.main.App.SetSetting('paste_html_behavior', behavior);
        return true;
    } catch (error) {
        console.error('Failed to save rich text paste setting:', error);
        return false;
    }
};

// Which flavour a paste keeps: 'html', 'text', or null when there is nothing
// to keep. HTML is only taken when the preference asks for it and the HTML
// flavour has content; otherwise the plain text is used as before.
function pasteFlavor(text, html, behavior) {
    if (behavior === 'html' && typeof html === 'string' && html.trim() !== '') {
        return 'html';
    }
    if (typeof text === 'string' && text !== '') {
        return 'text';
    }
    return null;
}

const HTML_DOCUMENT_RE = /<html[\s>]/i;
const DOCTYPE_RE = /^\s*<!doctype\s+html/i;
const CHARSET_RE = /<meta[^>]+charset/i;
const HEAD_OPEN_RE = /<head(\s[^>]*)?>/i;
const HTML_OPEN_RE = /<html(\s[^>]*)?>/i;

// Turns the clipboard's HTML flavour into a standalone UTF-8 document.
// WebKit hands over a bare fragment (often prefixed with its own charset
// meta); Chromium wraps the fragment in <html><body> without a charset. Opened
// later from disk, a page without a charset declaration is decoded with a
// legacy encoding and mangles anything outside ASCII, so one is added.
function pastedHtmlDocument(html) {
    const markup = String(html || '').trim();
    if (!markup) return '';
    if (!HTML_DOCUMENT_RE.test(markup)) {
        return '<!DOCTYPE html>\n<html>\n<head>\n<meta charset="utf-8">\n</head>\n<body>\n'
            + markup + '\n</body>\n</html>\n';
    }
    let doc = markup;
    if (!CHARSET_RE.test(doc)) {
        const open = HEAD_OPEN_RE.test(doc) ? HEAD_OPEN_RE : HTML_OPEN_RE;
        doc = doc.replace(open, (tag) => `${tag}\n<meta charset="utf-8">`);
    }
    if (!DOCTYPE_RE.test(doc)) {
        doc = '<!DOCTYPE html>\n' + doc;
    }
    return doc.endsWith('\n') ? doc : doc + '\n';
}

Object.assign(window.__testHelpers, {
    pasteFlavor: (text, html, behavior) => pasteFlavor(text, html, behavior),
    pastedHtmlDocument: (html) => pastedHtmlDocument(html),
    getPasteHtmlBehavior: () => window.getPasteHtmlBehavior(),
});
