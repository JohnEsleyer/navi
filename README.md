# 🧚 Navi

> *"Hey! Listen!"*

Navi is a lightweight, cost-optimized AI harness written in Go with **zero external dependencies**. It wraps any OpenAI-compatible provider, prioritizing minimum spend, fast cache hits, and zero bloat.

---

## ⚡ Built-in Slash Commands

Type `/` or `/help` inside the CLI to preview all available commands:

| Command | Arguments | Description |
|---|---|---|
| `/help` | — | Display the command preview panel |
| `/clear` | — | Flush context memory to zero out upcoming input token fees |
| `/stats` | — | Print exact session tokens, cumulative cost, and memory usage |
| `/model` | `<name>` | Show or dynamically change the active model (e.g. `/model gpt-4o-mini`) |
| `/history` | — | Inspect all messages inside the active sliding context buffer |
| `/clearcache` | — | Invalidate the in-memory exact-match response cache |
| `/exit` | — | Quit Navi and view final cost summary |

---

## 🚀 Quick Run

```bash
# Using free Groq tier
export NAVI_API_KEY="gsk_..."
export NAVI_BASE_URL="https://api.groq.com/openai/v1"
export NAVI_MODEL="llama-3.1-8b-instant"

go run ./cmd/navi
```

Simply type `/` at the prompt:

```text
you › /

┌── Slash Commands ──────────────────────────────────────────┐
│  /clear             Reset conversation history to start    │
│  /clearcache        Flush in-memory duplicate prompt cache │
│  /exit              Close the Navi session                 │
│  /help              Display available slash commands       │
│  /history           Inspect current sliding context window │
│  /model <name>      Inspect or switch active model         │
│  /stats             View cumulative session cost and stats │
└────────────────────────────────────────────────────────────┘
```
