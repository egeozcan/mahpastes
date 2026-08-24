const ZoomTool = (() => {
    // Multiplicative, like the wheel handler: a fixed additive step is a 100%
    // jump when zoomed out to 0.1 and a 1% nudge at 8x, and stepping down from
    // a small fit-zoom would hit the clamp floor after one or two clicks.
    const ZOOM_FACTOR = 1.25;

    function zoomIn() {
        EditorCore.setZoom(EditorCore.zoomLevel * ZOOM_FACTOR);
        updateZoomDisplay();
    }

    function zoomOut() {
        EditorCore.setZoom(EditorCore.zoomLevel / ZOOM_FACTOR);
        updateZoomDisplay();
    }

    function zoomToFit() {
        const canvas = EditorCore.canvas;
        if (!canvas) return;
        const container = canvas.closest('.editor-canvas-container');
        if (!container) return;
        const containerRect = container.getBoundingClientRect();
        const padding = 48;
        const fitZoom = Math.min(
            (containerRect.width - padding) / canvas.width,
            (containerRect.height - padding) / canvas.height,
            1
        );
        EditorCore.resetZoom();
        EditorCore.setZoom(fitZoom);
        updateZoomDisplay();
    }

    function zoomTo100() {
        EditorCore.resetZoom();
        updateZoomDisplay();
    }

    function updateZoomDisplay() {
        const display = document.getElementById('editor-zoom-display');
        if (display) display.textContent = Math.round(EditorCore.zoomLevel * 100) + '%';
    }

    return { zoomIn, zoomOut, zoomToFit, zoomTo100, updateZoomDisplay };
})();
