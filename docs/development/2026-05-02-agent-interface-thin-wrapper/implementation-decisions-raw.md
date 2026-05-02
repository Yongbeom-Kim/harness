# Implementation Decisions

## 2026-05-02 Exploration Context

- Request: refactor the current `orchestrator/internal/agent` surface so the public agent interface maps to direct backend CLI / tmux actions rather than bundled turn orchestration.
- Desired direction from the user:
  - make the agent interface the real primary abstraction
  - keep it thin over tmux
  - expose direct actions such as start, send a prompt into the chat, and capture output
  - avoid spreading the main runtime story across `session`, `driver`, `protocol`, `runtime`, and `launcher` layers
- Existing relevant code:
  - `orchestrator/internal/agent/agent.go`
  - `orchestrator/internal/agent/codex.go`
  - `orchestrator/internal/agent/claude.go`
  - `orchestrator/internal/agent/session/types.go`
  - `orchestrator/internal/agent/session/runtime.go`
  - `orchestrator/internal/agent/session/standalone.go`
  - `orchestrator/internal/agent/session/dependencies.go`
  - `orchestrator/cmd/implement-with-reviewer/main.go`
  - `orchestrator/cmd/tmux_codex/main.go`
  - `orchestrator/cmd/tmux_claude/main.go`
- Existing design context:
  - `docs/development/2026-04-26-tmux-session-orchestration-skill/design-document.md`
  - `docs/development/2026-04-26-tmux-session-orchestration-skill/implementation-spec.md`
  - `docs/development/2026-04-27-tmux-abstraction/implementation-spec.md`
  - `docs/development/2026-04-27-implementer-reviewer-file-channel/implementation-spec.md`

## 2026-05-02 Question Round 1

1. What should the new primary runtime interface own?
A: A thin action-level interface such as `Start`, `SendPrompt`, `Capture`, `Interrupt`, `Close`, and `SessionName`, while turn completion polling and output parsing move up into workflow code or small workflow-owned helpers. `(Recommended)`
B: Keep a high-level session interface with `Start` and `RunTurn`, but rename packages so the layering feels smaller.
C: Keep both levels: a low-level action interface plus the current high-level `RunTurn` interface in parallel.
D: Remove the shared interface entirely and let each workflow talk to `codex` / `claude` concrete types directly.

2. What should `Start` do in the refactored model?
A: `Start` should launch or attach to the backend CLI in tmux and wait until the backend is ready for input, but it should not send the role prompt; the first prompt is sent explicitly through `SendPrompt`. `(Recommended)`
B: `Start(rolePrompt string)` should keep launching the backend and sending the startup role prompt in one call.
C: Construction should auto-start the backend, and `Start` should disappear.
D: `Start` should only create tmux topology; a separate `LaunchBackend` method should start the CLI process.

3. Where should the `<promise>done</promise>` protocol and turn-completion logic live after this refactor?
A: Outside the core agent interface, in `reviewloop` or a small workflow-owned helper, so the agent remains a raw transport/control wrapper over tmux-backed CLIs. `(Recommended)`
B: Keep a generic `internal/agent/protocol` package that every workflow must use.
C: Put completion parsing inside each backend implementation so `codex` and `claude` keep their own turn semantics.
D: Move the logic into the command entrypoints and keep it out of both `agent` and `reviewloop`.

4. How should backend-specific launch details be represented?
A: Replace `CodexAgent` / `ClaudeAgent` as behavior-heavy wrappers with a small backend definition catalog that declares launch command, ready matcher, default session name, and any startup quirks; the shared agent implementation consumes those definitions. `(Recommended)`
B: Keep separate `CodexAgent` / `ClaudeAgent` structs with most of the current fields and methods.
C: Hardcode backend-specific behavior directly in each command entrypoint.
D: Move backend metadata into external config files and keep Go code generic.

5. What package structure should the implementation target?
A: Collapse `internal/agent/session/*` into a tighter `internal/agent` surface plus narrowly scoped subpackages only where they correspond to real external concepts, such as `internal/agent/backend` and `internal/agent/tmux`. `(Recommended)`
B: Keep the current `session/runtime/driver/protocol/launcher` split, but rename types to make the hierarchy easier to read.
C: Leave the current package tree intact and only change the top-level interface names.
D: Move all tmux and backend code into `cmd/*` packages and keep `internal/agent` minimal.

