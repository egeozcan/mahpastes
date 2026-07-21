// Shared sanitized GitHub-Flavored Markdown rendering.
const MarkdownRenderer = (() => {
    const MAX_SOURCE_BYTES = 2 * 1024 * 1024;
    const allowedTags = [
        'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'p', 'br', 'hr',
        'strong', 'em', 'del', 'code', 'pre', 'blockquote',
        'ul', 'ol', 'li', 'a', 'table', 'thead', 'tbody', 'tr', 'th', 'td',
        'span', 'input',
    ];
    const imageMarkerAttribute = 'data-mp-markdown-image';
    const allowedAttributes = ['href', 'title', 'type', 'checked', 'disabled', imageMarkerAttribute];

    if (typeof marked !== 'undefined') {
        marked.setOptions({ gfm: true, breaks: false });
    }

    function slugBase(text) {
        return text
            .trim()
            .toLocaleLowerCase()
            .replace(/[^\p{L}\p{N}\s-]/gu, '')
            .replace(/\s+/g, '-')
            .replace(/-+/g, '-');
    }

    function addHeadingIDs(container) {
        const counts = new Map();
        container.querySelectorAll('h1,h2,h3,h4,h5,h6').forEach((heading) => {
            const base = slugBase(heading.textContent || '') || 'section';
            const count = counts.get(base) || 0;
            heading.id = count === 0 ? base : `${base}-${count}`;
            counts.set(base, count + 1);
        });
    }

    function makeMessage(message, kind = 'empty') {
        const element = document.createElement('p');
        element.className = `markdown-preview-message markdown-preview-message-${kind}`;
        element.textContent = message;
        return element;
    }

    function createImagePlaceholder(descriptor) {
        const placeholder = document.createElement('span');
        placeholder.className = 'markdown-image-placeholder';
        placeholder.dataset.markdownImagePlaceholder = '';
        const label = document.createElement('span');
        label.className = 'markdown-image-label';
        label.textContent = descriptor.alt || 'Markdown image';
        placeholder.appendChild(label);
        return placeholder;
    }

    function render(source) {
        const container = document.createElement('div');
        container.className = 'markdown-content plugin-md-content';
        container.markdownImages = [];

        if (new TextEncoder().encode(source).byteLength > MAX_SOURCE_BYTES) {
            container.appendChild(makeMessage('Document too large to preview.', 'error'));
            return container;
        }

        try {
            const marker = (typeof crypto !== 'undefined' && crypto.randomUUID)
                ? crypto.randomUUID()
                : `${Date.now()}-${Math.random()}`;
            const images = [];
            const renderer = new marked.Renderer();
            renderer.image = ({ href, title, text }) => {
                const index = images.push({ source: href || '', title: title || '', alt: text || '' }) - 1;
                return `<span ${imageMarkerAttribute}="${marker}:${index}"></span>`;
            };
            const rawHTML = marked.parse(source, { renderer });
            const cleanHTML = DOMPurify.sanitize(rawHTML, {
                ALLOWED_TAGS: allowedTags,
                ALLOWED_ATTR: allowedAttributes,
                ALLOW_DATA_ATTR: false,
            });
            container.innerHTML = cleanHTML;
            container.querySelectorAll('input[type="checkbox"]').forEach((checkbox) => {
                checkbox.disabled = true;
            });
            container.querySelectorAll(`[${imageMarkerAttribute}]`).forEach((element) => {
                const value = element.getAttribute(imageMarkerAttribute) || '';
                const prefix = `${marker}:`;
                const index = value.startsWith(prefix) ? Number(value.slice(prefix.length)) : -1;
                const descriptor = Number.isInteger(index) && images[index] ? images[index] : null;
                if (!descriptor) {
                    element.remove();
                    return;
                }
                const placeholder = createImagePlaceholder(descriptor);
                descriptor.placeholder = placeholder;
                container.markdownImages.push(descriptor);
                element.replaceWith(placeholder);
            });
            addHeadingIDs(container);
        } catch (error) {
            console.error('Markdown rendering failed:', error);
            container.replaceChildren(makeMessage('Markdown preview unavailable.', 'error'));
        }
        return container;
    }

    return { render };
})();
