// Command plan shows a markdown checklist that you and coding agents
// edit together.
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/brueshi/plan/internal/view"
)

const defaultPlan = "PLAN.md"

const usage = `usage: plan [view] [FILE[#HEADING]]

Shows the checklist in FILE, or in PLAN.md when FILE is left out.
#HEADING narrows it to the tasks under that heading, matched by its
text or by its GitHub anchor, such as docs/design.md#rollout-plan.`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "plan:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) > 0 && args[0] == "view" {
		args = args[1:]
	}
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help") {
		fmt.Println(usage)
		return nil
	}
	if len(args) > 1 {
		return errors.New("too many arguments\n" + usage)
	}

	target := defaultPlan
	if len(args) == 1 {
		target = args[0]
	}
	path, section := splitTarget(target)

	m, err := view.New(path, section)
	if err != nil {
		return err
	}
	_, err = tea.NewProgram(m).Run()
	return err
}

// splitTarget separates FILE#HEADING. A file whose own
// name contains # is opened whole.
func splitTarget(target string) (path, section string) {
	if _, err := os.Stat(target); err == nil {
		return target, ""
	}
	i := strings.LastIndex(target, "#")
	if i < 0 {
		return target, ""
	}
	return target[:i], target[i+1:]
}