6. What should happen to the standalone tmux launcher flow?
A: Keep the `tmux_codex`, `tmux_claude`, and `tmux_agent` commands, but make them thin CLI wrappers over the same refactored action-level agent primitives used by workflows. `(Recommended)`
B: Keep `RunStandalone` as a separate helper path under `internal/agent/session` even if the workflow interface changes.
C: Delete the standalone launcher commands and keep only workflow-driven session startup.
D: Keep standalone launch as methods on concrete backend structs, separate from the shared runtime primitives.

- Answers:
  - `1D`
  - `2` custom
  - `3D`
  - `4B`
  - `5A`
  - `6A`
- Raw response notes:
  - `1`: remove the runtime interface; workflows should talk to `codex` and `claude` concrete types directly.
  - `2`: `Start` should create the tmux session if needed, create the tmux pane if needed, launch the backend CLI in tmux, and stop there; readiness should be a separate method.
  - `3`: the `<promise>done</promise>` protocol should not live inside the core agent interface and should move into common entry points.
  - `4`: keep separate concrete backend structs for now; do not introduce a shared agent implementation in this refactor.
  - `5`: collapse the package tree to something close to `internal/agent` plus `internal/agent/tmux`, with any prompt or shell-injection helpers kept narrow.
  - `6`: keep the existing launcher commands and make them use the refactored primitives.
- Interpretations:
  - `1D`: remove the shared runtime interface and let workflow code depend on concrete backend types directly.
  - `2` custom: replace the current `Start(rolePrompt)` contract with a launch-focused `Start()` that owns tmux session/pane creation plus backend CLI launch, and add a separate `WaitUntilReady()` method.
  - `3D`: move turn-completion and response-parsing protocol logic out of the core agent abstraction and into higher-level command/workflow code.
  - `4B`: keep `CodexAgent` and `ClaudeAgent` as separate concrete implementations instead of introducing a shared backend-definition catalog in this change.
  - `5A`: remove the `session/runtime/driver/protocol/launcher` layering and collapse the runtime into a tighter `internal/agent` package with a small tmux-focused subpackage.
  - `6A`: preserve `tmux_codex`, `tmux_claude`, and `tmux_agent` as thin command wrappers over the refactored concrete agents.

## 2026-05-02 Question Round 2

7. Where should backend branching live after removing `reviewloop.Session` and `RunConfig.NewSession`?
A: Put the branching directly in `orchestrator/internal/reviewloop`, so the workflow package imports concrete `codex` / `claude` types and owns backend-specific startup, send, wait, and capture calls. `(Recommended)`
B: Move all backend branching to `orchestrator/cmd/implement-with-reviewer`, and make `reviewloop` accept already-started concrete agents through function closures or backend-specific structs.
C: Duplicate the full implementer-reviewer loop per backend pair in `cmd/implement-with-reviewer` and shrink `reviewloop` to prompt helpers plus artifact helpers only.
D: Keep a generic `reviewloop` package by introducing a new non-interface wrapper struct that still hides the concrete backend types behind shared function fields.

8. What exact method set should each concrete backend type expose in v1 of this refactor?
A: `Start()`, `WaitUntilReady()`, `SendPrompt(prompt string)`, `Capture() (string, error)`, `Interrupt() error`, `SessionName() string`, and `Close() error`, with no higher-level turn API. `(Recommended)`
B: `Start()`, `WaitUntilReady()`, `SendPrompt(prompt string)`, `Capture() (string, error)`, `SessionName() string`, and `Close() error`; leave interruption out of scope for now.
C: `Start()`, `SendPrompt(prompt string)`, `Capture() (string, error)`, and `Close() error`; keep both readiness and interruption outside the concrete backend types.
D: Expose the owning tmux session and pane directly and let workflows call tmux methods instead of going through a backend method set.

9. Where should role startup prompts be assembled and sent after `Start()` no longer accepts a prompt?
A: In `orchestrator/internal/reviewloop`; it should call `Start()`, then `WaitUntilReady()`, then send the role prompt explicitly as the first prompt for each backend. `(Recommended)`
B: Inside each concrete backend’s `Start()` method, even though `Start()` no longer accepts a role prompt parameter.
C: In `orchestrator/cmd/implement-with-reviewer/main.go`, before control enters `reviewloop`.
D: In a new prompt package under `internal/agent`, which owns startup prompt text plus the send order.

