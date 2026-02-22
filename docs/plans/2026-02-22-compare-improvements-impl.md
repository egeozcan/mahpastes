# Compare Feature Improvements Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Improve the image comparison modal with diff visualization, swap/info features, fixed stretch/alignment, keyboard shortcuts via ShortcutManager, and UI polish.

**Architecture:** Add a `GetImageDiff` Go method on `App` (47th of ~49 limit) that extracts the pixel-diff logic from `plugin/api_image.go` into a shared helper. Frontend gets a third "Diff" mode alongside Fade/Slider, a swap button, image info in the header, and 10 keyboard shortcuts registered on a new `comparison` ShortcutManager context. Stretch/alignment bugs are fixed by using viewport-relative sizing.

**Tech Stack:** Go (Wails), Vanilla JS, Tailwind CSS, Playwright e2e tests

---

### Task 1: Add `GetImageDiff` Go Backend Method

**Files:**
- Modify: `app.go` (add `DiffResult` struct + `GetImageDiff` method)
- Modify: `plugin/api_image.go` (extract shared diff helper)

**Step 1: Extract shared diff function from plugin/api_image.go**

Move the pixel-comparison logic into an exported function that both the Lua API and the new App method can call. Add a `threshold` parameter (currently hardcoded to 30).

In `plugin/api_image.go`, add after line 929 (before the `diff` method):

```go
// DiffImages compares two images pixel-by-pixel and returns a diff image + similarity score.
// threshold controls sensitivity: lower = more differences shown (typical range 10-50, default 30).
func DiffImages(imgA, imgB image.Image, threshold float64) (diffImg *image.RGBA, similarity float64) {
	boundsA := imgA.Bounds()
	w := boundsA.Dx()
	h := boundsA.Dy()

	if w > 2000 || h > 2000 {
		scale := math.Min(2000.0/float64(w), 2000.0/float64(h))
		w = int(math.Max(1, math.Round(float64(w)*scale)))
		h = int(math.Max(1, math.Round(float64(h)*scale)))
	}

	var a, b *image.RGBA
	if imgA.Bounds().Dx() != w || imgA.Bounds().Dy() != h {
		a = resizeImage(imgA, w, h)
	} else {
		a = convertToRGBA(imgA)
	}
	if imgB.Bounds().Dx() != w || imgB.Bounds().Dy() != h {
		b = resizeImage(imgB, w, h)
	} else {
		b = convertToRGBA(imgB)
	}

	diffImg = image.NewRGBA(image.Rect(0, 0, w, h))
	totalDiff := 0.0
	totalPixels := float64(w * h)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ra, ga, ba, _ := a.At(x, y).RGBA()
			rb, gb, bb, _ := b.At(x, y).RGBA()

			dr := math.Abs(float64(ra>>8) - float64(rb>>8))
			dg := math.Abs(float64(ga>>8) - float64(gb>>8))
			db := math.Abs(float64(ba>>8) - float64(bb>>8))

			avgDiff := (dr + dg + db) / 3.0
			totalDiff += avgDiff / 255.0

			if avgDiff > threshold {
				intensity := uint8(math.Min(255, avgDiff*3))
				diffImg.SetRGBA(x, y, color.RGBA{R: intensity, G: 0, B: 0, A: 255})
			} else {
				diffImg.SetRGBA(x, y, color.RGBA{
					R: uint8(ra >> 9),
					G: uint8(ga >> 9),
					B: uint8(ba >> 9),
					A: 255,
				})
			}
		}
	}

	similarity = 1.0 - (totalDiff / totalPixels)
	if similarity < 0 {
		similarity = 0
	}
	return diffImg, similarity
}
```

Also export `EncodeImagePNG` (rename `encodeImagePNG` → `EncodeImagePNG`), `ResizeImage` (rename `resizeImage` → keep private, use through `DiffImages`), and `ConvertToRGBA` similarly. Actually — simpler approach: just export `DiffImages` and `EncodeImagePNG`. The `DiffImages` function inlines the resize/convert calls so nothing else needs exporting.

Rename `encodeImagePNG` to `EncodeImagePNG` at line 132 and update all callers in the same file (there are several — use find-replace).

**Step 2: Update the existing `diff` Lua method to use `DiffImages`**

Replace the body of `func (img *ImageAPI) diff(L *lua.LState) int` (lines 932-1027) to call `DiffImages`:

```go
func (img *ImageAPI) diff(L *lua.LState) int {
	clipIDA := L.CheckInt64(1)
	clipIDB := L.CheckInt64(2)

	imgA, _, err := img.loadClipImage(clipIDA)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("image A: " + err.Error()))
		return 2
	}

	imgB, _, err := img.loadClipImage(clipIDB)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString("image B: " + err.Error()))
		return 2
	}

	diffImg, similarity := DiffImages(imgA, imgB, 30.0)

	diffData, diffMime, err := EncodeImagePNG(diffImg)
	if err != nil {
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	result := L.NewTable()
	result.RawSetString("similarity", lua.LNumber(similarity))
	result.RawSetString("diff_data", lua.LString(diffData))
	result.RawSetString("diff_mime_type", lua.LString(diffMime))
	L.Push(result)
	return 1
}
```

