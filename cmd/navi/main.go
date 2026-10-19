package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"navi/pkg/navi"
)

// ANSI Styling
const (
	ColorReset  = "\033[0m"
	ColorBold   = "\033[1m"
	ColorDim    = "\033[2m"
	ColorCyan   = "\033[36m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorRed    = "\033[31m"
	ColorGray   = "\033[90m"
	ColorPurple = "\033[35m"
)

type SlashCommand struct {
	Name        string
	Args        string
	Description string
	Execute     func(engine *navi.Engine, args string) bool // returns true to exit program
}

var commandRegistry = map[string]SlashCommand{}

func registerCommands() {
	commandRegistry["help"] = SlashCommand{
		Name:        "/help",
		Args:        "",
		Description: "Display available slash commands and usage",
		Execute: func(engine *navi.Engine, args string) bool {
			printCommandPreview("")
			return false
		},
	}

	commandRegistry["clear"] = SlashCommand{
		Name:        "/clear",
		Args:        "",
		Description: "Reset conversation history to start a fresh token window",
		Execute: func(engine *navi.Engine, args string) bool {
			engine.ResetHistory()
			fmt.Printf("%s✔ Memory cleared. Active context tokens reset to zero.%s\n", ColorGreen, ColorReset)
			return false
		},
	}

	commandRegistry["stats"] = SlashCommand{
		Name:        "/stats",
		Args:        "",
		Description: "View cumulative session cost, token usage, and cache stats",
		Execute: func(engine *navi.Engine, args string) bool {
			s := engine.GetStats()
			fmt.Printf("\n%s📊 Session Metrics:%s\n", ColorBold, ColorReset)
			fmt.Printf("  • Model:        %s%s%s\n", ColorCyan, s.Model, ColorReset)
			fmt.Printf("  • Input Tokens:  %d\n", s.InputTokens)
			fmt.Printf("  • Output Tokens: %d\n", s.OutputTokens)
			fmt.Printf("  • Total Cost:    %s$%.6f%s\n", ColorYellow, s.TotalCost, ColorReset)
			fmt.Printf("  • Cached Items:  %d in-memory\n", s.CacheEntries)
			fmt.Printf("  • Buffer Depth:  %d messages\n", s.HistoryDepth)
			return false
		},
	}

	commandRegistry["model"] = SlashCommand{
		Name:        "/model",
		Args:        "<name>",
		Description: "Inspect or switch active model (e.g. /model gpt-4o-mini)",
		Execute: func(engine *navi.Engine, args string) bool {
			if strings.TrimSpace(args) == "" {
				s := engine.GetStats()
				fmt.Printf("Current model: %s%s%s\n", ColorCyan, s.Model, ColorReset)
				return false
			}
			engine.SetModel(args)
			fmt.Printf("%s✔ Switched active model to %s%s\n", ColorGreen, args, ColorReset)
			return false
		},
	}

	commandRegistry["clearcache"] = SlashCommand{
		Name:        "/clearcache",
		Args:        "",
		Description: "Flush in-memory duplicate prompt cache",
		Execute: func(engine *navi.Engine, args string) bool {
			engine.ClearCache()
			fmt.Printf("%s✔ Local response cache purged.%s\n", ColorGreen, ColorReset)
			return false
		},
	}

	commandRegistry["history"] = SlashCommand{
		Name:        "/history",
		Args:        "",
		Description: "Inspect messages in current sliding context window",
		Execute: func(engine *navi.Engine, args string) bool {
			history := engine.GetHistory()
			fmt.Printf("\n%s📜 Current Context Window (%d messages):%s\n", ColorBold, len(history), ColorReset)
			for i, m := range history {
				roleColor := ColorCyan
				if m.Role == "user" {
					roleColor = ColorGreen
				} else if m.Role == "system" {
					roleColor = ColorPurple
				}
				content := strings.ReplaceAll(m.Content, "\n", " ")
				if len(content) > 60 {
					content = content[:57] + "..."
				}
				fmt.Printf("  [%d] %s%-9s%s: %s\n", i, roleColor, m.Role, ColorReset, content)
			}
			return false
		},
	}

	commandRegistry["exit"] = SlashCommand{
		Name:        "/exit",
		Args:        "",
		Description: "Close the Navi session",
		Execute: func(engine *navi.Engine, args string) bool {
			return true
		},
	}
}

func printCommandPreview(filter string) {
	fmt.Printf("\n%s┌── Slash Commands ──────────────────────────────────────────┐%s\n", ColorPurple, ColorReset)

	// Sort commands alphabetically
	var keys []string
	for k := range commandRegistry {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		cmd := commandRegistry[k]
		if filter != "" && !strings.HasPrefix(cmd.Name, filter) {
			continue
		}
		cmdSignature := fmt.Sprintf("%s %s", cmd.Name, cmd.Args)
		fmt.Printf("%s│%s  %s%-18s%s %-41s %s│%s\n",
			ColorPurple, ColorReset,
			ColorBold, cmdSignature, ColorReset,
			cmd.Description,
			ColorPurple, ColorReset)
	}
	fmt.Printf("%s└────────────────────────────────────────────────────────────┘%s\n", ColorPurple, ColorReset)
}

func main() {
	registerCommands()

	apiKey := os.Getenv("NAVI_API_KEY")
	baseURL := os.Getenv("NAVI_BASE_URL")
	model := os.Getenv("NAVI_MODEL")

	if baseURL == "" {
		baseURL = "https://api.groq.com/openai/v1"
	}
	if model == "" {
		model = "llama-3.1-8b-instant"
	}

	engine := navi.New(navi.Config{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		Model:      model,
		MaxHistory: 6,
	})

	printHeader(model, baseURL)

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Printf("\n%s%syou ›%s ", ColorBold, ColorGreen, ColorReset)
		if !scanner.Scan() {
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// Handle Slash Commands
		if strings.HasPrefix(input, "/") {
			shouldExit := handleSlashCommand(engine, input)
			if shouldExit {
				printFooter(engine.TotalCost())
				break
			}
			continue
		}

		// Standard LLM Chat Query
		fmt.Printf("%sThinking...%s\r", ColorDim, ColorReset)

		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		res, err := engine.Ask(ctx, input)
		cancel()

		// Clear thinking text
		fmt.Print("\r\033[K")

		if err != nil {
			fmt.Printf("%sError: %v%s\n", ColorRed, err, ColorReset)
			continue
		}

		fmt.Printf("%s%snavi ›%s %s\n", ColorBold, ColorCyan, ColorReset, res.Text)
		printMetaBar(res.Meta)
	}
}

func handleSlashCommand(engine *navi.Engine, input string) bool {
	cleanInput := strings.TrimPrefix(input, "/")
	parts := strings.SplitN(cleanInput, " ", 2)
	cmdKey := strings.ToLower(parts[0])
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	// Direct match for single "/" -> shows preview of all commands
	if cmdKey == "" {
		printCommandPreview("")
		return false
	}

	cmd, exists := commandRegistry[cmdKey]
	if exists {
		return cmd.Execute(engine, args)
	}

	// If unknown, show matching commands or complete list
	fmt.Printf("%sUnknown command: /%s%s\n", ColorRed, cmdKey, ColorReset)
	printCommandPreview("/" + cmdKey)
	return false
}

func printHeader(model, endpoint string) {
	fmt.Printf("%s┌────────────────────────────────────────────────────────┐%s\n", ColorCyan, ColorReset)
	fmt.Printf("%s│                 🧚 NAVI AI HARNESS                     │%s\n", ColorCyan, ColorReset)
	fmt.Printf("%s│%s   Model:    %-42s %s│%s\n", ColorCyan, ColorDim, model, ColorCyan, ColorReset)
	fmt.Printf("%s│%s   Endpoint: %-42s %s│%s\n", ColorCyan, ColorDim, truncate(endpoint, 42), ColorCyan, ColorReset)
	fmt.Printf("%s│%s   Type %s/%s or %s/help%s to view slash commands.          %s│%s\n",
		ColorCyan, ColorYellow, ColorBold, ColorReset+ColorDim, ColorBold, ColorReset+ColorDim, ColorCyan, ColorReset)
	fmt.Printf("%s└────────────────────────────────────────────────────────┘%s\n", ColorCyan, ColorReset)
}

func printMetaBar(m navi.ResponseMeta) {
	if m.Cached {
		fmt.Printf("%s⚡ [CACHE HIT] $0.000000 | Session Total: $%.6f | Latency: %v%s\n",
			ColorYellow, m.SessionCost, m.Duration.Round(time.Millisecond), ColorReset)
		return
	}

	fmt.Printf("%s[%d in / %d out] • Turn: $%.6f • Session: $%.6f • %v%s\n",
		ColorGray,
		m.PromptTokens,
		m.OutputTokens,
		m.TurnCost,
		m.SessionCost,
		m.Duration.Round(time.Millisecond),
		ColorReset,
	)
}

func printFooter(totalCost float64) {
	fmt.Printf("\n%sSession closed. Final spend: $%.6f%s\n", ColorYellow, totalCost, ColorReset)
}

func truncate(str string, max int) string {
	if len(str) > max {
		return str[:max-3] + "..."
	}
	return str
}
