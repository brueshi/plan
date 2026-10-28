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
plan                          # PLAN.md in the current directory
plan docs/design.md           # every task in a file
plan docs/design.md#rollout   # only the tasks under one heading
```

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
| `q` | Quit |

Edits from agents and editors appear within a third of a second.

## Status

| Stage | Scope | State |
| --- | --- | --- |
| 1 | Task list parser and atomic, locked file updates | Done |
| 2 | Checklist view (Bubble Tea) with live reload | Done |
| 3 | Rex integration: keybinding, tab progress, send to agent | Next |
| 4 | Agent integration: `plan` CLI, skill, prompt hook | Planned |

## Development

```sh
go test -race ./...
```
