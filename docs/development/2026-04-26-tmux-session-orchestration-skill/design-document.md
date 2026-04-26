# Tmux-Backed Persistent CLI Sessions for `implement-with-reviewer`

**Goal:** Replace one-shot backend CLI invocations with tmux-backed persistent `codex` and `claude` sessions while keeping the `implement-with-reviewer` CLI and output contract materially unchanged.

**Status:** Reviewed design input complete; ready for implementation planning.

## Summary

The current `implement-with-reviewer` command runs an implementer/reviewer loop by spawning a fresh backend CLI process for every prompt. `codex` uses `exec` plus extracted session IDs for `resume`, and `claude` uses `-p` plus generated session IDs for `--resume`. This works, but it has three limitations the new design is intended to remove:

- sessions do not stay live in a terminal that can be observed directly
- mid-run progress is only available once a subprocess exits
- backend interaction remains effectively serial and subprocess-oriented rather than session-oriented

This design replaces the current one-shot adapter model with persistent tmux-backed adapter sessions. Each run creates one tmux session with two panes, one for the implementer and one for the reviewer. The adapters own tmux interaction details for their respective backends. The orchestration command still owns the run loop, approval logic, iteration control, and stdout/stderr transcript behavior.

The first version is intentionally narrow:

- keep the existing `implement-with-reviewer` CLI surface
- support only `codex` and `claude`
- keep human intervention out of scope
- use a simple `<promise>done</promise>` turn-completion contract
- persist raw runtime artifacts under `log/runs/<run-id>/`

## Goals

- Keep `implement-with-reviewer` as the primary user-facing command and preserve its current flags, validation rules, exit codes, transcript banners, and overall stdout/stderr shape.
- Replace backend-native resume/session-ID handling inside the adapters with tmux-backed persistent sessions.
- Start both implementer and reviewer panes at run start.
- Use one tmux session per run and one pane per role.
- Let `codex` and `claude` adapters own backend launch details and tmux interaction through adapter methods rather than through a separate shared tmux subsystem.
- Persist structured run metadata, state transitions, and raw pane captures after each completed turn and again on timeout/finalization.
- Detect turn completion by requiring each turn to end with `<promise>done</promise>`.
- Use a fixed per-turn idle timeout of 120 seconds for both roles.
- Fail fast on tmux unavailability, backend launch failure, timeout, or artifact persistence failure.

## Non-Goals

- No redesign of the external `implement-with-reviewer` UX.
- No new tmux-specific CLI flags in v1.
- No fallback to the current one-shot `exec` / `resume` / `-p` model when tmux mode fails.
- No generic multi-backend session engine beyond the concrete `codex` and `claude` adapters.
- No human pause/resume, clarification-handshake protocol, or blocked-state modeling.
- No configurable timeout in v1.
- No stripping of `<promise>done</promise>` from semantic outputs in v1.
- No requirement that the design center unit-test seams; v1 testing is integration-led.

## Current State

The current command behavior is defined by `orchestrator/cmd/implement-with-reviewer/main.go` and its contract document.

- `implement-with-reviewer` reads a task from `stdin`
- it instantiates two backend tools, one implementer and one reviewer
- each call to `SendMessage` spawns a backend subprocess and waits for full completion
- approval is detected by substring match on `<promise>APPROVED</promise>`
- non-approval feedback is fed back into the implementer as a rewrite prompt

The backend adapters in `orchestrator/internal/cli/` are currently subprocess wrappers:

- `codex.go` manages `exec` vs `resume` and extracts a session ID from stderr metadata
- `claude.go` manages `-p` vs `--resume` and stores a generated session ID

The new design changes that adapter contract fundamentally: the adapters become persistent session controllers instead of per-prompt subprocess wrappers.

## User-Facing Contract

### Command-line contract

The command invocation remains:

```sh
cat task.txt | implement-with-reviewer --implementer <backend> --reviewer <backend> [--max-iterations N]
```

Supported backends remain:

- `codex`
- `claude`

Validation rules remain unchanged.

Exit codes remain unchanged:

- `0` for success
- `1` for runtime failure or non-convergence
- `2` for usage/validation failure

### Stdout/stderr contract

The command keeps the existing transcript structure:

