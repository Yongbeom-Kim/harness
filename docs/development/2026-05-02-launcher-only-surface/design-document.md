# Launcher-Only Operator Surface for the Harness

**Goal:** Remove `implement-with-reviewer` and all other non-launcher operator binaries so the harness product surface consists only of `tmux_codex` and `tmux_claude`.

**Status:** Reviewed design input complete; ready for implementation planning.

## Summary

The current repo still carries a larger operator-facing story than the product now wants to support. Even after the concrete-agent refactor, the build, contracts, docs, and some skills still treat `implement-with-reviewer` as an active command, and the repo still contains non-launcher remnants that imply a broader surface than the product wants to keep.

This design intentionally narrows the product. The harness no longer presents itself as a multi-agent orchestration tool with a built-in review loop. Its supported operator surface is two persistent tmux-backed launcher commands:

- `tmux_codex`
- `tmux_claude`

Everything else is either removed from the active product or retained only as historical documentation. There is no replacement workflow binary in this design.

## Current State

- The `Makefile` still builds `bin/implement-with-reviewer` in addition to `bin/tmux_codex` and `bin/tmux_claude`.
- The repo still contains `orchestrator/cmd/implement-with-reviewer` and review-loop-specific runtime code, tests, contracts, and artifact-writing behavior.
- The repo history and active docs describe `implement-with-reviewer` as the first concrete end-to-end workflow.
- The repo still contains at least one orphan non-launcher entrypoint location, `orchestrator/cmd/tmux_agent/`, even though the desired product surface is only the two launcher binaries.
- At least one active skill, `skills/implementation-design/SKILL.md`, still ends by telling the operator to pipe the implementation spec into `implement-with-reviewer`.

The product direction for this design overrides that state.

## Goals

- Narrow the supported operator-facing binary surface to exactly `tmux_codex` and `tmux_claude`.
- Remove `implement-with-reviewer` as an active product command rather than deprecating it.
- Preserve the existing behavior and flag contracts of `tmux_codex` and `tmux_claude`.
- Remove review-loop-specific runtime code, runtime artifacts, and contracts from the active product.
- Rewrite active product docs so the harness is described as a launcher-only tmux-backed tool.
- Update skills and active docs that currently assume `implement-with-reviewer` so they no longer point to a removed command.
- Keep older review-loop design docs/specs as historical records while making this launcher-only design the active direction.

## Non-Goals

- No replacement orchestration binary.
- No new productized manual two-agent flow.
- No redesign of launcher flags, session naming defaults, attach behavior, or success/error semantics for `tmux_codex` and `tmux_claude`.
- No new generic artifact directory to replace `log/runs/`.
- No attempt to preserve backward compatibility for scripts that still invoke `implement-with-reviewer`.
- No rewrite of historical design documents to erase the repo's previous direction.
- No new launcher contract files, wrapper aliases, or symmetry work added only to make `tmux_codex` and `tmux_claude` look more uniform.

## Planning Boundaries

This design should produce one bounded implementation plan, not a repo-wide cleanup campaign. Use these classification rules during planning:

- Active product docs are files that currently instruct an operator how to use the harness now. In this repo, that includes maintained command contracts under live command directories, top-level usage/build docs, current skill instructions under `skills/`, and this launcher-only design.
- Historical docs are dated design and implementation records kept for history, especially older workflow documents under `docs/development/` that describe `implement-with-reviewer`.
- Historical preservation applies to dated docs under `docs/development/` and to existing on-disk artifacts such as old `log/runs/` directories. It does not require preserving source-local contract files, tests, or command packages that belong to removed binaries.
- If a file or directory exists only to support a removed non-launcher entrypoint and is not itself a historical design artifact, it should be deleted rather than relabeled as inactive.

## Product Surface

### Supported operator-facing binaries

The supported operator-facing binaries are exactly:

- `tmux_codex`
- `tmux_claude`

This is both a documentation statement and a build/entrypoint statement. The supported surface must not include non-launcher commands such as `implement-with-reviewer` or `tmux_agent`, and it does not promise helper aliases or wrapper binaries beyond the two launcher names above.

If an entrypoint, wrapper, or generated binary is not one of the two launcher commands above, it must not remain part of the supported product surface.

### Launcher contract

`tmux_codex` and `tmux_claude` keep their current launcher role:

- create a tmux session
- launch the corresponding backend in that session
- wait until the backend is ready for input
- optionally attach the operator to the tmux session

This design does not change their flag shapes, default session names, attach semantics, or success/failure contract. The simplification is about scope, not launcher behavior.

Preserving the launcher contract means preserving the current observable behavior of the existing `tmux_codex` and `tmux_claude` binaries. It does not require adding new launcher features, introducing a new generic launcher abstraction, or creating missing documentation artifacts solely to make the two launchers look symmetrical.

## Removed Surface

### `implement-with-reviewer`

`implement-with-reviewer` is removed completely as an active product surface:

- remove the command from the build
- delete the command package and its colocated contract/test files rather than leaving them in the source tree as inactive history
- remove its review-loop-specific runtime and tests rather than leaving them dormant
- remove its artifact contract, including `log/runs/<run-id>/` as an active product guarantee

This is an intentional breaking simplification. There is no hidden mode, compatibility shim, or deprecated-but-still-shipped binary.

### Review-loop-specific runtime

Runtime code exists to serve the product surface. Once the review-loop product is removed, code whose purpose is to support that workflow should also be removed instead of retained "just in case."

