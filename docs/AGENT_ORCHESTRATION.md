# Agent orchestration and contribution workflow

Metroshell should be developed as a sequence of small PRs. Each PR has one
behavioral goal, a short description of the design choice, tests run, and any
known risk. Avoid stacking speculative refactors with user-visible work.

## Suggested work units

- Baseline tests and CI command documentation.
- Bubble Tea v2 migration for the local entry point.
- Bubble Tea v2 migration for SSH and shared app construction.
- GTFS parser and fixtures.
- Graph/BFS routing and fixtures.
- Sidebar/cursor interaction.
- Metro rendering and train simulation.
- SSH/local parity and terminal-size polish.

An agent should inspect the relevant code, tests, and vision notes before editing;
keep changes scoped; run focused checks; and report changed files and risks. Do
not edit Go source in a docs-only task. If a task discovers a product decision
that changes routing, data ownership, or local/SSH behavior, stop and ask the
orchestrator rather than silently broadening scope.

## Review checklist

- Does the PR preserve Delhi-only scope and the fewest-stops definition?
- Are local and SSH paths using the same application behavior?
- Is async work represented as Bubble Tea commands/messages rather than shared
  mutable state?
- Can the behavior be tested with small fixtures and no downloaded data?
- Are missing MBTiles/GTFS files handled without making the map unusable?
- Does the PR avoid committing large map archives, secrets, or generated output?
- Are acceptance criteria and verification commands stated in the PR body?

## AO handoff

The orchestrator should assign one narrowly bounded work unit at a time, keep
dependent PRs ordered, and send decisions back to the active worker. Workers
should report blockers early, especially module/API uncertainty, fixture
ownership, and SSH environment assumptions. Review feedback is part of the work:
address each actionable comment, rerun the relevant checks, and update the PR
summary before handoff.

