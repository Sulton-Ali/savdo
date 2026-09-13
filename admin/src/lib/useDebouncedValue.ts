import { useEffect, useState } from "react";

/**
 * Returns `value`, delayed by `delayMs` of debounce — the returned value
 * only updates once `value` has stopped changing for `delayMs` (default
 * 300, matching the `SEARCH_DEBOUNCE_MS` every list page used inline before
 * this hook). Replaces the repeated `useEffect` + `setTimeout` debounce
 * pattern; the caller keeps its own input state for typing responsiveness
 * and only reacts to the debounced value (e.g. to push it into a route's
 * search params).
 */
export function useDebouncedValue<T>(value: T, delayMs = 300): T {
  const [debounced, setDebounced] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return debounced;
}
