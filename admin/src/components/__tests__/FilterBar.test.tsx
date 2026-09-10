import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ConfigProvider } from "antd";
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest";

import { i18next } from "../../i18n";
import { FilterBar } from "../FilterBar";

function renderBar(props: { onReset?: () => void; resultCount?: number; hasMore?: boolean }) {
  return render(
    <ConfigProvider theme={{ token: { motion: false } }}>
      <FilterBar
        onReset={props.onReset ?? vi.fn()}
        resultCount={props.resultCount ?? 0}
        hasMore={props.hasMore}
      >
        <FilterBar.Field label="Location">
          <input aria-label="Location" />
        </FilterBar.Field>
      </FilterBar>
    </ConfigProvider>,
  );
}

describe("FilterBar", () => {
  beforeAll(async () => {
    await i18next.changeLanguage("en");
  });

  afterEach(() => {
    cleanup();
  });

  it("renders each field's label above its control", () => {
    renderBar({});
    expect(screen.getByText("Location")).toBeTruthy();
    expect(screen.getByLabelText("Location")).toBeTruthy();
  });

  it("ties a render-prop field's control to the visible label via aria-labelledby", () => {
    render(
      <ConfigProvider theme={{ token: { motion: false } }}>
        <FilterBar onReset={vi.fn()} resultCount={0}>
          <FilterBar.Field label="Kind">
            {(labelId) => <input aria-labelledby={labelId} />}
          </FilterBar.Field>
        </FilterBar>
      </ConfigProvider>,
    );

    expect(screen.getByLabelText("Kind")).toBeTruthy();
  });

  it("calls onReset once when the reset button is clicked", () => {
    const onReset = vi.fn();
    renderBar({ onReset });

    fireEvent.click(screen.getByRole("button", { name: "Reset" }));

    expect(onReset).toHaveBeenCalledTimes(1);
  });

  it("shows a singular result count for exactly one result", () => {
    renderBar({ resultCount: 1 });
    expect(screen.getByText("1 result")).toBeTruthy();
  });

  it("shows a plural result count for many results", () => {
    renderBar({ resultCount: 3 });
    expect(screen.getByText("3 results")).toBeTruthy();
  });

  it("shows a plural result count for zero results", () => {
    renderBar({ resultCount: 0 });
    expect(screen.getByText("0 results")).toBeTruthy();
  });

  it("appends 'more available' when hasMore is set", () => {
    renderBar({ resultCount: 50, hasMore: true });
    expect(screen.getByText(/50 results/)).toBeTruthy();
    expect(screen.getByText(/more available/)).toBeTruthy();
  });

  it("does not append 'more available' when hasMore is unset", () => {
    renderBar({ resultCount: 50 });
    expect(screen.queryByText(/more available/)).toBeNull();
  });
});
