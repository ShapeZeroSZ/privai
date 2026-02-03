package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/bubbles/textinput"
)

const (
	ollamaURL = "http://localhost:11434/api/chat"
	defaultModel = "llama3.2"
)

type message struct {
	text string
	user bool
}

type model struct {
	messages   []message
	input      textinput.Model
	sending    bool
	err        string
	height     int
	width      int
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Ask the AI dev agent..."
	ti.Focus()

	return model{
		input:     ti,
		messages:  []message{},
		sending:   false,
		height:    20,
		width:     80,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter":
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				return m, nil
			}
			// Add user message
			m.messages = append(m.messages, message{text: input, user: true})
			m.input.Reset()
			m.sending = true
			m.err = ""
			return m, sendMessage(input)
		case "esc":
			m.input.Blur()
		case "tab":
			m.input.Focus()
		}
		// Update input
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case resultMsg:
		m.sending = false
		if msg.err != "" {
			m.err = msg.err
			m.messages = append(m.messages, message{text: "Error: " + msg.err, user: false})
		} else {
			m.messages = append(m.messages, message{text: msg.content, user: false})
		}
		return m, nil

	}

	return m, nil
}

type resultMsg struct {
	content string
	err     string
}

func sendMessage(userInput string) tea.Cmd {
	return func() tea.Msg {
		ctx := getContext()
		fullPrompt := ctx + "\n\nUser: " + userInput + "\nAI:"

		body := map[string]interface{}{
			"model":  defaultModel,
			"messages": []map[string]string{
				{"role": "system", "content": "You are a privacy-first AI dev agent. Help with code, files, git. Suggest commands in `shell: command`."},
				{"role": "user", "content": fullPrompt},
			},
			"stream": false,
		}

		jsonData, err := json.Marshal(body)
		if err != nil {
			return resultMsg{err: err.Error()}
		}

		resp, err := http.Post(ollamaURL, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return resultMsg{err: err.Error()}
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return resultMsg{err: fmt.Sprintf("HTTP %d", resp.StatusCode)}
		}

		var response struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
			body, _ := io.ReadAll(resp.Body)
			return resultMsg{err: fmt.Sprintf("Decode error: %v body: %s", err, string(body))}
		}

		return resultMsg{content: response.Message.Content}
	}
}

func getContext() string {
	var ctx strings.Builder

	// Files
	cmd := exec.Command("sh", "-c", "ls -la . | head -20")
	out, err := cmd.Output()
	if err == nil {
		ctx.WriteString("Current dir files:\n")
		ctx.Write(out)
	} else {
		ctx.WriteString("ls failed\n")
	}

	// Git
	if _, err := os.Stat(".git"); err == nil {
		gcmd := exec.Command("git", "status", "--short")
		gout, _ := gcmd.Output()
		ctx.WriteString("\nGit status:\n")
		ctx.Write(gout)
		cmd = exec.Command("git", "log", "--oneline", "-5")
		out, _ = cmd.Output()
		ctx.WriteString("Recent commits:\n")
		ctx.Write(out)
	}

	return ctx.String()
}

func (m model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	var b strings.Builder

	userStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#00FFFF")).
		Bold(true).
		Italic(true).
		Align(lipgloss.Right)

	aiStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AAFF00")).
		Bold(false).
		Margin(1, 2)

	errStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000"))

	chatLines := (m.height - 5) / 2
	numMsg := min(len(m.messages), chatLines)
	visibleMsgs := m.messages[len(m.messages)-numMsg:]

	b.WriteString("--- PrivAI Dev Agent ---")
	b.WriteString("\n")

	for i, msg := range visibleMsgs {
		if i > 0 {
			b.WriteString("\n")
		}
		if msg.user {
			b.WriteString(userStyle.Render(msg.text))
		} else {
			b.WriteString(aiStyle.Render(msg.text))
		}
	}

	if m.sending {
		b.WriteString("\n\nAI is thinking...")
	}

	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render(m.err))
	}

	inputStyle := lipgloss.NewStyle().
		Height(1).
		MarginTop(1).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#FFFFFF"))
	input := inputStyle.Render(m.input.View())

	b.WriteString(input)

	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Render(b.String())
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}