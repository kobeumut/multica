# Local AutoRouter

AutoRouter is a workspace entry point, not another coding agent. It uses exactly
`d1-omni:600m` on the selected runtime's machine; there is no cloud fallback.
Deploy the server, web client and daemon together before using it (migration 595).

## Setup

1. On the selected runtime, start an Ollama version supporting the decision API
   and run `ollama pull d1-omni:600m`. Local requests were tested on Ollama 0.40.13.
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
- Input is limited to 64,000 bytes and 254 eligible agents, plus a reserved
  “no match” option. D1 receives string descriptions under short option labels;
  those labels map back to allowed agent IDs, never directly to arbitrary IDs.
  A model error, timeout or no-match fails closed: nothing
  is silently sent to another model/agent. Select an agent explicitly to proceed.
- Server requests expire after 40 seconds; local inference has a 30-second budget.
  Decisions are persisted for multi-instance delivery, claimed on the router's
  runtime only, re-delivered after a short lease and accepted only once.
  The daemon deduplicates active inference. Failed reports may repeat a decision
  on redelivery; decisions have no task-execution side effects.

## Checks

```sh
cd server
bash ../scripts/go-test-with-agent-cli-guard.sh -- \
  go test -race ./internal/autorouter ./internal/daemon -run 'AutoRouter|Decision' -count=1
# Real model: opt-in, not a mock. UUID candidates, English complexity, Turkish roles,
# and an unrelated request. Remains failing if an expected route is not selected.
MULTICA_AUTOROUTER_SMOKE=1 go test ./internal/autorouter -run TestLocalDecisionSmoke -count=1 -v
```

Handler/service tests require the managed worktree PostgreSQL database migrated to
HEAD. Follow `CONTRIBUTING.md`: generate `.env.worktree` with `make worktree-env`
only if missing, then `make migrate-up ENV_FILE=.env.worktree`. Export that file's
environment before guarded handler/service tests (`-run AutoRouter`). Do not copy
`.env`, manually create a database or reset shared credentials. Check verbose
output: unreachable PostgreSQL can make these integration tests skip.

Unit tests cover HTTP errors, malformed decisions, timeouts, bounds, no-match and
allowlisted selection. Database tests cover idempotent provisioning, editable
categories, identity-only retry responses, runtime isolation, lease recovery,
fresh-request priority, duplicate results, transactional first-message handoff,
rollback/concurrent-send conflicts, single/batch existing-issue text and permission
revocation during inference. Daemon tests cover coalesced-request draining.
Realtime cache tests cover the agent handoff. A passing mock is not a model-quality
result.

### Actual D1 validation (2026-11-02)

- Guarded decision-client and focused daemon tests pass with `-race`;
  `go build ./...` passes. All six updated locale JSON files parse.
- The downloaded D1 model loads and answers real System One requests on Ollama
  **0.40.13**. Final UUID-candidate smoke with described categories as sent by
  the handler: **4/6 pass** (Normal, Turkish Mobile Developer, Turkish Project
  Manager, unrelated/no-match).
- The README typo and complex distributed-transaction requests incorrectly
  select **no match** instead of Entry-level and Advanced. The opt-in smoke
  therefore **fails**, not passes. Final observed calls were 88–181 ms after
  the first 852 ms request; these are local observations, not a benchmark or
  multilingual-accuracy guarantee. An earlier bare-category smoke passed 5/6;
  it did not represent the handler's full category metadata.
- Handler/service regression rerun is blocked: managed `make migrate-up
  ENV_FILE=.env.worktree` fails PostgreSQL authentication (`SQLSTATE 28P01`).
  Shared credentials were not reset and no substitute database was created.
- Frontend lint/typechecks were not rerun: dependencies are absent (`eslint`
  not found). Prior-commit DB/race tests, core/views typechecks and 49 realtime
  tests passed, but those are not a fresh result for this revision. The earlier
  full Go-suite attempt also hit the task-token guard and a five-minute limit.

UUIDs as model option names produced additional false no-matches. The adapter
uses short options mapped back to the original allowed UUIDs; it never relaxes
permission checks or retries with another model. Concrete agent/category
expertise descriptions matter. Live inference now works, but the unresolved
Entry-level/Advanced misses and incomplete integration/UI validation keep this
change draft.
No UI browser/device end-to-end test, merge or deployment was performed.

References: [D1 model](https://ollama.com/library/d1-omni:600m),
[Ollama decision API](https://docs.ollama.com/capabilities/decision).
