# Treehouse Pool Orphan Sweep - 2026-08-30

Crewmate task `treehouse-hygiene-20260830`: sweep and repair ALL treehouse pool
orphan collisions across `/home/jon/.treehouse/`.

## Method

1. Enumerated every pool dir `/home/jon/.treehouse/<repo>-<hash>/` (168 pools
   with a `treehouse-state.json`), every slot worktree `<slot>/<repo>` inside
   them (walked all slot dirs recursively), plus all nested in-project pools
   under `<pool>/.treehouse/<poolName>/` (7 found, all empty or fully managed).
2. Compared on-disk worktree directories against the authoritative lease list
   in each pool's `treehouse-state.json` (the same file `treehouse status`
   reads). A directory on disk NOT listed in state = orphan (untracked by
   treehouse = `treehouse get` collision source, `prune`/`return` refuse it).
3. Cross-checked `treehouse status` twice per affected pool, verified state
   files twice, scanned `/proc/*/cwd` and open-file handles, and re-ran the
   full enumeration twice - including after any action - to catch races.
   Nothing under an actively-leased home was touched.
4. Disposition rules: valid git repo with UNCOMMITTED changes -> tar to
   `/home/jon/.treehouse/orphan-archive-20260830/` (mode 0600); clean or not a
   valid git repo (partial clone) -> remove. Both cases recorded.

## Orphans found (1)

### REMOVED - `/home/jon/.treehouse/agents-flow-338c12/1/agents-flow-gate`

- Valid git worktree (`.git` -> `gitdir: .../firstmate-66c516/2/firstmate/
  projects/agents-flow/.git/worktrees/agents-flow-gate`), but working tree was
  CLEAN (zero staged/unstaged/untracked changes, verified twice with
  `git status --porcelain=v2 --untracked-files=all`).
- On branch `fm/gate_rollout_agents_flow` @ `913b33d` (ci: adopt canonical
  hardened no-mistakes-required gate). The branch ref and all commits live in
  the shared gitdir, so removal loses no data.
- Not present (then or ever) in any pool's `treehouse-state.json`; no pool
  anywhere references the path; `treehouse status` for the pool never lists it.
- No process had it as cwd (two scans) and no open file handles.
- Disposition per rule 3: CLEAN -> REMOVED (`rm -rf`, verified gone).

## Dispositions by category

- REMOVED: 1 (`agents-flow-338c12/1/agents-flow-gate`)
- ARCHIVED: 0 (no orphan carried uncommitted work; archive dir
  `/home/jon/.treehouse/orphan-archive-20260830/` not created because nothing
  was archived)
- Preserved: all 5 managed slots of `agents-flow-338c12` (1,2,3 in-use /
  you're here; 5,6 dirty), everything in every other pool, all nested pools
  (including `clean-second-brain-pool/.treehouse/second-brain-task-base-b431da/
  1/second-brain-task-base`, which is managed), and all live leased homes
  (e.g. `firstmate-66c516/*`, `firstmate-7bab20/2`).

## Pools now clean (all)

All 168 top-level pools + 7 nested in-project pools are now orphan-free,
including the two that wedged tonight:

- `monitoring-78da78` - 9 managed slots, 0 orphans
- `second-brain-b431da` - 2 managed slots, 0 orphans
- `agents-flow-338c12` - 5 managed slots, 0 orphans (this sweep's fix)
- Every other pool verified 0 orphans by full re-sweep after the removal.

## Notes / known benign remnants

- The stale `git worktree` registration for the removed gate dir remains in
  the live leased home `firstmate-66c516/2/firstmate/projects/agents-flow/.git/
  worktrees/agents-flow-gate`. Per the brief, nothing under an actively-leased
  home may be modified; the registration is benign and self-heals on the next
  `git worktree` operation (treehouse's acquire path prunes stale
  registrations). Left untouched by design.

## Verification

- Triple full-pool sweep (before action, pre-removal, post-removal):
  0 orphans after this sweep.
- `treehouse status` for `agents-flow-338c12` after removal: only the 5
  managed slots, all preserved.

## Project memory

Skipped AGENTS.md edits deliberately: this task produced durable knowledge
about the machine's treehouse pool machinery, not about the beads project
(beads only provided the disposable delivery worktree). No beads knowledge to
record.