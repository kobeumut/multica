# AutoRouter (GENE-46)

- [x] Inspect agent permissions, task dispatch and local daemon boundaries.
- [x] Add a bounded local decision client with deterministic HTTP tests and a runnable real-model smoke check.
- [x] Add durable daemon decision requests; enforce runtime ownership and allowed candidate IDs.
- [x] Reuse dynamic agent labels for routing levels/roles and expose selection on agent details.
- [x] Provision one stable AutoRouter entry point per workspace.
- [x] Route a new conversation once, then keep the chosen agent and existing conversation semantics; resolve direct issue assignments before dispatch (including batch updates).
- [x] Run focused/race checks, address review findings and document actual model/test limitations in `docs/autorouter.md`.
- [x] Switch the pinned model to the user-requested `d1-omni:600m`; verify its System One contract and actual local inference.
- [x] Update string-based model criteria, short-option/UUID mapping, regression tests, provisioning/setup copy and all six locales; preserve permissions without fallback.
- [x] Run guarded decision/daemon race tests, build all Go packages and run real UUID-based English/Turkish/no-match smoke; record results in `docs/autorouter.md`.
- [ ] Resolve Entry-level/Advanced false no-matches (realistic UUID/full-category smoke 4/6, deliberately left failing).
- [ ] Fix managed worktree PostgreSQL authentication (`SQLSTATE 28P01`) and rerun handler/service integration tests; do not reset shared credentials.
- [ ] Restore frontend dependencies and rerun lint/typechecks.
- [ ] Complete runtime/UI end-to-end validation before deployment.
