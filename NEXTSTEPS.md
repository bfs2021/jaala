# Next steps

Where each line of work left off. Work items live in GitHub issues; this file points at them.
Maintained by /checkpoint: one thread per branch, pruned when the branch merges.

## At a glance

- No open branches or PRs. `main` is v0.1.91 (#491 to #495: the re-ask, `ns.Builtin.Aliased`, the
  translator entry point, callable picks and prompt overrides).
- Mission #322 (`mission_agent_frontend`) is 31/31 on jaala's side and waits on Declaire:
  declaire#357 (the entry point), declaire#74 (callables) and a rerun of its arms on declaire#288, all
  asked on 2026-11-03. Exercise at 2179a0d: drafting 7/8 (2 first try), picking 7/8 (6 first try).
- Mission #437 (`mission_text_memory`) is active and 2/3. Next ready is #415 (P1). Its exercise was
  last logged at d56ca11; its scripted half (`TestExtract`) passed on 2026-11-03.
- Selfcheck: passed on 0e29960 (2026-11-03); 4 PRs merged since, so the next is due after one more.
- This run: dropped feat/translator-490, feat/callable-answers-484 and feat/prompt-overrides-485 (all
  merged); the v0.1.90 Declaire-build line is now declaire#357's.

## Across threads

- Declaire doesn't build on v0.1.90+ until declaire#357's step 1 lands (upgrade lines on #487).
