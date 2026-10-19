<!-- Official Navi Brand Icon: Concept 3 (The Wayfinder Spark) -->
<!-- GitHub strips inline <svg> from READMEs; it only renders SVG referenced via <img>. -->
<p align="center">
  <img src="assets/navi-icon.svg" width="80" height="80" alt="Navi — The Wayfinder Spark">
</p>

# Navi

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
| `/key` | `[api_key]` | Inspect or set the API key for this session |
| `/baseurl` | `[url]` | Inspect or set the endpoint (e.g. an Ollama server) |
| `/history` | — | Inspect current sliding context memory |
| `/exit` | — | Quit session and view total cost |

---

## 📦 Installation

```bash
git clone https://github.com/JohnEsleyer/navi.git
cd navi
./install.sh
```

This builds and installs `navi` to `~/.local/bin`. On a terminal you are then
asked whether to include the desktop GUI; non-interactive runs (CI, pipes)
build it best-effort instead of prompting. Then:

```bash
export NAVI_API_KEY=your_key_here
navi
```

| Flag | Effect |
|---|---|
| `--with-gui` | Include the desktop GUI |
| `--no-gui` | Skip the desktop GUI |
| `--prefix DIR` | Install to `DIR` instead of `~/.local/bin` |
| `--system` | Install to `/usr/local/bin` |
| `--uninstall` | Remove `navi` and `navi-gui` from the target directory |
| `--help` | Usage |

The GUI needs cgo plus GTK/WebKit headers. The installer probes for them
before asking, so the prompt warns up front if they are absent, and a failed
GUI build never blocks the CLI. On Debian/Ubuntu:

```bash
sudo apt install build-essential libgtk-3-dev libwebkit2gtk-4.1-dev
```

---

## 🖥️ Desktop GUI (Built with Wails)

Launch directly from the CLI with `/gui`, run independently with plain Go, or use the Wails CLI from the GUI package directory:

```bash
# Production build without the Wails CLI (assets are embedded)
go build -tags production ./gui

# Or use the Wails CLI (hot-reload dev server / packaged build)
cd gui
wails dev      # development with hot reload
wails build    # production binary → gui/build/bin/navi-gui
```

### GUI Features
* **Aesthetic Dark Theme**: Cyan & deep obsidian design.
* **Puppet Mode ("Pretend to be AI")**: Inject custom AI completions directly into history without making LLM API calls—useful for testing downstream agent workflows.
* **Custom UI Components**: All alerts, modals, dropdowns, and toasts are custom built (no native browser popups).
* **1-Click Copy & Dump**: Instant clipboard copy of the context buffer and `.txt` file dumping.