- run header lines
- per-invocation banners such as `--- iter N - ROLE (backend) ---`
- forwarded role turn text to command stdout
- orchestrator/runtime errors to command stderr
- approval/non-convergence/failure reporting in the existing command shape

V1 intentionally does **not** clean or normalize backend turn text. This means `<promise>done</promise>` remains present in:

- captured pane files
- turn results reused across the loop
- final printed implementation output

This is a deliberate simplicity tradeoff for v1.

The command does **not** continuously stream pane bytes to parent stdout in v1. Instead, it forwards each role’s raw turn result after the adapter has observed `<promise>done</promise>` and captured that turn. Live progress remains available through tmux inspection and runtime artifacts rather than through continuous parent-stdout mirroring.

Because tmux pane capture does not preserve the child process `stdout`/`stderr` split, the parent command no longer attempts to reconstruct backend-native stream separation inside successful turns. All captured role turn text is printed on parent `stdout` under the existing iteration banners. Parent `stderr` remains reserved for command/runtime failures and validation errors.

### Observability contract

The CLI does not add a new stdout header for run ID or tmux session name. Runtime observability is provided by:

- live tmux panes that can be inspected out-of-band
- raw persisted captures under `log/runs/<run-id>/`
- structured run metadata and state transitions on disk

## Runtime Model

### Run identity

Each command invocation creates a UUIDv7 run ID.

That run ID is used for:

- the runtime artifact directory: `log/runs/<run-id>/`
- the tmux session name derived from the run ID, for example `iwr-<run-id>`

### Tmux topology

Each run creates exactly one tmux session.

That session contains exactly two panes:

- `implementer`
- `reviewer`

Both panes are started at run initialization time, before the first implementation turn begins.

The orchestrator owns run-level topology:

- one run
- one tmux session
- two panes
- one adapter instance per role
- pane naming and target assignment for `implementer` and `reviewer`

The ownership split is intentionally concrete:

- the orchestrator creates the tmux session once, derives stable pane targets for both roles, and passes those targets into the adapters
- each adapter launches its backend process inside its assigned pane and owns all later pane-local interaction
- adapters do not create extra sessions, windows, or panes beyond the single orchestrator-provided pane for their role

The backend adapters own backend-specific tmux operations:

- how to launch the interactive CLI in the pane
- how to paste multi-line prompts
- how to wait for turn completion
- how to capture pane output
- how to clear/reset the pane between turns
- how to close the pane-local backend process cleanly

There is no separate generic tmux manager in v1.

## Session and Adapter Contract

The current single-call `CliTool` abstraction is replaced by a session-oriented adapter API. The exact Go type name is an implementation detail, but the behavior is not.

The adapter contract must support these responsibilities:

- `Start`
  - attach to the orchestrator-provided tmux pane target for that role
  - launch the backend interactive CLI in that pane
  - send the backend’s stable startup prompt
  - wait for startup completion using the same per-turn completion rules as any other turn
- `Send`
  - paste the current turn prompt into the pane verbatim
  - submit it to the interactive backend
- `WaitForDone`
  - block until the adapter observes that the newly captured turn output ends with `<promise>done</promise>`
  - use idle-time detection while polling/capturing pane output for that turn only
- `Capture`
  - return the raw current pane text exactly as tmux captures it for the current turn surface
- `Close`
  - terminate the adapter-owned interactive backend process in its pane without creating or destroying shared run topology

The adapter contract is sessionful and role-specific. The implementer and reviewer adapters each maintain a live interactive conversation in their pane for the duration of the run.

## Prompt Model

### Startup prompt

Each pane receives one startup prompt when the session starts.

The startup prompt contains only stable role rules.

Implementer startup prompt content must preserve the current role behavior:

- act as a software implementer
- answer with code/output only, consistent with the current command’s implementer expectations

Reviewer startup prompt content must preserve the current review role behavior:

- act as a strict code reviewer
- use `<promise>APPROVED</promise>` as the approval marker when approved
- otherwise return actionable feedback

The startup prompt must also instruct the backend to acknowledge initialization and end that acknowledgement with `<promise>done</promise>`.

There is no special startup timeout. Startup completion uses the same 120-second idle timeout and done-marker detection as any other turn.

The startup acknowledgement is not treated as a normal user-visible turn. On successful initialization, the adapter clears/resets the pane before the first real turn begins. Startup output does not need a normal per-turn capture file unless startup fails and the command is preserving best-effort failure artifacts.

