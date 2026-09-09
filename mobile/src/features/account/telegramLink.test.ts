import { describe, expect, it } from "vitest";

import { deriveTelegramLinkViewState } from "./telegramLink";

describe("deriveTelegramLinkViewState", () => {
  it("is loading while the status query is pending", () => {
    expect(
      deriveTelegramLinkViewState({
        statusPending: true,
        statusError: false,
        status: undefined,
        createdLinkCode: undefined,
      }),
    ).toEqual({ kind: "loading" });
  });

  it("is an error once settled with no status (query error or missing data)", () => {
    expect(
      deriveTelegramLinkViewState({
        statusPending: false,
        statusError: true,
        status: undefined,
        createdLinkCode: undefined,
      }),
    ).toEqual({ kind: "error" });

    expect(
      deriveTelegramLinkViewState({
        statusPending: false,
        statusError: false,
        status: undefined,
        createdLinkCode: undefined,
      }),
    ).toEqual({ kind: "error" });
  });

  it("is unlinked once settled, not linked, and no code has been created yet", () => {
    expect(
      deriveTelegramLinkViewState({
        statusPending: false,
        statusError: false,
        status: { linked: false, telegramUsername: null },
        createdLinkCode: undefined,
      }),
    ).toEqual({ kind: "unlinked" });
  });

  it("is linkCreated once a code was created and the status is still unlinked", () => {
    expect(
      deriveTelegramLinkViewState({
        statusPending: false,
        statusError: false,
        status: { linked: false, telegramUsername: null },
        createdLinkCode: { code: "abc123", deepLink: "https://t.me/savdo_bot?start=link_abc123" },
      }),
    ).toEqual({
      kind: "linkCreated",
      code: "abc123",
      deepLink: "https://t.me/savdo_bot?start=link_abc123",
    });
  });

  it("is linked with the username once the status reports linked", () => {
    expect(
      deriveTelegramLinkViewState({
        statusPending: false,
        statusError: false,
        status: { linked: true, telegramUsername: "alice_tg" },
        createdLinkCode: undefined,
      }),
    ).toEqual({ kind: "linked", username: "alice_tg" });
  });

  it("prefers linked over a stale createdLinkCode (a refresh after the bot confirmed the link)", () => {
    expect(
      deriveTelegramLinkViewState({
        statusPending: false,
        statusError: false,
        status: { linked: true, telegramUsername: "alice_tg" },
        createdLinkCode: { code: "abc123", deepLink: "https://t.me/savdo_bot?start=link_abc123" },
      }),
    ).toEqual({ kind: "linked", username: "alice_tg" });
  });

  it("is linked with a null username when Telegram never reported one", () => {
    expect(
      deriveTelegramLinkViewState({
        statusPending: false,
        statusError: false,
        status: { linked: true, telegramUsername: null },
        createdLinkCode: undefined,
      }),
    ).toEqual({ kind: "linked", username: null });
  });
});