**Step 3: Add `DiffResult` struct and `GetImageDiff` method to app.go**

Add near other type definitions (after `ClipData` struct around line 253):

```go
// DiffResult returned by GetImageDiff
type DiffResult struct {
	Similarity  float64 `json:"similarity"`
	DiffDataUrl string  `json:"diff_data_url"` // data:image/png;base64,...
}
```

Add the method (e.g. after `SetHiddenTags` at end of file):

```go
// GetImageDiff compares two image clips and returns a visual diff + similarity score.
// threshold controls sensitivity (10-50, default 30).
func (a *App) GetImageDiff(clipIdA, clipIdB int64, threshold int) (*DiffResult, error) {
	if threshold < 1 {
		threshold = 1
	}
	if threshold > 100 {
		threshold = 100
	}

	imgAPI := plugin.NewImageAPI(a.db)
	// Use the exported LoadClipImage — but that's on ImageAPI. We need direct DB access.
	// Instead, load images directly here using the same pattern:
	loadImg := func(clipID int64) (image.Image, error) {
		var data []byte
		var contentType string
		err := a.db.QueryRow("SELECT data, content_type FROM clips WHERE id = ?", clipID).Scan(&data, &contentType)
		if err != nil {
			return nil, fmt.Errorf("clip %d: %w", clipID, err)
		}
		if !strings.HasPrefix(contentType, "image/") {
			return nil, fmt.Errorf("clip %d is not an image", clipID)
		}
		decoded, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, fmt.Errorf("failed to decode clip %d: %w", clipID, err)
		}
		return decoded, nil
	}

	imgA, err := loadImg(clipIdA)
	if err != nil {
		return nil, err
	}
	imgB, err := loadImg(clipIdB)
	if err != nil {
		return nil, err
	}

	diffImg, similarity := plugin.DiffImages(imgA, imgB, float64(threshold))
	diffData, diffMime, err := plugin.EncodeImagePNG(diffImg)
	if err != nil {
		return nil, fmt.Errorf("failed to encode diff: %w", err)
	}

	return &DiffResult{
		Similarity:  similarity,
		DiffDataUrl: fmt.Sprintf("data:%s;base64,%s", diffMime, diffData),
	}, nil
}
```

Add required imports to app.go: `"bytes"`, `"image"`, `_ "image/gif"`, `_ "image/jpeg"`, `_ "image/png"`, `_ "golang.org/x/image/webp"` — check which are already imported and only add missing ones.

**Step 4: Regenerate Wails bindings**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails generate module`

Verify `frontend/wailsjs/go/main/App.js` now includes `GetImageDiff`.

**Step 5: Build and verify**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build`
Expected: Clean build with no errors.

**Step 6: Commit**

```bash
git add app.go plugin/api_image.go frontend/wailsjs/
git commit -m "feat: add GetImageDiff backend method for comparison diff mode"
```

---

### Task 2: UI Polish — Fix Off-Palette Colors and Slider Width

**Files:**
- Modify: `frontend/js/modals.js:999-1044` (fix blue classes in `updateComparisonView`)
- Modify: `frontend/css/modals.css:552-570` (widen slider)
- Modify: `frontend/index.html:467-474` (fix slider handle icon to left-right arrows)

**Step 1: Fix mode button colors in modals.js**

In `updateComparisonView()` (modals.js), replace all `text-blue-600` references with `text-stone-800`:

Line 1004: `'text-blue-600'` → `'text-stone-800'`
Line 1006: `'text-blue-600'` → `'text-stone-800'`
Line 1014: `'text-blue-600'` → `'text-stone-800'`
Line 1016: `'text-blue-600'` → `'text-stone-800'`

Also replace `'text-gray-500'` → `'text-stone-500'` (lines 1005, 1007, 1015, 1017).

**Step 2: Fix stretch button colors in modals.js**

Lines 1035-1036: Replace `'bg-blue-600'` → `'bg-stone-800'` (both add and remove calls).
Line 1042: Replace `'bg-gray-100'` → `'bg-stone-100'` in the remove call, and line 1043 in the add call.

**Step 3: Widen the range slider in modals.css**

Line 553: Change `width: 140px;` → `width: 200px;`

**Step 4: Fix slider handle icon in index.html**

Replace the current down-arrow SVG in the slider handle (lines 469-472) with a left-right arrows icon:

```html
<svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
        d="M7 16l-4-4m0 0l4-4m-4 4h18m-4 4l4-4m0 0l-4-4" />
</svg>
```

**Step 5: Commit**

```bash
git add frontend/js/modals.js frontend/css/modals.css frontend/index.html
git commit -m "fix: use stone palette colors and widen comparison slider"
```

---

### Task 3: Add Diff Mode Button + Third Image Element to HTML

**Files:**
- Modify: `frontend/index.html:447-537` (add diff button, diff image, similarity badge, swap button, image info, A/B labels)

