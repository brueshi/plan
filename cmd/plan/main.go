// Command plan shows a markdown checklist that you and coding agents
// edit together.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/brueshi/plan/internal/active"
	"github.com/brueshi/plan/internal/view"
)

const usage = `usage:
  plan [view] [FILE[#HEADING]]  show a checklist
  plan use FILE[#HEADING]       make it this Rex session's active plan
  plan new TITLE                start a scratch plan and make it active
  plan which                    print the plan that plan would show

With no FILE, plan shows this session's active plan, or PLAN.md at
the root of the current repo. #HEADING narrows a file to the tasks
under one heading, matched by its text or by its GitHub anchor.`

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "plan:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	cmd := "view"
	if len(args) > 0 {
		switch args[0] {
		case "view", "use", "new", "which":
			cmd, args = args[0], args[1:]
		case "-h", "--help", "help":
			fmt.Fprintln(stdout, usage)
			return nil
		}
	}
	session := os.Getenv("REX_SESSION")

	switch cmd {
	case "use":
		if len(args) != 1 {
			return errors.New("use takes one FILE[#HEADING]\n" + usage)
		}
		t := active.Parse(args[0])
		if err := active.Use(session, t); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "active plan:", t)
		return nil

	case "new":
		if len(args) == 0 {
			return errors.New("new needs a title\n" + usage)
		}
		path, err := active.NewScratch(strings.Join(args, " "), time.Now())
		if err != nil {
			return err
		}
		if session != "" {
			if err := active.Use(session, active.Target{Path: path}); err != nil {
				return err
			}
		}
		fmt.Fprintln(stdout, path)
		return nil

	case "which":
		t, err := resolve(session, args)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, t)
		return nil
	}

	t, err := resolve(session, args)
	if err != nil {
		return err
	}
	m, err := view.New(t.Path, t.Section)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m).Run()
	return err
}

func resolve(session string, args []string) (active.Target, error) {
	switch len(args) {
	case 0:
	case 1:
		return active.Parse(args[0]), nil
	default:
		return active.Target{}, errors.New("too many arguments\n" + usage)
	}
	dir, err := os.Getwd()
	if err != nil {
		return active.Target{}, err
	}
	t, err := active.Resolve(session, dir)
	if errors.Is(err, active.ErrNoPlan) {
		return t, errors.New(`no plan for this session or repo. Start one with:
  plan new "Title"          a scratch plan
  plan use FILE[#HEADING]   a plan in an existing file
  or create PLAN.md at the repo root`)
	}
	return t, err
}
