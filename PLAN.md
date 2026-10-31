# AutoRouter (GENE-46)

- [x] Inspect agent permissions, task dispatch and local daemon boundaries.
- [x] Select the user-requested `kev:0.8b-mlx-bf16` and verify the System One API contract.
- [x] Add a bounded local Kev decision client with deterministic HTTP tests and a runnable real-model smoke check.
- [x] Add durable daemon decision requests; enforce runtime ownership and allowed candidate IDs.
- [x] Reuse dynamic agent labels for routing levels/roles and expose selection on agent details.
- [x] Provision one stable AutoRouter entry point per workspace.
- [x] Route a new conversation once, then keep the chosen agent and existing conversation semantics; resolve direct issue assignments before dispatch (including batch updates).
- [x] Run focused/race checks, address review findings and document actual model/test limitations in `docs/autorouter.md`.
- [ ] Pass live Kev smoke and end-to-end runtime/UI validation. Exact model is installed, but Ollama 0.40.13 returns HTTP 500: `load model: layer 17 missing linear attention projections`. No alternate model or cloud fallback used.
