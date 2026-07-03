// Command planner is a personal planning agent: an interactive chat harness
// backed by an LLM with tools that manipulate a local SQLite task board.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/webcloster-dev/planner/internal/agent"
	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/contextmgr"
	"github.com/webcloster-dev/planner/internal/llm"
	"github.com/webcloster-dev/planner/internal/memory"
	"github.com/webcloster-dev/planner/internal/plane"
	"github.com/webcloster-dev/planner/internal/store"
	"github.com/webcloster-dev/planner/internal/tools"
	"github.com/webcloster-dev/planner/internal/tui"
)

const systemPrompt = `You are a personal planning agent. The user tells you what they are doing,
planning, postponing, or finishing during the day, and you keep their local task board in sync
using the provided tools.

Rules:
- Create tasks with a type (FEAT, FIX, HOTFIX, TEST, EPIC) and a short title.
- After creating or updating a task, state its label and id so the user knows what changed.
- When the user makes progress, postpones, blocks, or finishes something, update the matching
  task's status with set_status. Use set_details to enrich a task with template fields.
- Prefer calling tools over describing what you would do. Keep replies short and concrete.`

func main() {
	cmd := "chat"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "chat", "tui":
		err = runChat()
	case "config":
		err = runConfig()
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Print(`planner — personal planning agent

usage:
  planner          start the interactive chat harness (default)
  planner tui      alias for the chat harness
  planner config   open the configuration TUI (providers, keys, plane, context)
  planner help     show this help

in the harness: type / for the command menu — /todos /task /new /status /model /key /save /recall /clear
API keys: edit them in 'planner config', or set them live in the harness with /key.
`)
}

func configPath() string {
	if p := os.Getenv("PLANNER_CONFIG"); p != "" {
		return p
	}
	return config.DefaultPath()
}

func openStore(cfg config.Config) (*store.SQLite, error) {
	if err := os.MkdirAll(dir(cfg.DBPath), 0o755); err != nil {
		return nil, err
	}
	return store.OpenSQLite(cfg.DBPath)
}

func dir(path string) string {
	if i := strings.LastIndexByte(path, os.PathSeparator); i >= 0 {
		return path[:i]
	}
	return "."
}

func runConfig() error {
	path := configPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	return tui.RunConfig(&cfg, path)
}

func runChat() error {
	path := configPath()
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}
	provider, err := buildProvider(cfg, cfg.ActiveProvider)
	if err != nil {
		return err
	}
	st, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()

	mem := memory.Detect(cfg.Memory.Project)
	reg := tools.New(st)
	reg.SetMemory(mem)

	syncer := plane.NewSyncer(plane.New(plane.Config{
		BaseURL:       cfg.Plane.BaseURL,
		Token:         cfg.Plane.APIToken,
		WorkspaceSlug: cfg.Plane.WorkspaceSlug,
		ProjectID:     cfg.Plane.ProjectID,
	}), st, cfg.Plane.StateDefaults)
	syncer.SetEstimate(cfg.Plane.DefaultEstimate)
	reg.SetSyncer(syncer)

	ag := agent.New(provider, reg, systemPrompt)
	ag.SetWindow(contextmgr.New(cfg.ContextBudget))

	return tui.RunChat(tui.ChatDeps{
		Cfg:        &cfg,
		ConfigPath: path,
		Agent:      ag,
		Store:      st,
		Convos:     st,
		Tools:      reg,
		Memory:     mem,
		Syncer:     syncer,
		Build:      buildProvider,
	})
}

func buildProvider(cfg config.Config, name string) (llm.Provider, error) {
	pc, ok := cfg.Providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found in config", name)
	}
	return llm.Build(pc)
}
