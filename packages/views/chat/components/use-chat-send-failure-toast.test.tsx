import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { ApiError } from "@multica/core/api";
import enChat from "../../locales/en/chat.json";

const toastError = vi.hoisted(() => vi.fn());
vi.mock("sonner", () => ({ toast: { error: toastError } }));

import { useChatSendFailureToast } from "./use-chat-send-failure-toast";

function wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={{ en: { chat: enChat } }}>
      {children}
    </I18nProvider>
  );
}

// The server answers a blocked send with a stable reason_code, whatever the status.
function blocked(status: number, reason_code: string): ApiError {
  return new ApiError("blocked", status, "", { error: "blocked", reason_code });
}

describe("useChatSendFailureToast", () => {
  beforeEach(() => toastError.mockClear());

  it.each([
    ["invocation_not_allowed", 403, enChat.input.send_blocked_toast],
    ["agent_runtime_required", 409, enChat.input.runtime_required_toast],
    ["runtime_access_denied", 409, enChat.input.runtime_access_denied_toast],
    // AutoRouter answers 503, but the two causes need different next steps.
    ["autorouter_no_match", 503, enChat.input.autorouter_no_match_toast],
    ["autorouter_unavailable", 503, enChat.input.autorouter_unavailable_toast],
  ])("names the cause for %s", (reason, status, message) => {
    const { result } = renderHook(() => useChatSendFailureToast(), { wrapper });
    result.current(blocked(status, reason));
    expect(toastError).toHaveBeenCalledExactlyOnceWith(message);
  });

  it("keeps the two AutoRouter outcomes distinct", () => {
    expect(enChat.input.autorouter_no_match_toast).not.toBe(
      enChat.input.autorouter_unavailable_toast,
    );
  });

  it.each([
    ["an unknown reason code", blocked(503, "something_new")],
    ["a failure without a body", new ApiError("boom", 500, "")],
    ["a transport error", new TypeError("Failed to fetch")],
  ])("falls back to the generic toast for %s", (_label, err) => {
    const { result } = renderHook(() => useChatSendFailureToast(), { wrapper });
    result.current(err);
    expect(toastError).toHaveBeenCalledExactlyOnceWith(enChat.input.send_failed_toast);
  });
});