**Step 1: Add swap button and image info to header**

Replace the comparison header (lines 449-457) with:

```html
<div class="comparison-header">
    <div class="flex items-center gap-3">
        <h3 class="text-sm font-medium">Compare</h3>
        <span id="comparison-similarity" class="text-[10px] font-medium text-stone-400 hidden"></span>
    </div>
    <div class="flex items-center gap-2">
        <span id="comparison-image-info" class="text-[10px] font-medium text-stone-500 hidden md:inline"></span>
        <button id="comparison-swap" class="p-1.5 hover:bg-white/10 rounded-md transition-colors"
            aria-label="Swap images" title="Swap images (S)">
            <svg class="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5"
                    d="M7.5 21L3 16.5m0 0L7.5 12M3 16.5h13.5m0-13.5L21 7.5m0 0L16.5 12M21 7.5H7.5" />
            </svg>
        </button>
        <button id="comparison-close" class="p-1.5 hover:bg-white/10 rounded-md transition-colors"
            aria-label="Close comparison">
            <svg class="w-5 h-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M6 18L18 6M6 6l12 12">
                </path>
            </svg>
        </button>
    </div>
</div>
```

**Step 2: Add diff image element and A/B labels to viewport**

Inside `comparison-container` (after the slider-line div, before closing `</div>` of container at line 475), add:

```html
<img id="comparison-img-diff" src="" alt="Diff Image" class="comparison-img hidden">
```

Add A/B labels as the first children of `comparison-viewport` (inside the viewport div, before comparison-container):

```html
<span id="comparison-label-a" class="absolute top-4 left-4 z-20 text-[10px] font-semibold text-white/60 bg-black/30 px-1.5 py-0.5 rounded pointer-events-none">A</span>
<span id="comparison-label-b" class="absolute top-4 right-4 z-20 text-[10px] font-semibold text-white/60 bg-black/30 px-1.5 py-0.5 rounded pointer-events-none">B</span>
```

Note: The viewport div needs `relative` positioning for the absolute labels — it already has `position: relative` in CSS.

**Step 3: Add Diff button to mode toggle**

In the mode toggle group (line 481-486), add a third button after the Slider button:

```html
<button id="mode-diff"
    class="px-3 py-1 rounded text-xs font-medium transition-all text-stone-500 hover:text-stone-700"
    title="Diff mode (3)">Diff</button>
```

**Step 4: Update the range slider label default**

No change needed — it already says "Opacity" by default which is correct for fade mode.

**Step 5: Commit**

```bash
git add frontend/index.html
git commit -m "feat: add diff mode button, swap, image info, and A/B labels to comparison HTML"
```

---

### Task 4: Implement Diff Mode, Swap, and Image Info in JavaScript

**Files:**
- Modify: `frontend/js/app.js:59-91` (add new DOM refs, state vars, event listeners)
- Modify: `frontend/js/modals.js:956-1099` (update comparison functions)

**Step 1: Add new DOM references and state in app.js**

After line 78 (`const zoomFitBtn = ...`), add:

```javascript
const modeDiffBtn = document.getElementById('mode-diff');
const comparisonImgDiff = document.getElementById('comparison-img-diff');
const comparisonSwapBtn = document.getElementById('comparison-swap');
const comparisonSimilarity = document.getElementById('comparison-similarity');
const comparisonImageInfo = document.getElementById('comparison-image-info');
const comparisonLabelA = document.getElementById('comparison-label-a');
const comparisonLabelB = document.getElementById('comparison-label-b');
```

After line 89 (`let isStretched = false;`), add:

```javascript
let comparisonClipIds = []; // [idA, idB] - track which clips are being compared
let diffCache = new Map(); // Map<threshold, {dataUrl, similarity}> - cache diff results
```

**Step 2: Add event listeners for new elements in app.js**

After line 229 (`modeSliderBtn.addEventListener...`), add:

```javascript
modeDiffBtn.addEventListener('click', () => { comparisonMode = 'diff'; updateComparisonView(); });
comparisonSwapBtn.addEventListener('click', swapComparisonImages);
```

**Step 3: Update `openComparisonModal` in modals.js**

Replace the function (lines 958-986) to track clip IDs, show image info, and reset diff cache:

```javascript
async function openComparisonModal() {
    const selectedArray = Array.from(selectedIds);
    if (selectedArray.length !== 2) return;

    lastFocusedElementBeforeComparison = document.activeElement;
    comparisonClipIds = [...selectedArray];
    diffCache.clear();

    // Load both images as base64
    try {
        const [dataUrl1, dataUrl2] = await Promise.all([
            getImageDataUrl(selectedArray[0]),
            getImageDataUrl(selectedArray[1])
        ]);
        comparisonImgBottom.src = dataUrl1;
        comparisonImgTop.src = dataUrl2;
        comparisonImgDiff.src = '';
    } catch (error) {
        console.error('Failed to load images for comparison:', error);
        return;
    }

    // Reset state
    comparisonMode = 'fade';
    zoomLevel = 1;
    isStretched = false;
    comparisonRange.value = 50;

    // Show image info after images load
    updateComparisonImageInfo();

    updateComparisonView();
    comparisonModal.classList.add('active');
    comparisonModal.focus();
}
```

