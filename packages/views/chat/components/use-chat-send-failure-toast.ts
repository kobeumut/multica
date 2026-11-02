import { useCallback } from "react";
import { dispatchReasonCode } from "@multica/core/api";
import { toast } from "sonner";
import { useT } from "../../i18n";

/**
 * Toasts why a chat send, or the session create before it, was refused. The
 * server answers a blocked dispatch with a stable `reason_code` (MUL-4525), so
 * the toast names the cause instead of a generic failure the user would only
 * retry. Anything without a known code falls back to the generic sentence.
 */
export function useChatSendFailureToast(): (err: unknown) => void {
  const { t } = useT("chat");
  return useCallback(
    (err: unknown) => {
      switch (dispatchReasonCode(err)) {
        case "invocation_not_allowed":
          toast.error(t(($) => $.input.send_blocked_toast));
          return;
        case "agent_runtime_required":
          toast.error(t(($) => $.input.runtime_required_toast));
          return;
        case "runtime_access_denied":
          toast.error(t(($) => $.input.runtime_access_denied_toast));
          return;
        case "autorouter_no_match":
          toast.error(t(($) => $.input.autorouter_no_match_toast));
          return;
        case "autorouter_unavailable":
          toast.error(t(($) => $.input.autorouter_unavailable_toast));
          return;
        default:
          toast.error(t(($) => $.input.send_failed_toast));
      }
    },
    [t],
  );
}
