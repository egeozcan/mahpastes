const transferStrategyAdapters = {
    'file-uri-v1': {
        setDragData(dataTransfer, preparedItem) {
            if (!dataTransfer || !preparedItem || !preparedItem.file_url) {
                return false;
            }

            const fileUrl = preparedItem.file_url;
            const contentType = preparedItem.content_type || 'application/octet-stream';
            const filename = preparedItem.filename || 'clip';
            const absPath = preparedItem.abs_path || '';

            // For DownloadURL, prefer the HTTP transfer URL when available.
            // Chromium's DownloadURL needs a real HTTP URL to produce CF_HDROP on Windows;
            // file:/// URIs fail with a network error.
            const downloadUrl = preparedItem.transfer_url
                ? `${location.origin}${preparedItem.transfer_url}`
                : fileUrl;

            // Populate multiple types for better app compatibility across WebView engines.
            // Some targets inspect URI types, others inspect plain text or DownloadURL.
            const textPayload = absPath || fileUrl;
            const payloads = [
                ['text/uri-list', `${fileUrl}\r\n`],
                ['URL', fileUrl],
                ['text', fileUrl],
                ['text/plain', textPayload],
                ['DownloadURL', `${contentType}:${filename}:${downloadUrl}`],
                ['public.url', fileUrl],
                ['public.file-url', fileUrl],
            ];
            if (absPath) {
                payloads.push(['text/path-list', absPath]);
                payloads.push(['public.utf8-plain-text', absPath]);
            }
            let writes = 0;
            for (const [type, value] of payloads) {
                try {
                    dataTransfer.setData(type, value);
                    writes += 1;
                } catch (_) {
                    // Ignore unsupported MIME types for the current runtime.
                }
            }
            if (writes === 0) {
                console.warn('Drag payload write failed for all transfer types');
            }
            return writes > 0;
        }
    },

    // Placeholder for future Linux-specific outbound drag support.
    'linux-fileuri-v1': {
        setDragData() {
            return false;
        }
    }
};

function setDragDataForStrategy(dataTransfer, preparedItem, strategy) {
    const adapter = transferStrategyAdapters[strategy];
    if (!adapter || typeof adapter.setDragData !== 'function') {
        return false;
    }
    return adapter.setDragData(dataTransfer, preparedItem);
}
