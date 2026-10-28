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

## Status

| Stage | Scope | State |
| --- | --- | --- |
| 1 | Task list parser and atomic, locked file updates | Done |
| 2 | Checklist view (Bubble Tea) with live reload | Next |
| 3 | Rex integration: keybinding, tab progress, send to agent | Planned |
| 4 | Agent integration: `plan` CLI, skill, prompt hook | Planned |

## Development

```sh
go test -race ./...
```
