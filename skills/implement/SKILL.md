---
name: implement
description: "Execute a design spec and implementation plan produced by implementation-design. Starts with fresh context — reads all requirements from disk."
---

# Implement from Plan

Execute an implementation plan that was produced by the `implementation-design` skill. This skill starts with **zero prior context** — everything it needs is in the artifacts on disk.

## Inputs

Expected invocation format:

```
/implementation-design <directory_path>
```

Retrieve the design doc and implementation specification from the arguments. If the documents could not be found, notify the user and end the skill.

## Steps

1. **Register tools**: Execute the `register-development-tools` skill.
2. **Read artifacts**: Read both the design spec and implementation plan from disk. These are your sole source of truth — do not assume any prior context.
3. **Record base SHA**: Run `git rev-parse HEAD` and save it as `BASE_SHA` (the commit before implementation begins).
4. **Implement**: Invoke the `subagent-driven-execution` skill with the implementation plan.
5. **Review and fix**: Invoke the `review-and-fix` skill with `BASE_SHA`, `SPEC_FILE_PATH`, and `PLAN_FILE_PATH`.
