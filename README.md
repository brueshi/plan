# plan

A live checklist pane for [Rex](https://www.superlogical.com/rex/docs/), so
you can watch your coding agent's plan beside the session and edit it with
the agent.

> **Unofficial.** This is a community add-on. It is not affiliated with or
> endorsed by Superlogical, the makers of Rex.

```
 2026-10-28-release-pipeline.md                     5/8 ▓▓▓▓▓▓░░░░
 ──────────────────────────────────────────────────────────────────
   [x] Stage 1: Audit the current deploy scripts
   [x] Stage 2: Containerize the API
   [x] Stage 3: CI build and test
 ▸ [~] Stage 4: Staging environment
   [ ] Stage 5: Production rollout
   [ ] Stage 6: Remove the old scripts
 ──────────────────────────────────────────────────────────────────
 space done · ~ doing · a add · A sub · e edit · dd del · s send · q quit
```

## Why

Plans made with an agent often live only in the conversation, or in one
section of a long document. `plan` puts the plan in a pane next to the
agent:

- The agent writes its plan to a markdown checklist and checks off stages
  as it works. The pane shows each change within a third of a second.
- You check off, add, edit, and reorder tasks in the same file, with
  Vim-style keys.
- One key sends a task to the agent beside the pane.

## Requirements

- [Rex](https://www.superlogical.com/rex/docs/installation)
- Go 1.26 or later to build. Older Go versions download 1.26 automatically.
- macOS. Linux should work, but nobody has tested it yet.
- Optional: [Claude Code](https://claude.com/claude-code) as the agent.
  The send and ask keys look for panes that report themselves as Claude
  Code through Rex's program status.

## Install

1. Install the binary:

   ```sh
   go install github.com/brueshi/plan/cmd/plan@latest
   ```

2. Add the pane to Rex. Copy [`contrib/rex/plan.lua`](contrib/rex/plan.lua)
   next to your `init.lua` (usually `~/.config/rex/`), then add this line
   to `init.lua`:

   ```lua
   require("plan").setup{ key = "cmd+shift+l" }
   ```

   Choose any free key. Run `rex config reload`. The key toggles the plan
   pane on the right of the current tab. The action is also in the command
   palette as "Toggle Plan Checklist".

3. Optional: ask your agent to keep plans in `plan`. Add this to your
   `CLAUDE.md` or `AGENTS.md`:

   ```md
   - In a Rex session (`REX_SESSION` is set), write the agreed plan to a
     checklist I can watch: run `plan new "<short title>"`, put one
     `- [ ]` task per stage in the file it prints with steps as indented
     subtasks, mark the current stage `[~]`, and check off each stage as
     it completes.
   ```

   Without this, press `w` on the pane's waiting screen. That sends the
   same request to the agent.

## How a plan is found

The pane follows the Rex session's active plan:

1. The plan an agent made active with `plan new` or `plan use`.
2. Otherwise, `PLAN.md` at the root of the repo.
3. Otherwise, a waiting screen. It switches to the plan when one appears.

```sh
plan                              # follow the session's active plan
plan docs/design.md               # show every task in a file
plan docs/design.md#rollout       # show the tasks under one heading
plan use docs/design.md#rollout   # make it the session's active plan
plan new "Auth refactor"          # start a scratch plan and make it active
plan which                        # print the plan that plan would show
```

A heading matches by its text or by its GitHub anchor. Scratch plans and
active-plan pointers are stored in `$XDG_STATE_HOME/plan`, or
`~/.local/state/plan`.

## Keys

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move |
| `gg` / `G` | Top / bottom |
| `space` or `x` | Toggle done |
| `~` | Toggle in progress |
| `a` / `A` | Add a task below / add a subtask |
| `e` or `enter` | Edit |
| `dd` | Delete. Asks first when the task has subtasks |
| `J` / `K` | Move the task down / up |
| `s` | Send the task to the agent beside the pane |
| `w` | On the waiting screen, ask the agent to write its plan |
| `q` | Quit |

## Format

Plans are GitHub task lists, plus `[~]` for work in progress:

```md
- [ ] To do
- [~] In progress
- [x] Done
  - [ ] Indented tasks are subtasks
```

A plan can be a whole file or one section of a larger document. `plan`
rewrites only task lines and keeps every other byte, line endings
included. Task-like lines in fenced code blocks are ignored.

## How it works with an agent

- **Safe concurrent edits.** Every edit reads the file again, changes one
  line, and replaces the file in one step, under a lock. The pane and an
  agent can edit at the same moment without losing a change.
- **Tasks are matched by text,** not by line number. A task added above the
  cursor does not shift a toggle onto the wrong task.
- **Tab status.** The pane reports its progress to Rex. It reports idle
  with a count while tasks remain, so the tab spinner still means "an agent
  is working". It reports done when the last task is checked.
- **Send guard.** `s` and `w` type into the agent's pane and press Enter.
  They refuse when the agent waits on a prompt, where Enter would answer
  the prompt.

## Limits

- The active plan belongs to the Rex session, not to one tab. With several
  agents in one session, the last `plan new` or `plan use` wins.
- Send and ask look only for Claude Code panes.
- Unix only. File locking uses `flock`. Tested on macOS.

## Development

```sh
go test -race ./...
```

## License

[MIT](LICENSE)
