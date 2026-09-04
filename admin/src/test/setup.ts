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

// jsdom also has no `ResizeObserver`, which `Tree`/`TreeSelect` (rc-resize-
// observer) mount unconditionally. A no-op stub is enough for tests — they
// don't assert on resize-driven layout.
if (typeof window !== "undefined" && !window.ResizeObserver) {
  class ResizeObserverStub {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
  window.ResizeObserver = ResizeObserverStub as unknown as typeof ResizeObserver;
}
