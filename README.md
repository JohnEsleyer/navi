# 🧚 Navi

> *"Hey! Listen!"*

Navi is a cost-optimized, low-latency AI harness written in Go. The core engine (`pkg/navi`) uses only the standard library; the optional desktop GUI is built with Wails. It works across Groq, DeepSeek, OpenAI, or local Ollama instances.

---

## ⚡ Built-in Slash Commands

| Command | Arguments | Description |
|---|---|---|
| `/help` | — | Display slash commands preview |
| `/dump` | — | Export context window to a `.txt` file in current directory |
| `/gui` | — | Launch the aesthetic dark-mode desktop GUI |
| `/clear` | — | Reset context window to drop token consumption |
| `/stats` | — | View cumulative session spend, tokens, and cache count |
| `/model` | `<name>` | Inspect or switch model on the fly |
| `/history` | — | Inspect current sliding context memory |
| `/exit` | — | Quit session and view total cost |

---

## 🖥️ Desktop GUI (Built with Wails)

Launch directly from the CLI with `/gui`, run independently with plain Go, or use the Wails CLI from the GUI package directory:

```bash
# Production build without the Wails CLI (assets are embedded)
go build -tags production ./cmd/navi-gui

# Or use the Wails CLI (hot-reload dev server / packaged build)
cd cmd/navi-gui
wails dev      # development with hot reload
wails build    # production binary → cmd/navi-gui/build/bin/navi-gui
```

### GUI Features
* **Aesthetic Dark Theme**: Cyan & deep obsidian design.
* **Puppet Mode ("Pretend to be AI")**: Inject custom AI completions directly into history without making LLM API calls—useful for testing downstream agent workflows.
* **Custom UI Components**: All alerts, modals, dropdowns, and toasts are custom built (no native browser popups).
* **1-Click Copy & Dump**: Instant clipboard copy of the context buffer and `.txt` file dumping.
