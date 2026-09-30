package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	ollamaURL    string
	defaultModel string
)

func init() {
	ollamaBase := os.Getenv("OLLAMA_BASE_URL")
	if ollamaBase == "" {
		ollamaBase = "http://localhost:11434"
	}
	ollamaURL = ollamaBase + "/api/chat"

	defaultModel = os.Getenv("OLLAMA_MODEL")
	if defaultModel == "" {
		defaultModel = "llama3.2"
	}
}

const systemPrompt = "You are a privacy-first AI dev agent. Help with code, files, git. Suggest commands in `shell: command`."

type message struct {
	text string
	user bool
}

type model struct {
	messages []message
	input    textinput.Model
	sending  bool
	stream   chan tea.Msg
	err      string
	height   int
	width    int
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Ask the AI dev agent..."
	ti.Focus()

	return model{
		input:    ti,
		messages: []message{},
		sending:  false,
		height:   20,
		width:    80,
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
		case "ctrl+c":
			return m, tea.Quit
		case "q":
			// Only quit when not typing, so messages can contain "q".
			if !m.input.Focused() {
				return m, tea.Quit
			}
		case "enter":
			input := strings.TrimSpace(m.input.Value())
			if input == "" || m.sending {
				return m, nil
			}
			history := m.messages
			m.messages = append(m.messages, message{text: input, user: true})
			// Placeholder for the streamed reply.
			m.messages = append(m.messages, message{text: "", user: false})
			m.input.Reset()
			m.sending = true
			m.err = ""
			m.stream = make(chan tea.Msg)
			go streamChat(buildRequest(history, input), m.stream)
			return m, waitForStream(m.stream)
		case "esc":
			m.input.Blur()
		case "tab":
			m.input.Focus()
		}
		// Update input
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case chunkMsg:
		m.messages[len(m.messages)-1].text += string(msg)
		return m, waitForStream(m.stream)

	case doneMsg:
		m.sending = false
		m.stream = nil
		if msg.err != "" {
			m.err = msg.err
			last := &m.messages[len(m.messages)-1]
			if last.text == "" {
				last.text = "Error: " + msg.err
			}
		}
		return m, nil

	}

	return m, nil
}

// chunkMsg carries a piece of a streamed reply; doneMsg ends the stream.
type chunkMsg string

type doneMsg struct {
	err string
}

func waitForStream(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-ch
	}
}

// buildRequest sends the prior conversation plus the new input, with fresh
// workspace context attached to the new input only.
func buildRequest(history []message, userInput string) map[string]interface{} {
	msgs := []map[string]string{{"role": "system", "content": systemPrompt}}
	for _, h := range history {
		if h.text == "" {
			continue
		}
		role := "assistant"
		if h.user {
			role = "user"
		}
		msgs = append(msgs, map[string]string{"role": role, "content": h.text})
	}
	msgs = append(msgs, map[string]string{"role": "user", "content": getContext() + "\n\nUser: " + userInput})

	return map[string]interface{}{
		"model":    defaultModel,
		"messages": msgs,
		"stream":   true,
	}
}

func streamChat(body map[string]interface{}, ch chan<- tea.Msg) {
	jsonData, err := json.Marshal(body)
	if err != nil {
		ch <- doneMsg{err: err.Error()}
		return
	}

	resp, err := http.Post(ollamaURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		ch <- doneMsg{err: err.Error()}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ch <- doneMsg{err: fmt.Sprintf("HTTP %d", resp.StatusCode)}
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var line struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Done  bool   `json:"done"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			continue
		}
		if line.Error != "" {
			ch <- doneMsg{err: line.Error}
			return
		}
		if line.Message.Content != "" {
			ch <- chunkMsg(line.Message.Content)
		}
		if line.Done {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		ch <- doneMsg{err: err.Error()}
		return
	}
	ch <- doneMsg{}
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

	if m.sending && m.messages[len(m.messages)-1].text == "" {
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