### Per-turn prompts

Each turn prompt contains:

- the turn-specific task/rewrite/review content
- a suffix instructing the backend to end the response with exactly `<promise>done</promise>`

This suffix is repeated on every turn, even though the pane already received a startup prompt.

The prompt suffix is part of the submitted prompt text, so adapters must not treat echoed prompt text as completion. Done-marker detection only considers pane output produced after the adapter submits the turn and after the pane has been reset for that turn.

### Approval contract

Reviewer approval remains substring-based at the orchestration layer. That is required because reviewer output now also includes `<promise>done</promise>`.

Approved reviewer output therefore has the effective shape:

```text
<promise>APPROVED</promise>
<promise>done</promise>
```

or any equivalent response containing the approval marker and ending with the done marker.

This preserves current approval detection while making reviewer turns compatible with the tmux completion model.

## Turn Execution Model

### Implementer turn

For the initial implementer pass:

1. orchestrator asks the implementer adapter to send the initial task turn
2. adapter waits for `<promise>done</promise>`
3. adapter captures raw pane text
4. capture is persisted to disk
5. pane is cleared/reset for the next turn
6. raw captured turn result is returned to the orchestrator

### Reviewer turn

For each review iteration:

1. orchestrator builds the reviewer turn prompt from the original task and current implementation
2. reviewer adapter sends it
3. reviewer adapter waits for `<promise>done</promise>`
4. reviewer capture is persisted
5. pane is cleared/reset
6. raw captured reviewer output is returned
7. orchestrator checks whether the output contains `<promise>APPROVED</promise>`

### Rewrite turn

If the reviewer does not approve:

1. orchestrator builds the rewrite prompt from the original task, prior implementation, and reviewer feedback
2. implementer adapter sends it into the existing implementer pane
3. adapter waits for `<promise>done</promise>`
4. capture is persisted
5. pane is cleared/reset
6. raw captured output becomes the next implementation

### Blocked or clarification text

V1 does not introduce blocked-state semantics.

If an agent writes clarification text, uncertainty, or “I am blocked,” the system treats that text as ordinary turn output and continues the loop exactly as the current command does.

## Output Isolation Strategy

V1 chooses the simplest isolation strategy rather than a diff-based model.

After each successful capture is persisted, the adapter resets the pane before the next turn so that the next capture can be interpreted as new output.

For planning purposes, a turn has an explicit output surface:

- the surface starts immediately after the adapter submits the turn prompt into an already-reset pane
- the surface ends when the latest non-whitespace text in that pane ends with `<promise>done</promise>`
- prompt echo inside that surface is preserved in the raw capture, but does not count as completion unless it is also the final trailing text of the surface
- once the surface is persisted, the adapter resets the pane before any later prompt is submitted

The reset approach may use tmux commands such as screen clear and history clear. The exact tmux subcommands are an implementation detail, but the required behavior is:

- persist the raw pre-reset pane text first
- reset the pane after persistence
- treat the next capture as the next turn’s raw output surface

This design explicitly accepts raw CLI noise and prompt residue if they still appear in captures.

## Timeout Model

### Timeout type

V1 uses only a per-turn idle timeout.

There is:

- no hard per-turn cap
- no separate startup timeout
- no caller-configurable timeout

### Idle timeout rule

The timeout is 120 seconds.

The timer resets whenever the adapter observes new pane output for the current turn.

If `<promise>done</promise>` is not observed before the pane becomes idle for 120 seconds:

- the turn fails with a timeout runtime error
- current pane captures are persisted best-effort
- the command exits with code `1`
- the tmux session is killed immediately

## Runtime Artifact Contract

Each run persists artifacts to:

```text
log/runs/<uuidv7>/
```

The directory must contain at least:

- `metadata.json`
  - run ID
  - started timestamp
  - finished timestamp when available
  - implementer backend
  - reviewer backend
  - max iterations
  - tmux session name
  - timeout configuration
- `state-transitions.jsonl`
  - ordered lifecycle events such as `run_started`, `pane_started`, `turn_started`, `turn_completed`, `turn_timed_out`, `approved`, `non_converged`, `failed`, `closed`
  - each event record must include at least timestamp, run ID, event type, role when applicable, and iteration index when applicable
