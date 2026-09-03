// jsdom (used by Vitest here) does not implement `window.matchMedia`, which
// Ant Design's responsive utilities (Segmented, Grid breakpoints, ...) call
// unconditionally. Stub it so component tests can mount those components.
if (typeof window !== "undefined" && !window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}
