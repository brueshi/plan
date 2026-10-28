// Package rexctl finds the coding agent beside a pane and types into it,
// using the rex CLI.
package rexctl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AgentApp is the app name Claude Code reports in its program status.
const AgentApp = "claude-code"

var (
	ErrNoAgent = errors.New("no claude pane in this session")
	// ErrAgentBlocked guards against typing into a permission prompt,
	// where Enter would answer it.
	ErrAgentBlocked = errors.New("claude is waiting for an answer; reply to it first")
)

// Runner runs the rex CLI and returns its standard output.
type Runner func(ctx context.Context, args ...string) ([]byte, error)

// CLI runs the rex binary that matches the server: Rex puts its
// directory in REX_BIN_DIR for every terminal it opens.
func CLI(ctx context.Context, args ...string) ([]byte, error) {
	bin := "rex"
	if dir := os.Getenv("REX_BIN_DIR"); dir != "" {
		bin = filepath.Join(dir, "rex")
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("rex %s: %s", args[0], msg)
		}
		return nil, fmt.Errorf("rex %s: %w", args[0], err)
	}
	return out, nil
}

// Agent is a pane running a coding agent.
type Agent struct {
	Block     string
	Window    string
	State     string
	UpdatedAt string
}

type blockList struct {
	Blocks []struct {
		BlockID  string `json:"block_id"`
		WindowID string `json:"window_id"`
	} `json:"blocks"`
}

type statusList struct {
	Records []struct {
		State        string `json:"state"`
		EffectiveApp string `json:"effective_app"`
		UpdatedAt    string `json:"updated_at"`
	} `json:"records"`
}

// FindAgent picks the agent pane for self: one in the same tab if any,
// otherwise anywhere in the session, preferring the latest to report.
func FindAgent(ctx context.Context, run Runner, session, self string) (Agent, error) {
	out, err := run(ctx, "block", "ls", "-s", session, "--json")
	if err != nil {
		return Agent{}, err
	}
	var blocks blockList
	if err := json.Unmarshal(out, &blocks); err != nil {
		return Agent{}, fmt.Errorf("reading rex block list: %w", err)
	}

	selfWindow := ""
	for _, b := range blocks.Blocks {
		if b.BlockID == self {
			selfWindow = b.WindowID
		}
	}

	var best *Agent
	better := func(a Agent) bool {
		if best == nil {
			return true
		}
		if sameTab, bestSameTab := a.Window == selfWindow, best.Window == selfWindow; sameTab != bestSameTab {
			return sameTab
		}
		// RFC 3339 times from one server sort as strings.
		return a.UpdatedAt > best.UpdatedAt
	}
	for _, b := range blocks.Blocks {
		if b.BlockID == self {
			continue
		}
		out, err := run(ctx, "block", "call", "-s", session, "-b", b.BlockID, "program_status")
		if err != nil {
			continue // not a terminal, or closed meanwhile
		}
		var status statusList
		if json.Unmarshal(out, &status) != nil {
			continue
		}
		for _, r := range status.Records {
			if r.EffectiveApp != AgentApp {
				continue
			}
			a := Agent{Block: b.BlockID, Window: b.WindowID, State: r.State, UpdatedAt: r.UpdatedAt}
			if better(a) {
				best = &a
			}
		}
	}
	if best == nil {
		return Agent{}, ErrNoAgent
	}
	return *best, nil
}

// Send types message into the agent's pane and presses Enter.
func Send(ctx context.Context, run Runner, session string, a Agent, message string) error {
	if a.State == "blocked" {
		return ErrAgentBlocked
	}
	if strings.ContainsAny(message, "\r\n") {
		return errors.New("message must be one line: a newline would send it early")
	}
	// "--" keeps a task that starts with "-" from reading as a flag.
	if _, err := run(ctx, "send", "-s", session, "-b", a.Block, "--", message); err != nil {
		return err
	}
	_, err := run(ctx, "send-key", "-s", session, "-b", a.Block, "enter")
	return err
}
