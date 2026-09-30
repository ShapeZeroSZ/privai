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
	"github.com/charmbracelet/bubbles/viewport"
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

// Rows used by everything except the chat viewport: header, status line and
// the bordered input box.
const (
	headerHeight = 1
	statusHeight = 1
	inputHeight  = 3
	chromeHeight = headerHeight + statusHeight + inputHeight
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true)

	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#00FFFF")).
			Bold(true).
			Italic(true).
			Align(lipgloss.Right)

	aiStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#AAFF00")).
		Margin(1, 2)

	errStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000"))
	hintStyle = lipgloss.NewStyle().Faint(true)

	inputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#FFFFFF"))
)

type model struct {
	messages []message
	input    textinput.Model
	chat     viewport.Model
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

	m := model{
		input:    ti,
		messages: []message{},
		sending:  false,
		chat:     viewport.New(80, 20-chromeHeight),
		height:   20,
		width:    80,
	}
	m.refreshChat(true)
	return m
}

// refreshChat re-renders the conversation into the viewport. It keeps the
// view pinned to the newest text if it was already at the bottom (or if
// forced), and otherwise leaves the user's scroll position alone.
func (m *model) refreshChat(forceBottom bool) {
	atBottom := m.chat.AtBottom()
	m.chat.SetContent(m.renderMessages())
	if forceBottom || atBottom {
		m.chat.GotoBottom()
	}
}

func (m model) renderMessages() string {
	if len(m.messages) == 0 {
		return hintStyle.Render("Ask about the code in this directory. Replies stay on this machine.")
	}

	w := m.chat.Width
	parts := make([]string, 0, len(m.messages))
	for _, msg := range m.messages {
		if msg.user {
			parts = append(parts, userStyle.Width(w).Render(msg.text))
		} else if text := strings.TrimRight(msg.text, "\n"); text != "" {
			// Width covers the text only; the 2-column margins sit outside it.
			parts = append(parts, aiStyle.Width(max(w-4, 1)).Render(text))
		}
	}
	return strings.Join(parts, "\n")
}

// isScrollKey reports keys that scroll the chat even while typing. The
// text input doesn't use them, so there's no conflict.
func isScrollKey(k string) bool {
	switch k {
	case "up", "down", "pgup", "pgdown":
		return true
	}
	return false
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
		m.chat.Width = msg.Width
		m.chat.Height = max(msg.Height-chromeHeight, 1)
		// Border takes 2 columns; leave 1 for the prompt's cursor.
		m.input.Width = max(msg.Width-2-len(m.input.Prompt)-1, 1)
		m.refreshChat(false)
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
			m.refreshChat(true)
			go streamChat(buildRequest(history, input), m.stream)
			return m, waitForStream(m.stream)
		case "esc":
			m.input.Blur()
			return m, nil
		case "tab":
			return m, m.input.Focus()
		}
		// While typing, only the scroll keys reach the chat; once the input
		// is blurred (Esc), the viewport's full key map applies.
		if !m.input.Focused() || isScrollKey(msg.String()) {
			m.chat, cmd = m.chat.Update(msg)
			return m, cmd
		}
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case chunkMsg:
		m.messages[len(m.messages)-1].text += string(msg)
		m.refreshChat(false)
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
		m.refreshChat(false)
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

	header := headerStyle.Render("--- PrivAI Dev Agent ---")

	var status string
	switch {
	case m.sending && m.messages[len(m.messages)-1].text == "":
		status = "AI is thinking..."
	case m.err != "":
		status = errStyle.Render(m.err)
	case !m.chat.AtBottom():
		status = hintStyle.Render(fmt.Sprintf("%3.f%% · ↓/PgDn for newer messages", m.chat.ScrollPercent()*100))
	case !m.input.Focused():
		status = hintStyle.Render("Scrolling: j/k ↑/↓ PgUp/PgDn · Tab to type · q to quit")
	}
	status = lipgloss.NewStyle().MaxWidth(m.width).Render(status)

	input := inputStyle.Width(max(m.width-2, 1)).Render(m.input.View())

	return lipgloss.JoinVertical(lipgloss.Left, header, m.chat.View(), status, input)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Alas, there's been an error: %v", err)
		os.Exit(1)
	}
}