10. What should `Capture()` mean in the refactored concrete backend API?
A: Return the raw full tmux pane capture exactly as it exists at call time; turn slicing, delta tracking, and `<promise>done</promise>` parsing live outside the backend type. `(Recommended)`
B: Return only the current turn’s capture, with the backend remembering send boundaries and slicing internally.
C: Expose both `Capture()` and `CaptureSinceLastPrompt()`.
D: Do not expose capture directly; only expose `WaitUntilReady()` and `SendPrompt()`.

11. How should readiness detection be owned?
A: Each concrete backend type should own `WaitUntilReady()` and its backend-specific ready heuristics, because Codex and Claude have different startup surfaces. `(Recommended)`
B: `reviewloop` should poll raw captures and keep all readiness heuristics there.
C: Only `codex` should implement readiness checks; `claude` should be treated as ready immediately after launch.
D: Readiness should be manual only; automated waiting should be removed.

12. What should happen to the generic `tmux_agent` launcher once the refactor centers on concrete `codex` / `claude` types?
A: Keep `tmux_agent` as a thin command over `internal/agent/tmux` launch helpers only; it remains a generic launcher and does not participate in the concrete backend runtime model. `(Recommended)`
B: Delete `tmux_agent` and keep only backend-specific launchers.
C: Add a third concrete backend type for the `agent` CLI so every launcher command uses the same concrete-agent pattern.
D: Merge `tmux_agent` into the backend-specific commands as a flag.

13. How should runtime errors be represented after removing the shared session abstraction?
A: Keep one small shared error type in `internal/agent` for launch, readiness, capture, interrupt, and close failures, even though the runtime behavior is concrete-type-based. `(Recommended)`
B: Give `codex` and `claude` fully separate error types and let callers branch on concrete backend type when they care.
C: Return plain `error` everywhere and let `reviewloop` add role/backend context without any shared typed error.
D: Reuse tmux-layer errors directly and stop wrapping them in agent-level errors.

- Answers:
  - `7B`
  - `8B`
  - `9` custom
  - `10A`
  - `11A`
  - `12B`
  - `13A`
- Raw response notes:
  - `7`: move backend branching out of `reviewloop`.
  - `8`: choose option `B`.
  - `9`: remove startup prompts; there should not be a dedicated startup prompt stage.
  - `10`: choose option `A`.
  - `11`: choose option `A`.
  - `12`: remove the `tmux_agent` launcher.
  - `13`: choose option `A`.
  - Extra comment: "Why do we need a backend type?" This suggests we should avoid introducing an additional backend-typed abstraction in `reviewloop` if closures or direct concrete wiring are enough.
- Interpretations:
  - `7B`: move backend-name branching out of `orchestrator/internal/reviewloop` and into `orchestrator/cmd/implement-with-reviewer`, with `reviewloop` consuming prewired behavior rather than deciding which backend to instantiate.
  - `8B`: each concrete backend should expose `Start()`, `WaitUntilReady()`, `SendPrompt(prompt string)`, `Capture() (string, error)`, `SessionName() string`, and `Close() error`; interruption is out of scope for this refactor.
  - `9` custom: remove the dedicated startup-prompt phase entirely instead of merely relocating prompt assembly.
  - `10A`: `Capture()` should return the raw full tmux pane capture exactly as it exists at call time.
  - `11A`: each concrete backend owns its own readiness polling heuristics through `WaitUntilReady()`.
  - `12B`: delete `orchestrator/cmd/tmux_agent` as part of the refactor.
  - `13A`: keep a small shared agent-level error type for runtime failures even though workflow code uses concrete backends.

## 2026-05-02 Question Round 3

14. If there is no dedicated startup prompt, where should implementer/reviewer role instructions live?
A: Put the full role contract at the top of every workflow turn prompt built by `reviewloop`, so no separate startup phase exists and the backend types stay transport-only. `(Recommended)`
B: Put the role contract only in the first real prompt sent to each backend, and later prompts assume the persistent conversation carries that context forward.
C: Remove persistent role instructions entirely and rely only on task-specific per-turn prose.
D: Reintroduce a dedicated startup prompt after all.

