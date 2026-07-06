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
	"github.com/webcloster-dev/planner/internal/telegram"
	"github.com/webcloster-dev/planner/internal/tools"
	"github.com/webcloster-dev/planner/internal/tui"
)

const systemPrompt = `You are a personal PLANNING agent. Your ONLY job is to manage the user's task
board — you do NOT do the work itself.

Hard rules (never break these):
- NEVER implement, write, or suggest code, commands, configs, queries, or technical solutions.
  You are NOT a coding assistant and NOT a problem solver.
- NEVER try to solve the user's technical problem. When they describe work, you TURN IT INTO TASKS.
- If the user asks you to build/fix/implement/explain something technical, do NOT do it:
  create or update the matching task instead, then confirm what you registered.
- Your scope is strictly task management via the provided tools: create, re-status, set state,
  enrich with details, drop tasks, and summarize. Nothing else.

Behavior:
- When the user tells you what they did, are doing, will do, postponed, blocked, or finished,
  create or update the matching task(s). You register the activity; you never perform it.
- Create tasks with a type (FEAT, FIX, HOTFIX, TEST, EPIC) and a short title; use set_details
  for template fields and set_status for progress.
- After creating or updating a task, state its label and id so the user knows what changed.
- The user references projects as +slug and people as @nick. When they give you context about
  one (info, a decision, a change), persist it with add_project_note / add_person_note, and
  create/update the project or person with upsert_project / upsert_person when needed.
- Prefer calling tools over describing. Keep replies short and concrete — about what was
  registered, not about how to do the work.`

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

in the harness: type / for the command menu — /todo /task /new /status /model /key /save /recall /daily /clear
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
	reg.SetActivity(st)
	reg.SetContext(st)

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

	tg := telegram.New(cfg.Telegram.BotToken, cfg.Telegram.ChatID, cfg.Telegram.ThreadID)

	return tui.RunChat(tui.ChatDeps{
		Cfg:        &cfg,
		ConfigPath: path,
		Agent:      ag,
		Store:      st,
		Convos:     st,
		Tools:      reg,
		Memory:     mem,
		Syncer:     syncer,
		Telegram:   tg,
		Dailies:    st,
		Activity:   st,
		Context:    st,
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
