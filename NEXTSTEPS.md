# Next steps

Where each line of work left off. Work items live in GitHub issues; this file points at them.
Maintained by /checkpoint: one thread per branch, pruned when the branch merges.

## At a glance

- No open branches or PRs. The last tag is v0.1.90 (chakra-backed `nlmodels`, #487); `main` adds #491
  (#483, re-asking a repeated empty draft) and #492 (#480, `ns.Builtin.Aliased`), not yet tagged.
- Mission #322 (`mission_agent_frontend`) is active and 28/31. Next ready is #484 (P1), then #485 and
  #490 (P2, the translator entry point). Exercise ran at 0e29960: nl `TestEval` 7/8; #492 merged since.
- Mission #437 (`mission_text_memory`) is active and 2/3. Next ready is #415 (P1). Its exercise was
  last logged at d56ca11, and 8 PRs have merged since.
- Selfcheck: last pass on 7079085 (v0.1.88); a run on 0e29960 was started 2026-11-03, before #492
  merged (log in the session scratchpad). `make selfcheck-status` reads its note.
- This run: dropped the old run's v0.1.88 lines; no other branches on the remote.

## Across threads

- v0.1.90 removed `frontends/nlmodels/llamacpp` and `ollama`: Declaire doesn't build on it until
  declaire#357's step 1 lands (upgrade lines on #487).
- After the next tag carrying #491, ask on declaire#288 for the translator arms' rerun (#483's second
  done-when line).