**Step 4: Add `updateComparisonImageInfo` function in modals.js**

Add after `openComparisonModal`:

```javascript
function updateComparisonImageInfo() {
    const imgA = comparisonImgBottom;
    const imgB = comparisonImgTop;

    function formatSize(bytes) {
        if (bytes < 1024) return bytes + 'B';
        if (bytes < 1048576) return (bytes / 1024).toFixed(1) + 'KB';
        return (bytes / 1048576).toFixed(1) + 'MB';
    }

    function waitForLoad(img) {
        return new Promise(resolve => {
            if (img.naturalWidth > 0) return resolve();
            img.addEventListener('load', resolve, { once: true });
        });
    }

    Promise.all([waitForLoad(imgA), waitForLoad(imgB)]).then(() => {
        // Get clip metadata from gallery cards
        const cards = comparisonClipIds.map(id =>
            document.querySelector(`#gallery li[data-id="${id}"]`)
        );
        const typeA = cards[0]?.dataset.type?.split('/')[1]?.toUpperCase() || 'IMG';
        const typeB = cards[1]?.dataset.type?.split('/')[1]?.toUpperCase() || 'IMG';

        const sizeA = cards[0]?.dataset.size ? formatSize(parseInt(cards[0].dataset.size)) : '';
        const sizeB = cards[1]?.dataset.size ? formatSize(parseInt(cards[1].dataset.size)) : '';

        const infoA = `A: ${imgA.naturalWidth}\u00d7${imgA.naturalHeight} ${typeA}${sizeA ? ' ' + sizeA : ''}`;
        const infoB = `B: ${imgB.naturalWidth}\u00d7${imgB.naturalHeight} ${typeB}${sizeB ? ' ' + sizeB : ''}`;

        comparisonImageInfo.textContent = `${infoA}  \u2502  ${infoB}`;
        comparisonImageInfo.classList.remove('hidden');
    });
}
```

Note: Check whether `data-size` is stored on gallery cards. If not, we can get size from `ClipPreview.Size` — check `createClipCard` in ui.js. If size isn't on the card's `dataset`, add `card.dataset.size = clip.size;` in `createClipCard`. If it's not worth adding, just omit size from the info display.

**Step 5: Add `swapComparisonImages` function in modals.js**

Add after `updateComparisonImageInfo`:

```javascript
function swapComparisonImages() {
    // Swap the src of bottom and top
    const tmpSrc = comparisonImgBottom.src;
    comparisonImgBottom.src = comparisonImgTop.src;
    comparisonImgTop.src = tmpSrc;

    // Swap tracked clip IDs
    comparisonClipIds.reverse();

    // Clear diff cache (diff depends on order — image A's dimensions are used as reference)
    diffCache.clear();
    if (comparisonMode === 'diff') {
        loadDiffImage();
    }

    // Update info display
    updateComparisonImageInfo();
}
```

**Step 6: Update `updateComparisonView` in modals.js to handle diff mode**

Replace the entire `updateComparisonView` function (lines 999-1049) with:

```javascript
function updateComparisonView() {
    const value = comparisonRange.value;

    // Mode button states
    const modes = [
        { btn: modeFadeBtn, mode: 'fade' },
        { btn: modeSliderBtn, mode: 'slider' },
        { btn: modeDiffBtn, mode: 'diff' },
    ];
    for (const { btn, mode } of modes) {
        if (comparisonMode === mode) {
            btn.classList.add('bg-white', 'shadow-sm', 'text-stone-800');
            btn.classList.remove('text-stone-500');
        } else {
            btn.classList.remove('bg-white', 'shadow-sm', 'text-stone-800');
            btn.classList.add('text-stone-500');
        }
    }

    // Show/hide images based on mode
    if (comparisonMode === 'diff') {
        comparisonImgBottom.classList.add('hidden');
        comparisonImgTopWrapper.classList.add('hidden');
        comparisonImgDiff.classList.remove('hidden');
        comparisonSliderLine.classList.add('hidden');
        comparisonRangeLabel.textContent = 'Threshold';
        comparisonLabelA.classList.add('hidden');
        comparisonLabelB.classList.add('hidden');

        // Load diff if not cached for this threshold
        loadDiffImage();
    } else {
        comparisonImgBottom.classList.remove('hidden');
        comparisonImgTopWrapper.classList.remove('hidden');
        comparisonImgDiff.classList.add('hidden');
        comparisonLabelA.classList.remove('hidden');
        comparisonLabelB.classList.remove('hidden');

        if (comparisonMode === 'fade') {
            comparisonImgTopWrapper.style.clipPath = 'none';
            comparisonImgTop.style.opacity = value / 100;
            comparisonSliderLine.classList.add('hidden');
            comparisonRangeLabel.textContent = 'Opacity';
        } else {
            comparisonImgTopWrapper.style.clipPath = `inset(0 ${100 - value}% 0 0)`;
            comparisonImgTop.style.opacity = 1;
            comparisonSliderLine.classList.remove('hidden');
            comparisonSliderLine.style.left = `${value}%`;
            comparisonRangeLabel.textContent = 'Position';
        }
    }

    // Alignment & Stretch
    comparisonContainer.style.justifyContent = alignHSelect.value;
    comparisonContainer.style.alignItems = alignVSelect.value;

    if (isStretched) {
        comparisonImgBottom.style.width = '100%';
        comparisonImgBottom.style.height = '100%';
        comparisonImgBottom.style.objectFit = 'fill';
        comparisonImgTop.style.objectFit = 'fill';
        comparisonImgDiff.style.width = '100%';
        comparisonImgDiff.style.height = '100%';
        comparisonImgDiff.style.objectFit = 'fill';
        toggleStretchBtn.classList.add('bg-stone-800', 'text-white');
        toggleStretchBtn.classList.remove('bg-stone-100');
    } else {
        comparisonImgBottom.style.width = 'auto';
        comparisonImgBottom.style.height = 'auto';
        comparisonImgBottom.style.objectFit = 'contain';
        comparisonImgTop.style.objectFit = 'contain';
        comparisonImgDiff.style.width = 'auto';
        comparisonImgDiff.style.height = 'auto';
        comparisonImgDiff.style.objectFit = 'contain';
        toggleStretchBtn.classList.remove('bg-stone-800', 'text-white');
        toggleStretchBtn.classList.add('bg-stone-100');
    }

    // Zoom
    comparisonContainer.style.transform = `scale(${zoomLevel})`;
    zoomLevelEl.textContent = `${Math.round(zoomLevel * 100)}%`;
}
```

**Step 7: Add `loadDiffImage` function in modals.js**

Add after `updateComparisonView`:

```javascript
async function loadDiffImage() {
    const threshold = parseInt(comparisonRange.value);
    const cacheKey = threshold;

    if (diffCache.has(cacheKey)) {
        const cached = diffCache.get(cacheKey);
        comparisonImgDiff.src = cached.dataUrl;
        comparisonSimilarity.textContent = `${(cached.similarity * 100).toFixed(1)}% similar`;
        comparisonSimilarity.classList.remove('hidden');
        return;
    }

    if (comparisonClipIds.length !== 2) return;

    try {
        const result = await window.go.main.App.GetImageDiff(
            comparisonClipIds[0],
            comparisonClipIds[1],
            threshold
        );
        diffCache.set(cacheKey, {
            dataUrl: result.diff_data_url,
            similarity: result.similarity,
        });
        // Only apply if still in diff mode (user might have switched)
        if (comparisonMode === 'diff') {
            comparisonImgDiff.src = result.diff_data_url;
            comparisonSimilarity.textContent = `${(result.similarity * 100).toFixed(1)}% similar`;
            comparisonSimilarity.classList.remove('hidden');
        }
    } catch (error) {
        console.error('Failed to load diff image:', error);
        comparisonSimilarity.textContent = 'Diff failed';
        comparisonSimilarity.classList.remove('hidden');
    }
}
```

**Step 8: Update `closeComparisonModal` in modals.js to clean up diff state**

Replace the function (lines 988-997):

```javascript
function closeComparisonModal() {
    comparisonModal.classList.remove('active');
    comparisonSimilarity.classList.add('hidden');
    comparisonImageInfo.classList.add('hidden');
    setTimeout(() => {
        comparisonImgBottom.src = '';
        comparisonImgTop.src = '';
        comparisonImgDiff.src = '';
        diffCache.clear();
        comparisonClipIds = [];
        if (lastFocusedElementBeforeComparison) {
            lastFocusedElementBeforeComparison.focus();
        }
    }, 300);
}
```

**Step 9: Hide similarity badge when not in diff mode**

Already handled — `updateComparisonView` shows A/B labels and hides diff elements when not in diff mode. But we also need to hide the similarity badge. Add to the `else` branch (non-diff modes) in `updateComparisonView`:

```javascript
comparisonSimilarity.classList.add('hidden');
```

**Step 10: Commit**

```bash
git add frontend/js/app.js frontend/js/modals.js
git commit -m "feat: implement diff mode, swap images, and image info display"
```

---

### Task 5: Fix Stretch and Alignment Bugs

**Files:**
- Modify: `frontend/js/modals.js` (fix stretch logic in `updateComparisonView`, fix `zoomFit`)
- Modify: `frontend/css/modals.css` (ensure viewport container supports stretch properly)

**Step 1: Fix stretch in updateComparisonView**

The stretch fix is already included in Task 4 Step 6 — `width: '100%'` and `height: '100%'` instead of `'1000px'`. Verify this works correctly by checking the comparison-container CSS.

The `comparison-container` needs `width: 100%; height: 100%;` when stretched. Add to the stretch branch in `updateComparisonView`:

```javascript
if (isStretched) {
    comparisonContainer.style.width = '100%';
    comparisonContainer.style.height = '100%';
    // ... existing stretch code
} else {
    comparisonContainer.style.width = '';
    comparisonContainer.style.height = '';
    // ... existing non-stretch code
}
```

**Step 2: Ensure top image wrapper matches bottom image dimensions**

The top image wrapper needs to track the bottom image's rendered size. Add a ResizeObserver in `openComparisonModal` (after showing the modal):

```javascript
// Ensure top image wrapper matches bottom image rendered size
if (!window._comparisonResizeObserver) {
    window._comparisonResizeObserver = new ResizeObserver(() => {
        if (comparisonMode !== 'diff') {
            comparisonImgTopWrapper.style.width = comparisonImgBottom.offsetWidth + 'px';
            comparisonImgTopWrapper.style.height = comparisonImgBottom.offsetHeight + 'px';
        }
    });
}
window._comparisonResizeObserver.observe(comparisonImgBottom);
```

Disconnect in `closeComparisonModal`:

```javascript
if (window._comparisonResizeObserver) {
    window._comparisonResizeObserver.disconnect();
}
```

**Step 3: Show dimension mismatch warning**

In `updateComparisonImageInfo`, after setting the info text, check if dimensions differ:

```javascript
// Show warning icon if dimensions differ
const dimsDiffer = imgA.naturalWidth !== imgB.naturalWidth || imgA.naturalHeight !== imgB.naturalHeight;
if (dimsDiffer) {
    comparisonImageInfo.textContent += ' \u26a0';  // ⚠ warning sign
}
```

**Step 4: Commit**

```bash
git add frontend/js/modals.js frontend/css/modals.css
git commit -m "fix: stretch uses viewport-relative sizing, top wrapper tracks bottom image"
```

---

### Task 6: Add `comparison` Context to ShortcutManager

**Files:**
- Modify: `frontend/js/shortcuts.js:21-29` (add category), `43-94` (add context detection), `233` (add to priority), `405-412` (add to hierarchy)

**Step 1: Add comparison to CATEGORY_ORDER and CATEGORY_LABELS**

Line 21: Add `'comparison'` to `CATEGORY_ORDER` array:
```javascript
const CATEGORY_ORDER = ['navigation', 'gallery', 'clip', 'lightbox', 'comparison', 'bulk', 'system'];
```

Lines 22-29: Add to `CATEGORY_LABELS`:
```javascript
const CATEGORY_LABELS = {
    navigation: 'Navigation',
    gallery: 'Gallery',
    clip: 'Clip Actions',
    lightbox: 'Lightbox',
    comparison: 'Comparison',
    bulk: 'Bulk Actions',
    system: 'System',
};
```

**Step 2: Update `getActiveContexts` to detect comparison modal**

In `getActiveContexts()`, the comparison modal currently causes an early `return []` (line 57). Change this so it returns the comparison context instead.

Replace line 57:
```javascript
if (comparisonModal && comparisonModal.classList.contains('active')) return [];
```

With:
```javascript
if (comparisonModal && comparisonModal.classList.contains('active')) {
    contexts.push('comparison');
    return contexts;
}
```

This makes comparison shortcuts work while the modal is open, with `global` as fallback.

**Step 3: Add `comparison` to dispatch priority**

Line 233: Update the priority array:
```javascript
const priority = ['clip', 'bulk', 'lightbox', 'comparison', 'watch', 'gallery', 'global'];
```

**Step 4: Add `comparison` to context overlap hierarchy**

Lines 407-411: Update the hierarchy object:
```javascript
const hierarchy = {
    global: ['gallery', 'lightbox', 'comparison', 'watch', 'bulk', 'clip'],
    gallery: ['clip', 'bulk'],
};
```

**Step 5: Commit**

```bash
git add frontend/js/shortcuts.js
git commit -m "feat: add comparison context to ShortcutManager"
```

---

### Task 7: Register Comparison Keyboard Shortcuts

**Files:**
- Modify: `frontend/js/app.js` (add 10 shortcut registrations after lightbox shortcuts, before ShortcutManager.init)

**Step 1: Register all comparison shortcuts**

Add after the lightbox shortcuts block (after line 638, before `await ShortcutManager.init()` at line 641):

```javascript
// Comparison
ShortcutManager.register({
    id: 'compare-mode-fade', label: 'Fade Mode', category: 'comparison',
    defaultKey: '1', context: 'comparison',
    callback: () => { comparisonMode = 'fade'; comparisonRange.value = 50; updateComparisonView(); }
});
ShortcutManager.register({
    id: 'compare-mode-slider', label: 'Slider Mode', category: 'comparison',
    defaultKey: '2', context: 'comparison',
    callback: () => { comparisonMode = 'slider'; comparisonRange.value = 50; updateComparisonView(); }
});
ShortcutManager.register({
    id: 'compare-mode-diff', label: 'Diff Mode', category: 'comparison',
    defaultKey: '3', context: 'comparison',
    callback: () => { comparisonMode = 'diff'; comparisonRange.value = 30; updateComparisonView(); }
});
ShortcutManager.register({
    id: 'compare-swap', label: 'Swap Images', category: 'comparison',
    defaultKey: 's', context: 'comparison',
    callback: () => swapComparisonImages()
});
ShortcutManager.register({
    id: 'compare-zoom-in', label: 'Zoom In', category: 'comparison',
    defaultKey: '+', context: 'comparison',
    callback: () => { zoomLevel = Math.min(zoomLevel * 1.2, 5); updateComparisonView(); }
});
ShortcutManager.register({
    id: 'compare-zoom-out', label: 'Zoom Out', category: 'comparison',
    defaultKey: '-', context: 'comparison',
    callback: () => { zoomLevel = Math.max(zoomLevel / 1.2, 0.1); updateComparisonView(); }
});
ShortcutManager.register({
    id: 'compare-zoom-fit', label: 'Fit to Viewport', category: 'comparison',
    defaultKey: '0', context: 'comparison',
    callback: () => zoomFit()
});
ShortcutManager.register({
    id: 'compare-range-left', label: 'Adjust Range Left', category: 'comparison',
    defaultKey: 'ArrowLeft', context: 'comparison',
    callback: () => {
        comparisonRange.value = Math.max(0, parseInt(comparisonRange.value) - 5);
        updateComparisonView();
    }
});
ShortcutManager.register({
    id: 'compare-range-right', label: 'Adjust Range Right', category: 'comparison',
    defaultKey: 'ArrowRight', context: 'comparison',
    callback: () => {
        comparisonRange.value = Math.min(100, parseInt(comparisonRange.value) + 5);
        updateComparisonView();
    }
});
ShortcutManager.register({
    id: 'compare-close', label: 'Close Comparison', category: 'comparison',
    defaultKey: 'Escape', context: 'comparison',
    callback: () => closeComparisonModal()
});
```

**Step 2: Commit**

```bash
git add frontend/js/app.js
git commit -m "feat: register 10 comparison keyboard shortcuts via ShortcutManager"
```

---

### Task 8: Update E2E Test Selectors and Fixtures

**Files:**
- Modify: `e2e/helpers/selectors.ts:124-134` (add new selectors)
- Modify: `e2e/fixtures/test-fixtures.ts:931-967` (add new helper methods)

**Step 1: Add new selectors**

Update the `comparison` object in selectors.ts:

```typescript
comparison: {
    modal: '#comparison-modal',
    modeFade: '#mode-fade',
    modeSlider: '#mode-slider',
    modeDiff: '#mode-diff',
    rangeSlider: '#comparison-range',
    rangeLabel: '#comparison-range-label',
    zoomInButton: '#zoom-in',
    zoomOutButton: '#zoom-out',
    fitButton: '#zoom-fit',
    closeButton: '#comparison-close',
    swapButton: '#comparison-swap',
    similarity: '#comparison-similarity',
    imageInfo: '#comparison-image-info',
    diffImage: '#comparison-img-diff',
    labelA: '#comparison-label-a',
    labelB: '#comparison-label-b',
    stretchButton: '#toggle-stretch',
},
```

**Step 2: Add new fixture methods**

Add to the `AppHelper` class after the existing comparison methods (after `isComparisonOpen`):

```typescript
async setComparisonMode(mode: 'fade' | 'slider' | 'diff'): Promise<void> {
    if (mode === 'fade') {
        await this.page.locator(selectors.comparison.modeFade).click();
    } else if (mode === 'slider') {
        await this.page.locator(selectors.comparison.modeSlider).click();
    } else {
        await this.page.locator(selectors.comparison.modeDiff).click();
    }
}

