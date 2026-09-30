# PrivAI - Privacy-First TUI Dev Agent

A terminal chat interface for [Ollama](https://ollama.com), tailored for development work. Each question you ask is sent along with a snapshot of the current directory and git state, so the model can answer about the code in front of you. Everything runs against your local Ollama server; nothing leaves your machine.

## Features
- Chat with a local Ollama model (default: `llama3.2`)
- Replies stream in as they are generated
- Remembers the conversation for the whole session
- Scrollable chat history that follows new text, and stays put when you scroll up to reread
- Fresh workspace context with every question: directory listing, `git status`, last 5 commits
- Adapts to terminal resizes

## Prerequisites
- Go 1.24+ to build
- Ollama installed and running (`ollama serve`)
- A model pulled: `ollama pull llama3.2` (or set `OLLAMA_MODEL`)

## Installation
```bash
make install   # builds ./privai, installs it to /usr/local/bin and the man page
```

Or just build it: `make build` (or `go build -o privai .`).

## Usage
Run `privai` from the project directory you want help with.

| Key | Action |
|-----|--------|
| Enter | Send message |
| ↑ / ↓, PgUp / PgDn | Scroll the chat (works while typing) |
| Esc | Leave the input box; then j/k, space/b, u/d also scroll |
| Tab | Back to the input box |
| q | Quit (when not typing) |
| Ctrl+C | Quit |

A status line under the chat shows when the AI is thinking, any error, and how far up you have scrolled.

## Configuration
| Variable | Default |
|----------|---------|
| `OLLAMA_BASE_URL` | `http://localhost:11434` |
| `OLLAMA_MODEL` | `llama3.2` |

```bash
OLLAMA_MODEL=qwen2.5-coder privai
```

## Context sent to the model
With each new question (earlier turns are sent as plain conversation history):
- The first 20 lines of `ls -la`
- `git status --short` and `git log --oneline -5`, if the directory has a `.git`

The system prompt is: "You are a privacy-first AI dev agent. Help with code, files, git. Suggest commands in `shell: command`." PrivAI only displays suggested commands; it never runs them. To change the prompt, edit `systemPrompt` in `main.go`.

## Development
```bash
make test
```

## Completions
Bash: `source completions/privai.bash` (PrivAI takes no arguments, so this only registers the command name).

## Man page
`man privai` (installed by `make install`).

## License
MIT
