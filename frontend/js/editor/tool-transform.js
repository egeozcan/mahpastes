// --- Transform Tool ---
// Whole-image rotation (90° CW/CCW) and flip operations. The opening-image
// baseline follows the same geometry so the eraser stays aligned.

const TransformTool = (() => {
    function drawTransformed(source, kind) {
        const output = document.createElement('canvas');
        const swapDimensions = kind === 'rotate-cw' || kind === 'rotate-ccw';
        output.width = swapDimensions ? source.height : source.width;
        output.height = swapDimensions ? source.width : source.height;
        const context = output.getContext('2d');

        if (kind === 'rotate-cw') {
            context.translate(output.width, 0);
            context.rotate(Math.PI / 2);
        } else if (kind === 'rotate-ccw') {
            context.translate(0, output.height);
            context.rotate(-Math.PI / 2);
        } else if (kind === 'flip-h') {
            context.translate(output.width, 0);
            context.scale(-1, 1);
        } else if (kind === 'flip-v') {
            context.translate(0, output.height);
            context.scale(1, -1);
        }
        context.drawImage(source, 0, 0);
        return output;
    }

    function baselineCanvas() {
        const canvas = EditorCore.canvas;
        if (!canvas) return null;
        const imageData = EditorCore.getBaselineRegion(0, 0, canvas.width, canvas.height);
        if (!imageData) return null;
        const baseline = document.createElement('canvas');
        baseline.width = canvas.width;
        baseline.height = canvas.height;
        baseline.getContext('2d').putImageData(imageData, 0, 0);
        return baseline;
    }

    function apply(kind) {
        if (EditorCore.prepareForAction('transform') === 'consumed') return;
        const canvas = EditorCore.canvas;
        const ctx = EditorCore.ctx;
        const overlay = EditorCore.overlayCanvas;
        if (!canvas || !ctx) return;

        const transformed = drawTransformed(canvas, kind);
        const baseline = baselineCanvas();
        const transformedBaseline = baseline ? drawTransformed(baseline, kind) : transformed;

        canvas.width = transformed.width;
        canvas.height = transformed.height;
        if (overlay) {
            overlay.width = transformed.width;
            overlay.height = transformed.height;
        }
        ctx.drawImage(transformed, 0, 0);
        EditorCore.setBaselineFromCanvas(transformedBaseline);
        EditorCore.syncOverlay();
        EditorCore.saveUndoState();
        if (typeof ZoomTool !== 'undefined') ZoomTool.zoomToFit();
    }

    return {
        rotateCW: () => apply('rotate-cw'),
        rotateCCW: () => apply('rotate-ccw'),
        flipH: () => apply('flip-h'),
        flipV: () => apply('flip-v'),
    };
})();