15. What should `reviewloop` receive from `cmd/implement-with-reviewer` once backend branching moves out of the package?
A: A small set of per-role function closures such as `start`, `waitUntilReady`, `sendPrompt`, `capture`, `sendRaw`, `sessionName`, and `close`, all bound in `main.go` to concrete Codex or Claude instances. `(Recommended)`
B: Fully concrete backend structs passed through backend-specific runner structs.
C: A single shared helper struct with function fields that effectively replaces the old interface but is not declared as one.
D: Nothing; move the whole loop out of `reviewloop` and let `main.go` own the iteration logic directly.

16. How should side-channel delivery work once `InjectSideChannel(...)` disappears with the old session interface?
A: Add a low-level `SendRaw(text string) error` method on each concrete backend for non-turn text injection; `reviewloop` uses it for FIFO side-channel delivery while `SendPrompt(...)` remains the normal turn entrypoint. `(Recommended)`
B: Reuse `SendPrompt(...)` for side-channel text and let the workflow distinguish only by prompt content.
C: Let `reviewloop` talk directly to `internal/agent/tmux` for side-channel injection instead of going through the backend types.
D: Drop side-channel support from `implement-with-reviewer` in this refactor.

17. Where should `<promise>done</promise>` polling and turn slicing live if backend branching moves to `cmd/implement-with-reviewer`?
A: Keep it in `reviewloop` as a small workflow-owned helper that uses `Capture()` snapshots, prompt markers, and timeout rules. `(Recommended)`
B: Move it into `cmd/implement-with-reviewer/main.go` with the backend wiring so `reviewloop` becomes prompt/artifact helpers only.
C: Move it into `internal/agent/tmux` because polling is basically pane capture logic.
D: Put it back into each concrete backend implementation.

18. What should `Start()` do when the target tmux session already exists?
A: Reuse or attach to the named session and expected pane when possible, then ensure the backend CLI is running exactly once before returning. `(Recommended)`
B: Always fail if the named tmux session already exists.
C: Always create a fresh unique session name and never reuse existing tmux sessions.
D: Leave reuse-versus-create resolution to `main.go`; `Start()` should only launch once the tmux target is already prepared.

- Answers:
  - `14A`
  - `15` custom
  - `16B`
  - `17` custom
  - `18B`
- Raw response notes:
  - `14`: choose option `A`.
  - `15`: remove `reviewloop` package ownership entirely; let `cmd/implement-with-reviewer` own the whole loop.
  - `16`: reuse `SendPrompt(...)` for side-channel text.
  - `17`: same as `15`; `<promise>done</promise>` polling and turn slicing should also move into `cmd/implement-with-reviewer`.
  - `18`: fail fast if the session exists.
- Interpretations:
  - `14A`: prepend the full role contract to every workflow turn prompt so there is no dedicated startup prompt phase.
  - `15` custom: `orchestrator/cmd/implement-with-reviewer` should own the full implementer-reviewer loop, backend wiring, and command-specific runtime flow; the existing `internal/reviewloop` package should not remain the control center.
  - `16B`: side-channel delivery should reuse `SendPrompt(...)` rather than introducing a second text-injection method.
  - `17` custom: `<promise>done</promise>` completion polling and turn slicing should move into `orchestrator/cmd/implement-with-reviewer` together with the loop itself.
  - `18B`: `Start()` should fail immediately if the requested tmux session already exists.

## 2026-05-02 Question Round 4

19. If `cmd/implement-with-reviewer` owns the full loop, which support code should still remain outside the command package?
A: Keep only clearly reusable infrastructure such as `internal/filechannel`, `internal/dirlock`, and `internal/agent/tmux`; move prompts, artifacts, run metadata, completion polling, and session naming into `cmd/implement-with-reviewer`. `(Recommended)`
B: Keep `internal/filechannel` and also move artifacts into a new reusable internal package such as `internal/runartifact`.
C: Keep most of `internal/reviewloop`, but rename it so the package name is less misleading.
D: Move everything, including FIFO and tmux helpers, into `cmd/implement-with-reviewer`.

