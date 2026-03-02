// --- Tooltip Smart Positioning ---
// Monitors [data-tooltip] elements and flips position when near viewport edges.

(function() {
    const MARGIN = 8; // px from viewport edge

    function computePosition(el) {
        const rect = el.getBoundingClientRect();
        const vh = window.innerHeight;
        const vw = window.innerWidth;

        const spaceBelow = vh - rect.bottom;
        const spaceAbove = rect.top;
        const spaceRight = vw - rect.right;
        const spaceLeft = rect.left;

        // Default: below. Flip if not enough room.
        if (spaceBelow < 40 && spaceAbove > spaceBelow) return 'above';
        if (spaceBelow >= 40) return 'below';
        if (spaceRight < 40 && spaceLeft > 40) return 'left';
        if (spaceLeft < 40 && spaceRight > 40) return 'right';
        return 'below';
    }

    document.addEventListener('mouseenter', function(e) {
        const el = e.target.closest?.('[data-tooltip]');
        if (!el) return;

        const pos = computePosition(el);
        if (pos !== 'below') {
            el.setAttribute('data-tooltip-pos', pos);
        }
    }, true);

    document.addEventListener('mouseleave', function(e) {
        const el = e.target.closest?.('[data-tooltip]');
        if (!el) return;

        el.removeAttribute('data-tooltip-pos');
    }, true);

    // --- Settings: tooltips enabled/disabled ---
    async function loadTooltipSetting() {
        try {
            const val = await window.go.main.App.GetSetting('tooltips_enabled');
            if (val === 'false') {
                document.body.classList.add('tooltips-disabled');
            }
        } catch (e) {
            // Setting doesn't exist yet — tooltips enabled by default
        }
    }

    // Expose toggle for settings UI
    window.toggleTooltips = async function(enabled) {
        if (enabled) {
            document.body.classList.remove('tooltips-disabled');
        } else {
            document.body.classList.add('tooltips-disabled');
        }
        try {
            await window.go.main.App.SetSetting('tooltips_enabled', enabled ? 'true' : 'false');
        } catch (e) {
            console.error('Failed to save tooltip setting:', e);
        }
    };

    // Load on startup
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', loadTooltipSetting);
    } else {
        loadTooltipSetting();
    }
})();
