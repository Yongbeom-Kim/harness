# implement-with-reviewer Product Contract

## Purpose

`implement-with-reviewer` is a CLI that runs a two-role agent loop:

- an `implementer` agent produces an implementation for a task read from `stdin`
- a `reviewer` agent reviews that implementation and either approves it or returns actionable feedback
- if not approved, the implementer rewrites the implementation using the reviewer feedback
- the loop repeats until approval or the maximum iteration limit is reached

The command is intended to orchestrate agent-to-agent implementation and review, while streaming the interaction transcript to the caller.

## Invocation

```sh
cat task.txt | implement-with-reviewer --implementer <backend> --reviewer <backend> [--max-iterations N]
```

Supported backends:

- `codex`
- `claude`

## Inputs

### Required flags

- `--implementer <backend>`
- `--reviewer <backend>`

### Optional flags

- `--max-iterations <N>`

### Environment variables

- `MAX_ITERATIONS`
  - used only when `--max-iterations` is not provided
  - default value when neither is provided: `10`

### Standard input

- The full task is read from `stdin`
- Trailing newline characters are removed before execution
- A task that is empty or whitespace-only is rejected

## Validation Contract

The command exits with code `2` and prints an error to `stderr` when:

- `--implementer` is missing
- `--reviewer` is missing
- unknown backend is provided for either role
- positional arguments are provided
- `--max-iterations` is not a positive integer
- `MAX_ITERATIONS` is set but not a positive integer, and `--max-iterations` is not provided
- the task from `stdin` is empty or whitespace-only

`-h` / help exits with code `0`.

## Execution Flow

1. Print a run header to `stdout`:
   - `Implementer : <backend>`
   - `Reviewer    : <backend>`
   - `Task        : <task>`
2. Send the original task to the implementer.
3. For each review iteration from `1` to `maxIterations`:
   - send the original task and current implementation to the reviewer
   - if approved, print the final result and exit successfully
   - otherwise send the original task, previous implementation, and reviewer feedback back to the implementer for rewrite
4. If no approval is received after `maxIterations`, print a non-convergence message and exit with failure

## Prompt Contract

### Implementer prompt shape

Initial implementer call:

```text
<implementer system prompt>

<task>
```

Rewrite implementer call:

```text
<implementer system prompt>

Original task:
<task>

Your previous implementation:
<previous implementation>

Reviewer feedback:
<reviewer feedback>

Rewrite addressing all feedback.
```

### Reviewer prompt shape

```text
<reviewer system prompt>

Task given to implementer:
<task>

Implementation:
<current implementation>
```

### System prompts

Implementer system prompt:

```text
You are an expert software implementer. When given a task or reviewer feedback, output only clean, working code. No explanations, no markdown fences unless the task explicitly requires a file.
```

Reviewer system prompt:

```text
You are a strict code reviewer. Review the implementation provided. If it is correct, complete, and handles edge cases properly, respond with exactly: <promise>APPROVED</promise> - nothing else. Otherwise respond with specific, actionable feedback only. No praise, no filler.
```

## Approval Contract

Approval marker:

```text
<promise>APPROVED</promise>
```

Current approval behavior is substring-based:

- a review is treated as approved if the reviewer output contains the approval marker anywhere in its `stdout`
- the reviewer response does not need to exactly equal the approval marker for the run to succeed

## Output Contract

### Transcript format

Each agent invocation prints a banner to `stdout` before agent output:

```text
--- iter <n> - <ROLE> (<backend>) ---
```

Where:

- implementer initial call uses `iter 0 - IMPLEMENTER (<backend>)`
- reviewer calls use `iter <n> - REVIEWER (<backend>)`
- implementer rewrites use `iter <n> - IMPLEMENTER (<backend>)`

Agent `stdout` is forwarded to the command's `stdout`.

Agent `stderr` is forwarded to the command's `stderr`.

If an agent stream does not end with a newline, the command appends one before continuing output.

### Success output

On approval, the command prints:

```text
Approved after <N> review round(s).

Final implementation
<latest implementation>
```

Exit code: `0`

### Non-convergence output

If the loop reaches the iteration limit without approval, the command prints:

```text
Did not converge after <N> iterations.
```

Exit code: `1`

### Runtime failure output

If reading `stdin` fails, or an agent invocation returns an error:

- the command prints the underlying error to `stderr`
- agent stderr, if any, is still surfaced
- exit code: `1`

Agent invocation failures are reported as:

```text
agent invocation failed: <error>
```

## Backend Contract

Backend selection is currently fixed to:

- `codex` -> `cli.NewCodexCliTool(nil)`
- `claude` -> `cli.NewClaudeCliTool(nil)`

Any other backend name is rejected.

The command depends on the selected backend adapter being able to:

- accept a single message string per invocation
- return `stdout`, `stderr`, and an execution error
- manage any backend-specific session behavior internally

## Exit Codes

- `0`: success, including `-h`
- `1`: runtime failure or non-convergence
- `2`: usage or validation error

## Non-Goals

This command does not currently guarantee:

- exact approval matching instead of substring matching
- structured machine-readable output
- persistence of the final implementation to a file
- deterministic agent behavior
- semantic validation of generated code beyond reviewer approval
