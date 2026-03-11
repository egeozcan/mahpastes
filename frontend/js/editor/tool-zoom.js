const ZoomTool = (() => {
    const MIN_ZOOM = 0.1;   // 10%
    const MAX_ZOOM = 8.0;   // 800%
    const ZOOM_STEP = 0.1;

    function clampZoom(z) {
        return Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, z));
    }

    function zoomIn() {
        EditorCore.zoomLevel = clampZoom(EditorCore.zoomLevel + ZOOM_STEP);
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function zoomOut() {
        EditorCore.zoomLevel = clampZoom(EditorCore.zoomLevel - ZOOM_STEP);
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function zoomToFit() {
        const canvas = EditorCore.canvas;
        if (!canvas) return;
        const container = canvas.parentElement;
        if (!container) return;
        const containerRect = container.getBoundingClientRect();
        const padding = 48;
        const availW = containerRect.width - padding;
        const availH = containerRect.height - padding;
        const fitZoom = Math.min(availW / canvas.width, availH / canvas.height, 1);
        EditorCore.zoomLevel = clampZoom(fitZoom);
        EditorCore.panX = 0;
        EditorCore.panY = 0;
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function zoomTo100() {
        EditorCore.zoomLevel = 1;
        EditorCore.panX = 0;
        EditorCore.panY = 0;
        EditorCore.applyTransform();
        updateZoomDisplay();
    }

    function updateZoomDisplay() {
        const display = document.getElementById('editor-zoom-display');
        if (display) {
            display.textContent = Math.round(EditorCore.zoomLevel * 100) + '%';
        }
    }

    return {
        zoomIn, zoomOut, zoomToFit, zoomTo100, updateZoomDisplay,
    };
})();
