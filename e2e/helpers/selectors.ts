// Centralized DOM selectors for mahpastes UI
// Using data-testid where possible, falling back to semantic selectors

export const selectors = {
  // Header
  header: {
    root: 'header',
    title: 'header h1',
    searchInput: '#search-input',
    drawerToggle: '#drawer-toggle-btn',
    addButton: '#add-btn',
    archiveButton: '#header-archive-btn',
    clipControls: '#clip-controls',
  },

  // Bottom bar
  bottomBar: {
    root: '#bottom-bar',
    addButton: '#add-btn',
    expirySelect: '#expiry-select',
    clipCount: '#clip-count',
  },

  // Nav drawer
  drawer: {
    overlay: '#drawer-overlay',
    panel: '#nav-drawer',
    closeButton: '#drawer-close-btn',
    watchButton: '#view-tab-watch',
    watchIndicator: '#watch-indicator',
    archiveButton: '#toggle-archive-view-btn',
    clearAllButton: '#delete-all-temp-btn',
    settingsButton: '#open-settings-btn',
    pluginActionsContainer: '#drawer-plugin-actions',
    pluginAction: '#drawer-plugin-actions [data-global-action]',
  },

  // Upload
  upload: {
    fileInput: '#file-input',
    dropOverlay: '#drop-overlay',
  },

  // Clip gallery
  gallery: {
    container: '#gallery',
    clipCard: '#gallery > li:not([data-folder])',
    clipCardByName: (name: string) => `#gallery > li[data-filename="${name.toLowerCase()}"]`,
    clipCardById: (id: string) => `#gallery > li[data-id="${id}"]`,
    clipCheckbox: '.clip-checkbox',
    clipImage: '#gallery > li img',
    clipPreview: '.preview-container',
    clipTitle: '#gallery > li p',
    clipType: '#gallery > li span',
    expirationBadge: '.absolute.top-2.left-2',
    emptyState: '#empty-state',
    marqueeOverlay: '.marquee-overlay',
  },

  // Clip card actions (now in dropdown menu)
  clipActions: {
    menuTrigger: '[data-action="menu"]',
    view: '[data-action="open-lightbox"]',
    dragOut: '[data-action="drag-out"]',
    dragOutPreparing: '[data-action="drag-out"].is-preparing',
    dragOutArming: '[data-action="drag-out"].is-hover-arming',
    dragOutProgress: '[data-action="drag-out"] .clip-drag-icon-progress',
    dragOutSpinner: '[data-action="drag-out"] .clip-drag-icon-spinner',
  },

  // Card menu dropdown
  cardMenu: {
    dropdown: '.card-menu-dropdown',
    open: '.card-menu-dropdown [data-action="open"]',
    openWith: '.card-menu-dropdown [data-action="open-with"]',
    copyTrigger: '[data-submenu="copy"]',
    pluginsTrigger: '[data-submenu="plugins"]',
    submenu: '.card-menu-submenu',
    copyPath: '.card-menu-submenu [data-action="copy-path"]',
    copyFile: '.card-menu-submenu [data-action="copy-file"]',
    copyContents: '.card-menu-submenu [data-action="copy-contents"]',
    save: '.card-menu-dropdown [data-action="save-file"]',
    edit: '.card-menu-dropdown [data-action="edit"]',
    tags: '.card-menu-dropdown [data-action="tags"]',
    archive: '.card-menu-dropdown [data-action="archive"]',
    setExpiration: '.card-menu-dropdown [data-action="set-expiration"]',
    cancelExpiration: '.card-menu-dropdown [data-action="cancel-expiration"]',
    delete: '.card-menu-dropdown [data-action="delete"]',
    mergeDuplicates: '.card-menu-dropdown [data-action="merge-duplicates"]',
    metadata: '.card-menu-dropdown [data-action="metadata"]',
    rename: '.card-menu-dropdown [data-action="rename"]',
    pluginAction: '.card-menu-submenu [data-action="plugin"]',
    divider: '.card-menu-dropdown .card-menu-divider',
  },

  // Metadata modal
  metadata: {
    modal: '[data-testid="metadata-modal"]',
    closeButton: '[data-testid="metadata-close"]',
    list: '[data-testid="metadata-list"]',
    addButton: '[data-testid="metadata-add"]',
    saveButton: '[data-testid="metadata-save"]',
    emptyState: '[data-testid="metadata-empty"]',
    row: '[data-testid="metadata-row"]',
    keyInput: '[data-testid="metadata-key"]',
    valueInput: '[data-testid="metadata-value"]',
    deleteRowButton: '[data-testid="metadata-delete-row"]',
    systemInfo: '[data-testid="metadata-system-info"]',
    systemRow: '[data-testid="metadata-system-row"]',
  },

  // Sort
  sort: {
    button: '[data-testid="sort-button"]',
    popover: '[data-testid="sort-popover"]',
    option: (field: string) => `[data-testid="sort-option-${field}"]`,
  },

  // Bulk toolbar
  bulk: {
    toolbar: '#bulk-toolbar',
    selectAllCheckbox: '#select-all-checkbox',
    selectedCount: '#selected-count',
    moreButton: '#bulk-more-btn',
    copyButton: '#bulk-copy-btn',
    downloadButton: '#bulk-download-btn',
    archiveButton: '#bulk-archive-btn',
    deleteButton: '#bulk-delete-btn',
    expiryButton: '#bulk-expiry-btn',
    cancelExpiryButton: '#bulk-cancel-expiry-btn',
    contextMenu: '.card-menu-dropdown[data-source="bulk"]',
    contextMenuItem: (action: string) =>
      `.card-menu-dropdown[data-source="bulk"] [data-action="${action}"]`,
  },

  // Expiration
  expiration: {
    popover: '.expiration-popover',
    badge: '.absolute.top-2.left-2',
  },

  // Lightbox
  lightbox: {
    overlay: '#lightbox',
    viewport: '#lightbox-viewport',
    panLayer: '#lightbox-pan-layer',
    image: '#lightbox-img',
    caption: '#lightbox-caption',
    status: '#lightbox-status',
    loading: '#lightbox-loading',
    error: '#lightbox-error',
    retryButton: '#lightbox-retry',
    prevButton: '#lightbox-prev',
    nextButton: '#lightbox-next',
    closeButton: '#lightbox-close',
    bar: '.lightbox-bar',
    imageInfo: '#lightbox-image-info',
    fileTrigger: '#lightbox-file-menu-trigger',
    zoomOut: '#lightbox-zoom-out',
    zoomSlider: '#lightbox-zoom-slider',
    zoomIn: '#lightbox-zoom-in',
    zoomFit: '#lightbox-zoom-fit',
    zoomActual: '#lightbox-zoom-actual',
    zoomInfo: '#lightbox-zoom-info',
    pluginActions: '#lightbox-plugin-actions',
    pluginTrigger: '#lightbox-plugin-menu-trigger',
    pluginMenu: '#lightbox-plugin-menu',
    pluginMenuItem: '.lightbox-plugin-menu-item',
  },

  // Image editor
  editor: {
    modal: '#editor-modal',
    canvas: '#editor-canvas',
    toolbar: '.editor-toolbar',
    tools: {
      select: '[data-tool="select"]',
      crop: '[data-tool="crop"]',
      brush: '[data-tool="brush"]',
      eraser: '[data-tool="eraser"]',
      line: '[data-tool="line"]',
      arrow: '[data-tool="arrow"]',
      rectangle: '[data-tool="rectangle"]',
      circle: '[data-tool="circle"]',
      text: '[data-tool="text"]',
      anonymize: '[data-tool="anonymize"]',
      eyedropper: '[data-tool="eyedropper"]',
    },
    colorPicker: '#editor-color',
    brushSize: '#editor-brush-size',
    opacity: '#editor-opacity',
    undoButton: '#editor-undo',
    redoButton: '#editor-redo',
    saveButton: '#editor-save',
    cancelButton: '#editor-close',
    zoomIn: '#editor-zoom-in',
    zoomOut: '#editor-zoom-out',
    zoomFit: '#editor-zoom-fit',
    zoom100: '#editor-zoom-100',
    zoomDisplay: '#editor-zoom-display',
    rotateCW: '#editor-rotate-cw',
    rotateCCW: '#editor-rotate-ccw',
    flipH: '#editor-flip-h',
    flipV: '#editor-flip-v',
    // Crop options
    cropOptions: '#editor-crop-options',
    cropConfirm: '#editor-crop-confirm',
    cropCancel: '#editor-crop-cancel',
    cropRatio: '#editor-crop-ratio',
    cropSwap: '#editor-crop-swap',
    cropRotate: '#editor-crop-rotate',
    // Anonymize options
    anonymizeOptions: '#editor-anonymize-options',
    anonBrush: '#editor-anon-brush',
    anonRect: '#editor-anon-rect',
    anonPixelate: '#editor-anon-pixelate',
    anonBlur: '#editor-anon-blur',
    // Text options
    textOptions: '#editor-text-options',
    fontSize: '#editor-font-size',
    // Overlay canvas
    overlayCanvas: '#editor-overlay-canvas',
  },

  // Image comparison
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

  // Watch folders view
  watch: {
    view: '#watch-view',
    globalToggle: '#global-watch-toggle',
    globalLabel: '#global-watch-label',
    folderCount: '#watch-folder-count',
    folderList: '#watch-folder-list',
    folderCard: '#watch-folder-list > li',
    addFolderZone: '#add-folder-zone',
    addFolderButton: '#add-folder-btn',
    backButton: '#watch-back-btn',
  },

  // View tabs (3-way toggle in drawer)
  viewTabs: {
    clips: '#view-tab-clips',
    watch: '#view-tab-watch',
    serve: '#view-tab-serve',
  },

  // Serve view
  serve: {
    view: '#serve-view',
    list: '#serve-list',
    backBtn: '#serve-back-btn',
    addBtn: '#add-serve-btn',
    tagPicker: '#serve-tag-picker',
    toggleBtn: '.serve-toggle-btn',
    urlCopy: '.serve-url-copy',
  },

  // Watch folder card
  watchFolder: {
    path: 'p.text-stone-700',
    pauseToggle: '[data-action="toggle-pause"]',
    deleteButton: '[data-action="remove"]',
  },

  // Watch folder edit modal
  watchEdit: {
    modal: '#folder-modal',
    pathDisplay: '#folder-modal-path',
    filterAll: '#filter-all',
    filterImages: '#filter-images',
    filterDocuments: '#filter-documents',
    filterVideos: '#filter-videos',
    regexInput: '#filter-regex',
    processExisting: '#process-existing',
    autoArchive: '#auto-archive',
    saveButton: '#folder-modal-save',
    cancelButton: '#folder-modal-cancel',
  },

  // Confirmation dialog
  confirm: {
    dialog: '#confirm-dialog',
    title: '#confirm-title',
    message: '#confirm-message',
    confirmButton: '#confirm-yes-btn',
    cancelButton: '#confirm-no-btn',
  },

  // Conflict dialog (upload duplicate detection)
  conflict: {
    dialog: '#conflict-dialog',
    title: '#conflict-title',
    message: '#conflict-message',
    fileList: '#conflict-file-list',
    overwriteButton: '#conflict-overwrite-btn',
    keepButton: '#conflict-keep-btn',
    skipButton: '#conflict-skip-btn',
  },

  // Pasted path dialog (text vs. referenced file)
  pathPaste: {
    dialog: '#path-paste-dialog',
    title: '#path-paste-title',
    message: '#path-paste-message',
    fileList: '#path-paste-file-list',
    fileButton: '#path-paste-file-btn',
    textButton: '#path-paste-text-btn',
    cancelButton: '#path-paste-cancel-btn',
  },

  // Prompt dialog
  prompt: {
    dialog: '#prompt-dialog',
    title: '#prompt-title',
    input: '#prompt-input',
    saveButton: '#prompt-save-btn',
    cancelButton: '#prompt-cancel-btn',
  },

  // Toast notifications
  toast: {
    container: '#toast',
    message: '#toast', // Toast text is set directly on the container
  },

  // Text editor
  textEditor: {
    modal: '#editor-modal',
    // CodeMirror replaced the textarea. `mount` is the container (carries
    // data-wrap); `editor` is the focusable, typeable content element.
    mount: '#text-editor-mount',
    editor: '#text-editor-mount .cm-content',
    scroller: '#text-editor-mount .cm-scroller',
    lineNumbers: '#text-editor-mount .cm-gutters .cm-lineNumbers',
    // The stock @codemirror/search panel must never be mounted.
    stockSearchPanel: '#text-editor-mount .cm-panels',
    currentFilename: '#editor-current-filename',
    saveButton: '#editor-save-in-place',
    saveAsButton: '#editor-save',
    saveAsInput: '#editor-filename',
    saveAsCancelButton: '#editor-save-as-cancel',
    cancelButton: '#editor-close',
    toolbar: '#text-editor-toolbar',
    findToggle: '#text-editor-find-toggle',
    findPanel: '#text-editor-find-panel',
    findInput: '#text-editor-find',
    findNextButton: '#text-editor-find-next',
    findPreviousButton: '#text-editor-find-previous',
    replaceInput: '#text-editor-replace',
    replaceButton: '#text-editor-replace-current',
    replaceAllButton: '#text-editor-replace-all',
    searchStatus: '#text-editor-search-status',
    matchCaseToggle: '#text-editor-match-case',
    wholeWordToggle: '#text-editor-whole-word',
    searchMatches: '#text-editor-mount [data-search-match]',
    activeSearchMatch: '#text-editor-mount [data-search-active="true"]',
    wrapToggle: '#text-editor-wrap-toggle',
    formatJSONButton: '#text-editor-format-json',
    validationStatus: '#text-editor-validation-status',
    cursorStatus: '#text-editor-cursor-status',
    characterStatus: '#text-editor-character-status',
    draftStatus: '#text-editor-draft-status',
    modeLabel: '#editor-mode-label',
    // Format-neutral Preview/Edit tabs and panel. Every registered text clip has
    // both modes; the descriptor decides which one it opens in.
    modeTabs: '#editor-mode-tabs',
    previewTab: '#editor-preview-tab',
    editTab: '#editor-edit-tab',
    previewPanel: '#editor-preview-panel',
    previewContent: '#markdown-preview-content',
    sourcePreview: '#source-preview-content .source-preview',
    // Syntax-highlighted token spans. Deliberately the same class in both panels:
    // Edit's highlighting reuses the Source Preview classes verbatim, so the two
    // read as one system. NOTE: CodeMirror virtualizes off-screen lines, so only
    // count these on short documents — assert absence, or use a small document.
    editorToken: '#text-editor-mount .source-token',
    sourcePreviewToken: '#source-preview-content .source-token',
    sourcePreviewLines: '#source-preview-content .source-preview-line',
    sourcePreviewPlain: '#source-preview-content .source-preview-plain',
    sourcePreviewNote: '#source-preview-content .source-preview-note',
    // Bounded CSV/TSV table preview and its temporary interpretation controls.
    tablePreview: '#table-preview-content .table-preview',
    tablePreviewTable: '#table-preview-content .table-preview-table',
    tablePreviewNote: '#table-preview-content .table-preview-note',
    tablePreviewHeaderCell: '#table-preview-content .table-preview-table thead th:not(.table-preview-row-number)',
    tablePreviewRow: '#table-preview-content .table-preview-table tbody tr',
    tablePreviewCell: '#table-preview-content .table-preview-table tbody td',
    tablePreviewPaddedCell: '#table-preview-content .table-preview-table tbody td[data-padded="true"]',
    tableControls: '#editor-table-controls',
    tableDelimiterSelect: '#editor-table-delimiter',
    tableHeaderSelect: '#editor-table-header',
    tableSummary: '#editor-table-summary',
    // Byte-safety screen: shown instead of both modes for invalid UTF-8.
    unavailablePanel: '#editor-unavailable-panel',
    unavailableReason: '#editor-unavailable-reason',
    degradedNotice: '#text-editor-degraded-notice',
    // Collapsible bottom diagnostics drawer.
    diagnostics: '#editor-diagnostics',
    diagnosticsToggle: '#editor-diagnostics-toggle',
    diagnosticsSummary: '#editor-diagnostics-summary',
    diagnosticsBody: '#editor-diagnostics-body',
    diagnosticsList: '#editor-diagnostics-list',
    diagnosticsRow: '#editor-diagnostics-list .editor-diagnostic-row',
    diagnosticsRowLocation: '#editor-diagnostics-list .editor-diagnostic-location',
    diagnosticsNotice: '#editor-diagnostics-notice',
    diagnosticsEmpty: '#editor-diagnostics-empty',
    // Inline markers inside CodeMirror. NOTE: CodeMirror virtualizes off-screen
    // lines, so a DOM count of these is only meaningful on a short document.
    errorMarker: '#text-editor-mount [data-diagnostic-severity="error"]',
    possibleMarker: '#text-editor-mount [data-diagnostic-severity="possible-issue"]',
    errorMarkerLine: '#text-editor-mount [data-diagnostic-line="error"]',
  },

  // Tags
  tags: {
    filterButton: '[data-testid="tag-filter-button"]',
    filterDropdown: '[data-testid="tag-filter-dropdown"]',
    filterList: '#tag-filter-list',
    filterBadge: '#tag-filter-badge',
    clearFiltersButton: '#clear-tag-filters',
    activeTagsContainer: '#active-tags-container',
    tagCheckbox: (name: string) => `[data-testid="tag-checkbox-${name}"]`,
    tagPill: (name: string) => `[data-testid="tag-pill-${name}"]`,
    popover: '[data-testid="tag-popover"]',
    popoverList: '#tag-popover-list',
    createTagInput: '[data-testid="create-tag-input"]',
    createTagButton: '#create-tag-btn',
    bulkTagButton: '#bulk-tag-btn',
    clipTagsContainer: '.clip-tags',
    cardTagButton: '[data-action="tags"]',
    // Folder mode
    folderModeButton: '[data-testid="folder-mode-button"]',
    folderCard: (name: string) => `[data-testid="folder-card-${name}"]`,
    homeIcon: '[data-testid="folder-home-icon"]',
  },

  // Watch folder auto-tag
  watchAutoTag: {
    select: '[data-testid="watch-folder-auto-tag"]',
  },

  // Plugins
  plugins: {
    modalButton: '#open-plugins-btn', // now inside drawer
    modal: '[data-testid="plugins-modal"]',
    closeButton: '#plugins-close',
    importButton: '[data-testid="import-plugin-btn"]',
    list: '[data-testid="plugins-list"]',
    emptyState: '#plugins-empty-state',
    pluginCard: (id: number) => `[data-testid="plugin-card-${id}"]`,
    pluginToggle: (id: number) => `[data-testid="plugin-toggle-${id}"]`,
    pluginRemove: (id: number) => `[data-testid="remove-plugin-${id}"]`,
    expandToggle: '[data-action="toggle-expand"]',
  },

  // Plugin review modal
  pluginReview: {
    modal: '[data-testid="plugin-review-modal"]',
    cancelButton: '[data-testid="plugin-review-cancel"]',
    approveButton: '[data-testid="plugin-review-approve"]',
    name: '#plugin-review-name',
    version: '#plugin-review-version',
    warning: '#plugin-review-warning',
    networkSection: '#plugin-review-network-section',
    fsSection: '#plugin-review-fs-section',
    clipboardSection: '#plugin-review-clipboard-section',
    eventsSection: '#plugin-review-events-section',
  },

  // Plugin URL install
  pluginURL: {
    installButton: '[data-testid="install-url-btn"]',
    input: '[data-testid="plugin-url-input"]',
    submitButton: '[data-testid="plugin-url-install-btn"]',
    container: '#plugin-url-container',
  },

  // Plugin update
  pluginUpdate: {
    button: (id: number) => `[data-testid="update-plugin-${id}"]`,
  },

  // Plugin settings
  pluginSettings: {
    section: '[data-settings-section]',
    settingField: (key: string) => `[data-setting-key="${key}"]`,
    textInput: '[data-setting-type="text"]',
    passwordInput: '[data-setting-type="password"]',
    checkbox: '[data-setting-type="checkbox"]',
    select: '[data-setting-type="select"]',
    passwordToggle: '[data-action="toggle-password"]',
  },

  // Plugin options dialog (for action parameters)
  pluginOptions: {
    modal: '#plugin-options-modal',
    form: '#plugin-options-form',
    cancelButton: '#plugin-options-cancel',
    submitButton: 'button[type="submit"][form="plugin-options-form"]',
    formField: (id: string) => `#plugin-options-form [name="${id}"]`,
  },

  // Settings modal
  settings: {
    modal: '#settings-modal',
    closeButton: '#settings-close',
    hiddenTagsList: '[data-testid="hidden-tags-list"]',
    hiddenTagRow: (name: string) => `[data-testid="hidden-tag-row-${name}"]`,
    hiddenTagToggle: (name: string) => `[data-testid="hidden-tag-toggle-${name}"]`,
    lightboxBackdropToggle: '[data-testid="lightbox-backdrop-close-toggle"]',
    lightboxBackdropLabel: '[data-testid="lightbox-backdrop-close-label"]',
    pastePathSection: '[data-testid="settings-paste-path-section"]',
    pastePathSelect: '[data-testid="paste-path-behavior-select"]',
  },

  // Settings - update interval
  settingsUpdateInterval: {
    select: '[data-testid="update-interval-select"]',
  },

  // Plugin result modal
  pluginResultModal: {
    modal: '#plugin-result-modal',
    title: '#plugin-result-title',
    body: '#plugin-result-body',
    closeButton: '#plugin-result-close',
    copyButton: '#plugin-result-copy',
    pasteButton: '#plugin-result-paste',
  },

  // Backup
  backup: {
    createButton: '[data-testid="create-backup-btn"]',
    restoreButton: '[data-testid="restore-backup-btn"]',
    restoreConfirmDialog: '#restore-confirm-dialog',
    restoreConfirmCancel: '#restore-confirm-cancel',
    restoreConfirmYes: '#restore-confirm-yes',
    restoreBackupInfo: '#restore-backup-info',
  },

  // Deduplication
  dedup: {
    badge: '.dedup-badge',
    deduplicateBtn: '#maintenance-deduplicate-btn',
  },

  // Maintenance modal
  maintenance: {
    openButton: '#open-maintenance-btn',
    modal: '#maintenance-modal',
    closeButton: '#maintenance-close',
    deduplicateButton: '#maintenance-deduplicate-btn',
    removeEmptyTagsButton: '#maintenance-remove-empty-tags-btn',
    compactDbButton: '#maintenance-compact-db-btn',
    markdownCacheSize: '#maintenance-markdown-cache-size',
    clearMarkdownCacheButton: '#maintenance-clear-markdown-cache-btn',
  },

  // Tooltips
  tooltips: {
    anyTooltip: '[data-tooltip]',
    headerTagFilter: '#tag-filter-btn[data-tooltip]',
    headerArchive: '#header-archive-btn[data-tooltip]',
    headerSort: '#sort-btn[data-tooltip]',
    headerMenu: '#drawer-toggle-btn[data-tooltip]',
    settingsToggle: '[data-testid="tooltips-toggle"]',
  },

  // Keyboard Shortcuts
  shortcuts: {
    cheatsheet: '[data-testid="shortcuts-cheatsheet"]',
    cheatsheetClose: '#shortcuts-cheatsheet-close',
    cheatsheetContent: '#shortcuts-cheatsheet-content',
    settingsSection: '[data-testid="settings-shortcuts-section"]',
    settingsList: '[data-testid="shortcuts-settings-list"]',
    resetButton: '[data-testid="shortcuts-reset-btn"]',
    shortcutRow: (id: string) => `[data-testid="shortcut-row-${id}"]`,
    shortcutBadge: (id: string) => `[data-testid="shortcut-badge-${id}"]`,
    focusedClip: '#gallery > li:focus-visible',
  },
  // Folder context menu + status badges (top level for convenience)
  folderCard: (name: string) => `[data-testid="folder-card-${name}"]`,
  folderStatusBadges: (name: string) => `[data-testid="folder-card-${name}"] .folder-status-badges`,
  folderBadgeServed: (name: string) => `[data-testid="folder-card-${name}"] .folder-badge-serve, [data-testid="folder-card-${name}"] .folder-badge-paused[data-kind="serve"]`,
  folderBadgeShared: (name: string) => `[data-testid="folder-card-${name}"] .folder-badge-share, [data-testid="folder-card-${name}"] .folder-badge-paused[data-kind="share"]`,
  folderBadgeServedActive: (name: string) => `[data-testid="folder-card-${name}"] .folder-badge-serve`,
  folderBadgeSharedActive: (name: string) => `[data-testid="folder-card-${name}"] .folder-badge-share`,
  folderContextMenu: '.card-menu-dropdown[data-source="folder"]',
  folderContextMenuItem: (action: string) => `.card-menu-dropdown[data-source="folder"] [data-action="${action}"]`,
  folderHidden: (name: string) => `[data-testid="folder-card-${name}"][data-hidden="true"]`,
} as const;

export type Selectors = typeof selectors;
