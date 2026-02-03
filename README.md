# PrivAI - Privacy-First TUI Dev Agent

A terminal-based chat interface for Ollama, tailored for development workflows. Provides local context (files, git status) to the AI.

## Features
- TUI chat with Ollama (default: llama3.2)
- Automatic context injection: `ls -la | head -20`, git status, recent commits
- Responsive to terminal resize
- Auto-scroll to recent messages
- Privacy-first: local LLM only

## Prerequisites
- Ollama installed and running (`ollama serve`)
- Model: `ollama pull llama3.2` (or edit defaultModel)

## Installation
```bash
go mod init privai
go mod tidy
go build -o privai
sudo cp privai /usr/local/bin/
```

## Usage
```
./privai
```

- Type your query and press Enter
- q or Ctrl+C to quit
- Tab/Esc for input focus

## Context Sent to AI
- Current directory listing
- Git status (if .git exists)
- Last 5 commits

AI is prompted as: \"You are a privacy-first AI dev agent. Help with code, files, git. Suggest commands in `shell: command`.\"

## Customization
- Edit `ollamaURL`, `defaultModel` in main.go
- Modify system prompt

## Completions
Bash: source completions/privai.bash

## Man Page
man privai

## Build & Install as OpenClaw Skill
See packaging instructions.