- `captures/`
  - one raw text file per completed turn
  - additional raw text files for startup failure or timeout captures when applicable
- `result.json`
  - final command outcome, exit code, approval status, iterations used, and any terminal error summary

Capture filenames must be stable and iteration-addressable. Iteration numbering follows the existing loop semantics:

- `iter-0` is the initial implementer turn
- `iter-N-reviewer` for `N >= 1` is the reviewer response for review round `N`
- `iter-N-implementer` for `N >= 1` is the rewrite produced after reviewer feedback from that same review round `N`

Examples:

- `captures/iter-0-implementer.txt`
- `captures/iter-1-reviewer.txt`
- `captures/iter-1-implementer.txt`

Failure-only captures should use explicit suffixes rather than inventing new numbering semantics, for example:

- `captures/startup-failed-implementer.txt`
- `captures/iter-1-reviewer-timeout.txt`

Pane capture files contain raw tmux text exactly as captured:

- backend text
- prompt echo
- CLI noise
- `<promise>done</promise>`
- `<promise>APPROVED</promise>` when present

### Artifact persistence failure

Artifact persistence is part of the success contract.

If the agent loop succeeds logically but required runtime artifacts cannot be written:

- the command fails
- exit code is `1`
- the persistence error is surfaced on `stderr`

## Failure Handling

### Launch/runtime failure

If tmux is unavailable, a pane cannot be created, or the backend launch command fails before a turn completes:

- fail fast
- surface the error on `stderr`
- exit code `1`
- preserve any artifacts that were successfully written before failure

There is no fallback to the old one-shot subprocess path.

### Timeout failure

Timeout is treated as a runtime failure, not a validation failure.

### Non-convergence

If the run reaches `maxIterations` without reviewer approval:

- behavior stays the same as today
- exit code is `1`
- runtime artifacts still persist
- tmux session is killed during cleanup

### Cleanup

Tmux sessions do not survive run completion in v1.

On both success and failure, the orchestrator kills the run’s tmux session during cleanup after both adapters have been asked to close their pane-local backend processes. Cleanup is best-effort, but a cleanup error is still recorded in artifacts and surfaced on `stderr` if it is the terminal failure.

## Backend-Specific Ownership

`codex` and `claude` adapters are the only supported backends in v1.

Each adapter owns:

- its interactive launch command
- its startup prompt text
- its turn prompt paste behavior
- its done-marker observation logic
- its pane reset/cleanup mechanics

The orchestrator owns:

- task reading and validation
- run ID generation
- tmux session creation and pane target allocation
- iteration control
- prompt composition for initial implementation, review, and rewrite turns
- approval detection
- stdout/stderr transcript forwarding
- runtime artifact persistence
- exit codes and failure classification

This split keeps the feature narrow: tmux semantics live behind the backend wrappers, while the command-level orchestration flow stays recognizable.

## Compatibility and Intentional Behavior Changes

The design keeps the CLI surface and core loop intact, but several internal behaviors intentionally change:

- backend-native session IDs and resume paths are removed from the adapters
- interactive panes persist across turns instead of subprocesses ending after each call
- startup prompts become part of adapter initialization
- `<promise>done</promise>` now appears in semantic outputs and final printed output
- runtime artifacts are now a required part of successful execution

These are acceptable because they support the main goal while preserving the primary command UX.

## Testing Requirement

V1 testing should be integration-led.

The design should be implemented with tmux-backed integration coverage that exercises:

- run startup with both panes
- successful implementer/reviewer loop completion
- reviewer approval detection with `<promise>APPROVED</promise>` plus `<promise>done</promise>`
- timeout when done marker never appears
- artifact creation under `log/runs/<run-id>/`
- cleanup of tmux session on success and failure
- launch failure when tmux or backend startup fails

Unit tests may still exist, but they are not the primary design requirement for v1.

## Open Risks Accepted in V1

- Keeping `<promise>done</promise>` in semantic outputs pollutes reviewer inputs and final printed output.
- Idle-time detection without a hard cap can leave very long active turns running indefinitely if output keeps changing.
- Clearing the pane between turns is simpler than diffing but may preserve some CLI noise and depends on tmux reset behavior.
- No human intervention model means live panes are for observation only in the intended v1 flow.

These are accepted constraints rather than unresolved design gaps.