The launcher-only core should keep only code required by the two remaining launchers. That includes the concrete launcher-facing runtime and infrastructure they actually use, such as:

- `orchestrator/cmd/tmux_codex`
- `orchestrator/cmd/tmux_claude`
- concrete agent launch/runtime code under `orchestrator/internal/agent`
- tmux integration under `orchestrator/internal/agent/tmux`
- launcher-supporting utilities still needed by the two commands, such as shell launch helpers and directory locking

Packages, tests, and contract files that exist only to support the removed review loop should be deleted rather than preserved as speculative infrastructure.

If code is still referenced by `tmux_codex` or `tmux_claude`, it stays even if it was introduced during earlier workflow work. If a package, helper, or empty directory is reachable only from removed non-launcher entrypoints, it should be removed.

### Build outputs and non-launcher entrypoints

The concrete build requirement is:

- `make build` produces `bin/tmux_codex`
- `make build` produces `bin/tmux_claude`
- `make build` no longer produces `bin/implement-with-reviewer`

Beyond that, the implementation should remove non-launcher entrypoint remnants that would otherwise imply a broader supported surface. That includes deleting orphan command directories such as `orchestrator/cmd/tmux_agent/` if they remain.

This design does not require chasing or standardizing nonexistent helper wrappers. If extra wrapper scripts or generated binaries are present in the working tree at implementation time, they must not remain built, shipped, or documented as supported operator entrypoints.

## Documentation Model

### Active docs

Active docs should describe the harness as a tmux-backed launcher tool, not as a built-in multi-agent orchestrator.

For planning purposes, active docs are the files that define current product usage, including:

- maintained contract docs under live command directories
- current skill instructions under `skills/`
- top-level or product-facing usage/build docs
- this launcher-only design document

That means:

- stop calling `implement-with-reviewer` the first concrete workflow
- stop describing `log/runs/` as part of the current product contract
- stop presenting multi-agent orchestration as a built-in capability of the core harness
- present higher-level workflows, if mentioned at all, as future work or as something external to the core product surface

### Historical docs

Older design docs and implementation specs for `implement-with-reviewer` remain in the repo as historical records. They should not be rewritten to hide what the product used to be.

However, they also should not continue to read as the active direction. The new launcher-only design supersedes them.

Historical docs are primarily dated records under `docs/development/`. The preservation rule for those files does not require keeping source-local contracts or entrypoints inside removed command directories.

## Skills and Workflow Docs

Skills and docs that currently end by instructing the operator to run `implement-with-reviewer` must be updated in the same product transition.

The update obligation applies to current instruction-bearing files under `skills/` and other active workflow docs. It does not require rewriting archived prompts or historical design artifacts that are kept only for record.

The replacement rule is conservative:

- remove the obsolete execution command
- stop at the produced artifact path or document neutral manual next steps
- do not invent a new orchestration command as part of this design
- do not silently substitute a launcher command as though it were equivalent to the removed workflow

This matters especially for `skills/implementation-design/SKILL.md`, which currently ends with a command that will no longer exist after this product change.

## Artifact Model

`log/runs/` is not part of the launcher-only product contract.

After `implement-with-reviewer` is removed:

- existing historical `log/runs/` directories on disk may remain untouched
- older docs may still mention `log/runs/` in historical contexts
- the launcher-only V1 product does not redefine `log/runs/` for the remaining binaries

In other words, `log/runs/` is historical workflow baggage from the removed command, not an active requirement for the narrowed product.

## Compatibility and Migration

This change intentionally breaks any workflow that still depends on `implement-with-reviewer`.

The migration stance is:

- no deprecation window
- no compatibility alias
- no unsupported hidden binary left behind
- no doc claim that the old command still exists

The only supported operator migration path is to adopt the launcher-only model and use `tmux_codex` / `tmux_claude` directly.

## Resulting Product Statement

After this design is implemented, the harness should be understandable in one sentence:

> The harness is a small tmux-backed launcher tool that exposes persistent Codex and Claude sessions through `tmux_codex` and `tmux_claude`.

That statement should be true across:

- build outputs
- command packages
- contract docs
- active design docs
- skill output instructions

## Edge Cases

### Historical runtime artifacts

Existing review-loop artifacts under `log/runs/` are preserved as historical files already on disk. The design does not require deleting old run directories just because the product contract no longer includes them.

### Historical documentation references

Historical design docs may continue to describe the removed workflow, but active docs and contracts must not rely on those documents as the current product definition.

### Users with old scripts

Scripts or habits that still invoke `implement-with-reviewer` will fail after the transition. That is acceptable in this design because the product decision is to remove the command, not to soften the change with compatibility plumbing.

### Future orchestration work

A future design may introduce a new orchestration surface, but this design does not reserve a binary name, artifact model, or compatibility story for it. Future workflow productization should start from a fresh design instead of preserving `implement-with-reviewer` scaffolding now.

## Acceptance Criteria

This design is satisfied when all of the following are true:

- the supported binary surface and active product docs promise exactly `tmux_codex` and `tmux_claude`
- `make build` produces only `bin/tmux_codex` and `bin/tmux_claude`
- `implement-with-reviewer` is removed from active build outputs, source surface, and active product docs
- review-loop-specific runtime/artifact contracts are no longer part of the active product
- active docs and current skill instructions describe the harness as launcher-only according to the classification rules above
- active skills/docs that pointed to `implement-with-reviewer` stop doing so
- historical review-loop docs remain available as history, but no longer define the active direction
- orphan non-launcher entrypoints do not remain in source or docs in a way that implies they are supported binaries
