import { expect, it } from "vitest";
import { currentChatAgent } from "./current-chat-agent";

it("uses the handed-off session agent even when composition selection is stale", () => {
  const router = { id: "router" };
  const worker = { id: "worker" };
  // Full list lookup preserves the bound agent if invocation access is revoked.
  expect(
    currentChatAgent({ agent_id: worker.id }, router.id, [router, worker], [router]),
  ).toBe(worker);
  expect(
    currentChatAgent({ agent_id: "missing" }, router.id, [router], [router]),
  ).toBeNull();
});

it("uses explicit selection or the first available agent only for a new chat", () => {
  const first = { id: "first" };
  const selected = { id: "selected" };
  expect(
    currentChatAgent(null, selected.id, [first, selected], [first, selected]),
  ).toBe(selected);
  expect(currentChatAgent(null, null, [first], [first])).toBe(first);
  expect(currentChatAgent(null, "unavailable", [first], [first])).toBeNull();
  expect(currentChatAgent(null, null, [], [])).toBeNull();
});
