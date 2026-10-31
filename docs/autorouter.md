# Local AutoRouter

AutoRouter is a workspace entry point, not another coding agent. It uses exactly
`kev:0.8b-mlx-bf16` on the selected runtime's machine; there is no cloud fallback.
Deploy the server, web client and daemon together before using it (migration 595).

## Setup

1. On an Apple Silicon runtime, install Ollama **0.40.4 or later**, start Ollama,
   and run `ollama pull kev:0.8b-mlx-bf16`.
2. Upgrade/restart the Multica daemon. Its heartbeat must advertise pending-work
   support. An old daemon cannot answer routing requests.
3. In **Agents → Add AutoRouter**, choose that runtime. The button provisions one
   built-in entry per workspace; it does not install the model or start Ollama.
4. In **Settings → Labels → Agent**, edit the seeded Entry-level, Normal and
   Advanced categories, or add roles such as Project Manager or Mobile Developer.
   Add descriptions, then attach categories in each target agent's inspector.
   Existing agent labels remain usable. Creation retries do not reset categories.
5. Open a new chat with AutoRouter, or explicitly assign/run an issue with it.
   The chosen agent must already be configured, available and invokable by you.

Ollama's decision API is `http://127.0.0.1:11434/v1/systemone` **on the runtime**,
not on the hosted server. Proxy environment variables and HTTP redirects are not
used. Only the request text and eligible agents' names, descriptions and categories
are sent to that device. Choose a trusted runtime: its owner can inspect that data.
The cloud server temporarily stores the same routing input until completion or
expiry cleanup. This is local inference, not an entirely offline Multica service.

## Dispatch and failure behavior

- New chats route on their first non-empty message. The session's agent changes
  to the selected agent and a normal task is queued; later messages stay with it.
  There is no synthetic AutoRouter introduction or mid-conversation rerouting.
  The existing chat update event refreshes desktop/web and mobile session state.
- Explicit issue creation, single/batch update, assignment and run resolve AutoRouter before
  normal dispatch, using the issue title and description. The selected agent
  becomes the assignee. Internal automations cannot execute AutoRouter as a
  general-purpose agent; they must select a concrete agent.
- Eligibility honors workspace scope, invocation permissions and runtime
  readiness. Permissions and readiness are checked again after inference.
  AutoRouter cannot select itself, a made-up ID or an excluded agent.
- Input is limited to 64,000 bytes and 254 eligible agents, leaving Kev's last
  choice for “no match.” A model error, timeout or no-match fails closed: nothing
  is silently sent to another model/agent. Select an agent explicitly to proceed.
- Server requests expire after 40 seconds; local inference has a 30-second budget.
  Decisions are persisted for multi-instance delivery, claimed on the router's
  runtime only, re-delivered after a short lease and accepted only once.
  The daemon deduplicates active inference. Failed reports may repeat a decision
  on redelivery; decisions have no task-execution side effects.

## Checks

```sh
cd server
go test ./internal/autorouter
# Disposable PostgreSQL migrated to HEAD is required for handler/service tests:
DATABASE_URL='postgres://…' go test ./internal/handler ./internal/service -run AutoRouter -count=1
# Real model: opt-in, not a mock. Includes English complexity and Turkish roles.
MULTICA_AUTOROUTER_SMOKE=1 go test ./internal/autorouter -run TestKevLocalSmoke -count=1 -v
```

Unit tests cover HTTP errors, malformed decisions, timeouts, bounds, no-match and
allowlisted selection. Database tests cover idempotent provisioning, editable
categories, identity-only retry responses, runtime isolation, lease recovery,
fresh-request priority, duplicate results, transactional first-message handoff,
rollback/concurrent-send conflicts, single/batch existing-issue text and permission
revocation during inference. Daemon tests cover coalesced-request draining.
Realtime cache tests cover the agent handoff. A passing mock is not a model-quality
result.

Focused Go regression tests also pass with `-race`; all Go packages compile.
Core/views typechecks, 49 realtime cache tests and lint of the changed view
components pass. The full `go test ./...` attempt is **not green**: CLI tests
encounter the inherited daemon-task marker/token guard, and the run hit its
five-minute command limit. The marker was not removed or bypassed. Focused
handler/service tests use a disposable PostgreSQL database migrated to HEAD.

### Actual local result (2026-10-31)

The exact model is downloaded (1.5 GB) and advertises the `decision` capability.
With Ollama **0.40.13**, the opt-in smoke fails at model loading, before selection:

```text
HTTP 500
load model: layer 17 missing linear attention projections
```

A direct System One request reproduces it. Routing is therefore **not validated
against live Kev** on this machine. The application reports failure and does not
fall back. An Ollama/model loader correction and a rerun of the real smoke are
required before enabling this entry point for users. No production database,
Ollama installation or running Multica daemon was upgraded by this change.
Turkish examples are a smoke check, not a measured multilingual accuracy claim.
No UI browser/device end-to-end test or deployment was performed.

References: [Kev model](https://ollama.com/library/kev:0.8b-mlx-bf16),
[Ollama decision API](https://docs.ollama.com/capabilities/decision).
