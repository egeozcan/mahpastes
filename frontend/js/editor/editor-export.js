// Authoritative image export policy: returned MIME, bytes and filename always agree.
const EditorExport = (() => {
    const MIME_EXTENSIONS = {
        'image/png': '.png',
        'image/jpeg': '.jpg',
        'image/webp': '.webp',
    };

    function preferredMime(originalMime) {
        return Object.hasOwn(MIME_EXTENSIONS, originalMime) ? originalMime : 'image/png';
    }

    function normalizeFilename(filename, mime) {
        const extension = MIME_EXTENSIONS[mime] || '.png';
        const slash = Math.max(filename.lastIndexOf('/'), filename.lastIndexOf('\\'));
        const dot = filename.lastIndexOf('.');
        const stem = dot > slash ? filename.slice(0, dot) : filename;
        if (mime === 'image/jpeg' && /\.jpe?g$/i.test(filename)) return filename;
        return stem + extension;
    }

    function toBlob(canvas, mime, quality) {
        return new Promise((resolve, reject) => {
            canvas.toBlob(blob => blob ? resolve(blob) : reject(new Error('Image encoder returned no data')), mime, quality);
        });
    }

    async function encode(canvas, requestedMime) {
        const quality = requestedMime === 'image/jpeg' || requestedMime === 'image/webp' ? 0.92 : undefined;
        let blob = await toBlob(canvas, requestedMime, quality);
        if (blob.type !== requestedMime) {
            blob = await toBlob(canvas, 'image/png');
        }
        return blob;
    }

    async function blobToBase64(blob) {
        const bytes = new Uint8Array(await blob.arrayBuffer());
        let binary = '';
        const chunkSize = 0x8000;
        for (let offset = 0; offset < bytes.length; offset += chunkSize) {
            binary += String.fromCharCode(...bytes.subarray(offset, offset + chunkSize));
        }
        return btoa(binary);
    }

    async function exportCanvas({ canvas, originalMime, filename }) {
        const requestedMime = preferredMime(originalMime);
        let exportCanvas = canvas;
        if (requestedMime === 'image/jpeg') {
            exportCanvas = document.createElement('canvas');
            exportCanvas.width = canvas.width;
            exportCanvas.height = canvas.height;
            const context = exportCanvas.getContext('2d');
            context.fillStyle = '#ffffff';
            context.fillRect(0, 0, canvas.width, canvas.height);
            context.drawImage(canvas, 0, 0);
        }

        const blob = await encode(exportCanvas, requestedMime);
        const contentType = Object.hasOwn(MIME_EXTENSIONS, blob.type) ? blob.type : 'image/png';
        return {
            data: await blobToBase64(blob),
            contentType,
            filename: normalizeFilename(filename, contentType),
        };
    }

    return { exportCanvas, normalizeFilename, preferredMime };
})();
