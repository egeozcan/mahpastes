/**
 * Ambient declarations for the globals that live on the app's `window` inside
 * page.evaluate() callbacks.
 *
 * The app frontend is plain classic scripts, so its Wails bindings and test
 * hooks are untyped `window` properties. Without these declarations every
 * `window.go.main.App.X()` in a spec is a compile error, which buried the real
 * type errors in the noise. `any` is deliberate — these mirror generated Wails
 * bindings and debug-only helpers, and typing them properly would mean
 * hand-maintaining a copy of the whole binding surface.
 */
declare global {
  interface Window {
    /** Wails-generated bindings: window.go.main.{App,PluginService,ShareService,…} */
    go: any;
    /** Test-only helpers the frontend installs for e2e (loadClips, setActiveTagFilters, …). */
    __testHelpers: any;
    /** Classic-script globals the specs drive directly. */
    ContextMenu: any;
    renderFolderCards: any;
    setHiddenTagsState: any;
    loadClips: any;
  }

  /**
   * serve.js declares this as a top-level `const` in a classic script, so it
   * lives in the global lexical scope — reachable as a bare name from an
   * evaluate() callback, but never as a property of `window`.
   */
  const configuredEntries: Map<number, any> | undefined;
}

export {};
