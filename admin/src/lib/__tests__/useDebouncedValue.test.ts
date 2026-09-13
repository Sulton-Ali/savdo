import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useDebouncedValue } from "../useDebouncedValue";

describe("useDebouncedValue", () => {
  it("returns the initial value immediately", () => {
    const { result } = renderHook(() => useDebouncedValue("a", 30));
    expect(result.current).toBe("a");
  });

  it("only updates after the value has stopped changing for delayMs", async () => {
    const { result, rerender } = renderHook(({ value }) => useDebouncedValue(value, 30), {
      initialProps: { value: "a" },
    });

    rerender({ value: "ab" });
    // Still the stale value right after the change — the debounce window
    // has not elapsed yet.
    expect(result.current).toBe("a");

    rerender({ value: "abc" });

    await waitFor(() => {
      expect(result.current).toBe("abc");
    });
  });

  it("never settles on an intermediate value when it changes faster than delayMs", async () => {
    const { result, rerender } = renderHook(({ value }) => useDebouncedValue(value, 30), {
      initialProps: { value: "a" },
    });

    rerender({ value: "ab" });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 10));
    });
    rerender({ value: "abc" });

    await waitFor(() => {
      expect(result.current).toBe("abc");
    });
  });

  it("unmounting mid-debounce clears the pending timer without error or a late update", () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    vi.useFakeTimers();
    try {
      const { result, rerender, unmount } = renderHook(
        ({ value }) => useDebouncedValue(value, 30),
        {
          initialProps: { value: "a" },
        },
      );

      rerender({ value: "ab" });
      unmount();

      // If the cleanup did not clear the timer, this would call
      // `setState` on an unmounted hook and React would log an error.
      act(() => {
        vi.advanceTimersByTime(30);
      });

      expect(result.current).toBe("a");
      expect(errorSpy).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
      errorSpy.mockRestore();
    }
  });

  it("defaults to a 300ms delay", async () => {
    const { result, rerender } = renderHook(({ value }) => useDebouncedValue(value), {
      initialProps: { value: "a" },
    });

    rerender({ value: "b" });
    expect(result.current).toBe("a");

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 100));
    });
    expect(result.current).toBe("a");

    await waitFor(
      () => {
        expect(result.current).toBe("b");
      },
      { timeout: 1000 },
    );
  });
});
