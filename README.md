<!-- Official Navi Brand Icon: Concept 3 (The Wayfinder Spark) -->
<p align="center">
  <svg class="brand-icon" width="80" height="80" viewBox="0 0 100 100" fill="none" xmlns="http://www.w3.org/2000/svg">
    <defs>
      <linearGradient id="naviSparkGrad" x1="0%" y1="0%" x2="100%" y2="100%">
        <stop offset="0%" stop-color="#00e5ff"/>
        <stop offset="50%" stop-color="#ffffff"/>
        <stop offset="100%" stop-color="#9d4edd"/>
      </linearGradient>
      <radialGradient id="sparkHalo" cx="50%" cy="50%" r="50%">
        <stop offset="0%" stop-color="#00e5ff" stop-opacity="0.5"/>
        <stop offset="100%" stop-color="#00e5ff" stop-opacity="0"/>
      </radialGradient>
    </defs>
    <circle cx="50" cy="50" r="46" fill="url(#sparkHalo)"/>
    <path d="M50 10 Q50 50 10 50 Q50 50 50 90 Q50 50 90 50 Q50 50 50 10 Z" fill="url(#naviSparkGrad)"/>
    <circle cx="50" cy="50" r="7.5" fill="#0b0d13" stroke="#00e5ff" stroke-width="2.5"/>
    <circle cx="28" cy="28" r="3" fill="#00e5ff" opacity="0.8"/>
    <circle cx="72" cy="72" r="3" fill="#9d4edd" opacity="0.8"/>
  </svg>
</p>

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
