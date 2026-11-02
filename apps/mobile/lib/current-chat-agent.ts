// Existing sessions remain authoritative after a handoff or permission change.
export function currentChatAgent<T extends { id: string }>(
  session: { agent_id: string } | null,
  selectedId: string | null,
  agents: readonly T[],
  available: readonly T[],
): T | null {
  if (session) return agents.find((agent) => agent.id === session.agent_id) ?? null;
  if (selectedId) return available.find((agent) => agent.id === selectedId) ?? null;
  return available[0] ?? null;
}
