// Command planner is a personal planning agent: a chat REPL backed by an LLM
// with tools that manipulate a local SQLite task board, plus a TUI board view.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/webcloster-dev/planner/internal/agent"
	"github.com/webcloster-dev/planner/internal/config"
	"github.com/webcloster-dev/planner/internal/llm"
	"github.com/webcloster-dev/planner/internal/store"
	"github.com/webcloster-dev/planner/internal/tools"
	"github.com/webcloster-dev/planner/internal/tui"
)

const systemPrompt = `You are a personal planning agent. The user tells you what they are doing,
planning, postponing, or finishing during the day, and you keep their local task board in sync
using the provided tools.

Rules:
- Create tasks with a type (FEAT, FIX, HOTFIX, TEST, EPIC) and a short title.
- When the user makes progress, postpones, blocks, or finishes something, update the matching
  task's status with set_status.
- Prefer calling tools over describing what you would do. Keep replies short and concrete.`

func main() {
	cmd := "chat"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "chat":
		err = runChat()
	case "tui":
		err = runTUI()
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
  planner          start the chat agent (default)
  planner tui      open the task board (read-only)
  planner config   write default config if missing and print its path
  planner help     show this help

in chat, slash commands: /todos  /model <name>  /help  /quit
`)
}

func configPath() string {
	if p := os.Getenv("PLANNER_CONFIG"); p != "" {
		return p
	}
	return config.DefaultPath()
}

func openStore(cfg config.Config) (store.TaskStore, error) {
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
	if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
		if err := config.Save(path, cfg); err != nil {
			return err
		}
		fmt.Println("wrote default config:", path)
	} else {
		fmt.Println("config:", path)
	}
	fmt.Println("active provider:", cfg.ActiveProvider)
	fmt.Print("providers: ")
	names := make([]string, 0, len(cfg.Providers))
	for n := range cfg.Providers {
		names = append(names, n)
	}
	sort.Strings(names)
	fmt.Println(strings.Join(names, ", "))
	return nil
}

func runTUI() error {
	cfg, err := config.Load(configPath())
	if err != nil {
		return err
	}
	st, err := openStore(cfg)
	if err != nil {
		return err
	}
	defer st.Close()
	tasks, err := st.List(context.Background(), store.Filter{})
	if err != nil {
		return err
	}
	return tui.Run(tasks)
}

func runChat() error {
	cfg, err := config.Load(configPath())
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

	reg := tools.New(st)
	ag := agent.New(provider, reg, systemPrompt)
	ctx := context.Background()

	fmt.Printf("planner — provider: %s (type /help)\n", ag.Provider())
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for {
		fmt.Print("> ")
		if !sc.Scan() {
			fmt.Println()
			return nil
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			if quit := handleSlash(ctx, line, cfg, ag, st); quit {
				return nil
			}
			continue
		}
		reply, err := ag.Send(ctx, line)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			continue
		}
		fmt.Println(reply)
	}
}

func handleSlash(ctx context.Context, line string, cfg config.Config, ag *agent.Agent, st store.TaskStore) (quit bool) {
	fields := strings.Fields(line)
	switch fields[0] {
	case "/quit", "/exit", "/q":
		return true
	case "/help":
		fmt.Println("/todos  list tasks\n/model <name>  switch provider\n/quit  exit")
	case "/todos":
		tasks, err := st.List(ctx, store.Filter{})
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return false
		}
		if len(tasks) == 0 {
			fmt.Println("(no tasks)")
		}
		for _, t := range tasks {
			fmt.Printf("  %d [%s] %-6s %s (%s)\n", t.ID, t.Label, t.Type, t.Title, t.Status)
		}
	case "/model":
		if len(fields) < 2 {
			fmt.Println("usage: /model <name>")
			return false
		}
		p, err := buildProvider(cfg, fields[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return false
		}
		ag.SetProvider(p)
		fmt.Println("provider:", p.Name())
	default:
		fmt.Println("unknown command; try /help")
	}
	return false
}

func buildProvider(cfg config.Config, name string) (llm.Provider, error) {
	pc, ok := cfg.Providers[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not found in config", name)
	}
	return llm.Build(pc)
}
