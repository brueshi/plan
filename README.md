# plan

A live checklist that you and your coding agents edit together, shown in a
[Rex](https://www.superlogical.com/rex/docs/) pane.

The plan is plain markdown. It can be a dedicated file, one section of a
larger document, or a scratch plan with no file of its own. Only task lines
are ever rewritten; every other byte of the file is left as it was.

## Task format

GitHub task list syntax, with one addition for work in progress:

```md
- [ ] To do
- [~] In progress
- [x] Done
  - [ ] Nested tasks are subtasks
```

Task-like lines inside fenced code blocks are ignored.

## Usage

```sh
plan                          # this session's active plan, or the repo's PLAN.md
plan docs/design.md           # every task in a file
plan docs/design.md#rollout   # only the tasks under one heading
plan use docs/design.md#rollout   # make it the Rex session's active plan
plan new "Auth refactor"      # start a scratch plan outside the repo
plan which                    # print the plan that plan would show
```

Scratch plans and each session's active-plan pointer live in
`$XDG_STATE_HOME/plan`, or `~/.local/state/plan`.

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move |
| `gg` / `G` | Top / bottom |
| `space` or `x` | Toggle done |
| `~` | Toggle in progress |
| `a` / `A` | Add a task below / add a subtask |
| `e` or `enter` | Edit |
| `dd` | Delete (asks first when the task has subtasks) |
| `J` / `K` | Move the task down / up |
| `s` | Send the task to the claude pane beside this one |
| `q` | Quit |

Edits from agents and editors appear within a third of a second.

## In Rex

- The view reports its progress to Rex as program status: idle with a
  count while tasks remain, and done when the last one is checked.
- `s` types the task into the nearest Claude Code pane: the same tab
  first, then the session. It refuses when that agent is waiting on a
  prompt, where Enter would answer it.
- A Rex action can toggle the view as a full-height pane on the right
  of the current tab. See `plan_pane` in the author's `init.lua`.

## Status

| Stage | Scope | State |
| --- | --- | --- |
| 1 | Task list parser and atomic, locked file updates | Done |
| 2 | Checklist view (Bubble Tea) with live reload | Done |
| 3 | Rex integration: active plans, tab status, send to agent | Done |
| 4 | Agent integration: `plan` CLI, skill, prompt hook | Next |

## Development

```sh
go test -race ./...
```