20. Where should the command-specific artifact schema and writer live after removing `internal/reviewloop` ownership?
A: In `orchestrator/cmd/implement-with-reviewer` alongside the loop, because the run metadata, transitions, captures, and result files are command-specific behavior. `(Recommended)`
B: In a new `orchestrator/internal/implementerreviewer` package dedicated only to artifacts and prompt helpers.
C: Keep them in `internal/reviewloop` temporarily and clean that up later.
D: In a generic `orchestrator/internal/artifact` package shared by all future workflows.

21. How should prompt construction be organized once role instructions are included on every turn?
A: Keep a small `prompts.go` file under `orchestrator/cmd/implement-with-reviewer` for prompt builders and role contracts, instead of inlining them into `main.go`. `(Recommended)`
B: Inline prompt construction directly in `main.go` because the command owns the whole loop now.
C: Put prompt builders under `internal/agent` because they are closely tied to backend sessions.
D: Move prompts into Markdown contract files and load them at runtime.

22. How should session names be chosen if `Start()` must fail when the tmux session already exists?
A: `cmd/implement-with-reviewer` should generate explicit per-run per-role session names from the run ID and pass them into concrete Codex/Claude constructors, so collisions are avoided by construction. `(Recommended)`
B: Each concrete backend should generate its own default session name internally.
C: Keep fixed session names like `codex` and `claude`, and require operators to clean up old tmux sessions before every run.
D: Add a required CLI flag so the user provides both tmux session names explicitly.

23. What is the preferred test seam for the command-owned loop once the shared runtime interface is gone?
A: Keep an injectable `runnerConfig`-style test seam in `cmd/implement-with-reviewer`, with constructor and helper function fields for concrete Codex/Claude creation, artifact writing, file channels, and run IDs. `(Recommended)`
B: Remove most command-level seams and test the loop only through fake tmux dependencies inside the concrete backend types.
C: Rely mostly on end-to-end tests for `implement-with-reviewer` and reduce unit seams.
D: Keep the old shared session interface only in tests.

24. If `Capture()` returns the full raw pane text, how should the command isolate the current turn?
A: Prefix each turn prompt with a unique marker generated by `cmd/implement-with-reviewer`, then slice the full pane capture from that marker while polling for `<promise>done</promise>`. `(Recommended)`
B: Clear/reset the pane before every turn and treat the next full capture as the current turn.
C: Both add a marker and clear the pane before every turn.
D: Do not isolate turns explicitly; just read the latest full capture and search for `<promise>done</promise>`.

25. If side-channel messages reuse `SendPrompt(...)`, how should they interact with turn markers and `<promise>done</promise>` polling?
A: Side-channel sends should not get turn markers or done-suffix instructions; the main turn loop must isolate its own prompts with markers so side-channel traffic does not confuse completion detection. `(Recommended)`
B: All sends, including side-channel traffic, should get markers and done-suffix instructions.
C: Pause side-channel delivery while a main turn is in progress.
D: Reintroduce a dedicated raw-send method after all.

- Answers:
  - `19A`
  - `20A`
  - `21A`
  - `22A`
  - `23A`
  - `24A`
  - `25A`
- Raw response notes:
  - `23`: use the command seam approach rather than reintroducing a shared production agent interface.
- Interpretations:
  - `19A`: keep only reusable infrastructure outside the command package: `internal/filechannel`, `internal/dirlock`, and `internal/agent/tmux`.
  - `20A`: move the implementer-reviewer artifact schema and writer into `orchestrator/cmd/implement-with-reviewer`.
  - `21A`: keep command-owned prompt builders in a separate `prompts.go` file under `cmd/implement-with-reviewer`.
  - `22A`: generate explicit per-run per-role session names in `cmd/implement-with-reviewer` and pass them into concrete backend constructors.
  - `23A`: keep a command-local injected seam for tests through constructor/helper function fields; do not reintroduce a shared production `Agent` interface.
  - `24A`: isolate turns by prefixing prompts with command-generated unique markers, then slice the raw pane capture from the marker while polling for `<promise>done</promise>`.
  - `25A`: side-channel sends should bypass marker/done decoration; only main turn prompts participate in marker-based slicing and done-marker completion detection.