async swapComparisonImages(): Promise<void> {
    await this.page.locator(selectors.comparison.swapButton).click();
}

async getComparisonSimilarity(): Promise<string> {
    return this.page.locator(selectors.comparison.similarity).textContent() ?? '';
}

async getComparisonImageInfo(): Promise<string> {
    return this.page.locator(selectors.comparison.imageInfo).textContent() ?? '';
}

async isComparisonDiffVisible(): Promise<boolean> {
    const img = this.page.locator(selectors.comparison.diffImage);
    return !(await img.evaluate(el => el.classList.contains('hidden')));
}

async getComparisonRangeLabel(): Promise<string> {
    return this.page.locator(selectors.comparison.rangeLabel).textContent() ?? '';
}
```

Note: The existing `setComparisonMode` method signature changes from `mode: 'fade' | 'slider'` to `mode: 'fade' | 'slider' | 'diff'`. This is backwards-compatible since the old two values still work.

**Step 3: Commit**

```bash
git add e2e/helpers/selectors.ts e2e/fixtures/test-fixtures.ts
git commit -m "test: add comparison selectors and fixture methods for diff, swap, info"
```

---

### Task 9: Write E2E Tests for New Features

**Files:**
- Modify: `e2e/tests/images/comparison.spec.ts` (add test suites for diff mode, swap, image info, keyboard shortcuts)

**Step 1: Add Diff Mode tests**

Add a new `test.describe` block after the existing "Slider Mode Controls" block:

```typescript
test.describe('Diff Mode', () => {
    test('should switch to diff mode', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();
        await app.setComparisonMode('diff');

        // Diff image should be visible
        const isDiffVisible = await app.isComparisonDiffVisible();
        expect(isDiffVisible).toBe(true);
    });

    test('should show similarity score in diff mode', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();
        await app.setComparisonMode('diff');

        // Wait for diff to load
        await app.page.waitForFunction(
            (sel) => {
                const el = document.querySelector(sel);
                return el && !el.classList.contains('hidden') && el.textContent.includes('similar');
            },
            selectors.comparison.similarity,
            { timeout: 10000 }
        );

        const similarity = await app.getComparisonSimilarity();
        expect(similarity).toContain('similar');
    });

    test('should show threshold label in diff mode', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();
        await app.setComparisonMode('diff');

        const label = await app.getComparisonRangeLabel();
        expect(label).toBe('Threshold');
    });
});
```

**Step 2: Add Swap tests**

```typescript
test.describe('Swap Images', () => {
    test('should swap images when swap button clicked', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();

        // Get initial bottom image src
        const initialSrc = await app.page.locator('#comparison-img-bottom').getAttribute('src');

        await app.swapComparisonImages();

        // Bottom image should now have different src
        const swappedSrc = await app.page.locator('#comparison-img-bottom').getAttribute('src');
        expect(swappedSrc).not.toBe(initialSrc);
    });
});
```

**Step 3: Add Image Info tests**

```typescript
test.describe('Image Info', () => {
    test('should display image dimensions', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 150, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(300, 250, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();

        // Wait for image info to appear
        await app.page.waitForSelector(`${selectors.comparison.imageInfo}:not(.hidden)`, { timeout: 5000 });

        const info = await app.getComparisonImageInfo();
        expect(info).toContain('200');
        expect(info).toContain('150');
        expect(info).toContain('300');
        expect(info).toContain('250');
    });
});
```

**Step 4: Add Keyboard Shortcut tests**

```typescript
test.describe('Keyboard Shortcuts', () => {
    test('should switch to diff mode with key 3', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();

        await app.page.keyboard.press('3');

        const isDiffVisible = await app.isComparisonDiffVisible();
        expect(isDiffVisible).toBe(true);
    });

    test('should switch modes with keys 1 and 2', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();

        // Switch to slider
        await app.page.keyboard.press('2');
        let label = await app.getComparisonRangeLabel();
        expect(label).toBe('Position');

        // Switch back to fade
        await app.page.keyboard.press('1');
        label = await app.getComparisonRangeLabel();
        expect(label).toBe('Opacity');
    });

    test('should swap images with s key', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();

        const initialSrc = await app.page.locator('#comparison-img-bottom').getAttribute('src');
        await app.page.keyboard.press('s');
        const swappedSrc = await app.page.locator('#comparison-img-bottom').getAttribute('src');
        expect(swappedSrc).not.toBe(initialSrc);
    });

    test('should close comparison with Escape', async ({ app }) => {
        const files = await Promise.all([
            createTempFile(generateTestImage(200, 200, [255, 0, 0]), 'png'),
            createTempFile(generateTestImage(200, 200, [0, 255, 0]), 'png'),
        ]);
        const filenames = files.map((f) => path.basename(f));

        await app.uploadFiles(files);
        await app.selectClips(filenames);
        await app.openComparison();
        await app.page.keyboard.press('Escape');

        const isOpen = await app.isComparisonOpen();
        expect(isOpen).toBe(false);
    });
});
```

**Step 5: Run all tests**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass, including old and new comparison tests.

**Step 6: Commit**

```bash
git add e2e/tests/images/comparison.spec.ts
git commit -m "test: add e2e tests for diff mode, swap, image info, and keyboard shortcuts"
```

---

### Task 10: Final Verification and Data-Size Attribute Check

**Files:**
- Possibly modify: `frontend/js/ui.js` (add `data-size` to clip cards if missing)

**Step 1: Check if `data-size` exists on clip cards**

Search `createClipCard` in `ui.js` for `dataset.size` or `data-size`. If it's not set, add:

```javascript
li.dataset.size = clip.size;
```

alongside the existing `li.dataset.type = clip.content_type;` and `li.dataset.id = clip.id;`.

**Step 2: Run the full e2e test suite**

Run: `cd /Users/egecan/Code/mahpastes/e2e && npm test`
Expected: All tests pass.

**Step 3: Build the app**

Run: `cd /Users/egecan/Code/mahpastes && ~/go/bin/wails build`
Expected: Clean build.

**Step 4: Manual smoke test (if dev server available)**

Run: `make dev` and verify:
- Open comparison with 2 images
- Fade and Slider modes work as before
- Diff mode shows red highlighted differences + similarity score
- Swap button swaps images
- Image info shows dimensions
- Keyboard shortcuts 1/2/3/S/+/-/0/arrows/Esc work
- Stretch uses full viewport, not hardcoded 1000px
- All colors are stone palette (no blue)

**Step 5: Final commit if any fixes needed**

```bash
git add -A
git commit -m "fix: final comparison improvements polish"
```
