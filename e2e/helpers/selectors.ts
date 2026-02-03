// Centralized DOM selectors for mahpastes UI
// Using data-testid where possible, falling back to semantic selectors

export const selectors = {
  // Header
  header: {
    root: 'header',
    title: 'header h1',
    searchInput: '#search-input',
    watchButton: '#toggle-watch-view-btn',
    watchIndicator: '#watch-indicator',
    archiveButton: '#toggle-archive-view-btn',
    clearAllButton: '#delete-all-temp-btn',
  },

  // Upload zone
  upload: {
    dropZone: '#drop-zone',
    fileInput: '#file-input',
    selectButton: '#file-select-btn',
    expirationSelect: '#expiration-select',
  },

  // Clip gallery
  gallery: {
    container: '#gallery',
    clipCard: '#gallery > li',
    clipCardByName: (name: string) => `#gallery > li[data-filename="${name.toLowerCase()}"]`,
    clipCardById: (id: string) => `#gallery > li[data-id="${id}"]`,
    clipCheckbox: '.clip-checkbox',
    clipImage: '#gallery > li img',
    clipPreview: '.preview-container',
    clipTitle: '#gallery > li p',
    clipType: '#gallery > li span',
    expirationBadge: '.absolute.top-2.left-2',
    emptyState: '#empty-state',
  },

  // Clip card actions (visible on hover)
  clipActions: {
    copyPath: '[data-action="copy-path"]',
    save: '[data-action="save-file"]',
    edit: '[data-action="edit"]',
    archive: '[data-action="archive"]',
    delete: '[data-action="delete"]',
    view: '[data-action="open-lightbox"]',
  },

  // Bulk toolbar
  bulk: {
    toolbar: '#bulk-toolbar',
    selectAllCheckbox: '#select-all-checkbox',
    selectedCount: '#selected-count',
    compareButton: '#bulk-compare-btn',
    downloadButton: '#bulk-download-btn',
    archiveButton: '#bulk-archive-btn',
    deleteButton: '#bulk-delete-btn',
  },

  // Lightbox
  lightbox: {
    overlay: '#lightbox',
    image: '#lightbox-img',
    prevButton: '#lightbox-prev',
    nextButton: '#lightbox-next',
    closeButton: '#lightbox-close',
    bar: '.lightbox-bar',
    zoomSlider: '#lightbox-zoom-slider',
    zoomInfo: '#lightbox-zoom-info',
    aiActions: '#lightbox-ai-actions',
  },

  // Image editor
  editor: {
    modal: '#editor-modal',
    canvas: '#editor-canvas',
    toolbar: '.editor-toolbar',
    tools: {
      brush: '[data-tool="brush"]',
      line: '[data-tool="line"]',
      rectangle: '[data-tool="rectangle"]',
      circle: '[data-tool="circle"]',
      text: '[data-tool="text"]',
      eraser: '[data-tool="eraser"]',
    },
    colorPicker: '#editor-color',
    brushSize: '#editor-brush-size',
    opacity: '#editor-opacity',
    undoButton: '#editor-undo',
    redoButton: '#editor-redo',
    saveButton: '#editor-save',
    cancelButton: '#editor-close',
  },

  // Image comparison
  comparison: {
    modal: '#comparison-modal',
    modeFade: '#mode-fade',
    modeSlider: '#mode-slider',
    rangeSlider: '#comparison-range',
    zoomInButton: '#zoom-in',
    zoomOutButton: '#zoom-out',
    fitButton: '#zoom-fit',
    closeButton: '#comparison-close',
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

  // Toast notifications
  toast: {
    container: '#toast',
    message: '#toast', // Toast text is set directly on the container
  },

  // Text editor
  textEditor: {
    modal: '#text-editor-modal',
    textarea: '#text-editor-content',
    saveButton: '#text-editor-save',
    cancelButton: '#text-editor-cancel',
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
  },

  // Watch folder auto-tag
  watchAutoTag: {
    select: '[data-testid="watch-folder-auto-tag"]',
  },

  // Plugins
  plugins: {
    modalButton: '#open-plugins-btn',
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
} as const;

export type Selectors = typeof selectors;
