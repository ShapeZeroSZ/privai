package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestQDoesNotQuitWhileTyping(t *testing.T) {
	m := initialModel()
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd != nil {
		if _, ok := cmd().(tea.QuitMsg); ok {
			t.Fatal("typing q quit the app")
		}
	}
	if got := next.(model).input.Value(); got != "q" {
		t.Fatalf("input = %q, want %q", got, "q")
	}

	m.input.Blur()
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q with input blurred should quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q with input blurred should quit")
	}
}

func TestBuildRequestIncludesHistory(t *testing.T) {
	history := []message{{text: "hi", user: true}, {text: "hello", user: false}}
	msgs := buildRequest(history, "next")["messages"].([]map[string]string)
	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4", len(msgs))
	}
	want := []string{"system", "user", "assistant", "user"}
	for i, r := range want {
		if msgs[i]["role"] != r {
			t.Errorf("msgs[%d].role = %q, want %q", i, msgs[i]["role"], r)
		}
	}
}

func TestStreamChatSendsChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"message":{"content":"Hel"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":"lo"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":""},"done":true}`)
	}))
	defer srv.Close()
	old := ollamaURL
	ollamaURL = srv.URL
	defer func() { ollamaURL = old }()

	ch := make(chan tea.Msg)
	go streamChat(buildRequest(nil, "x"), ch)

	var got string
	for msg := range ch {
		switch msg := msg.(type) {
		case chunkMsg:
			got += string(msg)
		case doneMsg:
			if msg.err != "" {
				t.Fatalf("unexpected error: %s", msg.err)
			}
			if got != "Hello" {
				t.Fatalf("got %q, want %q", got, "Hello")
			}
			return
		}
	}
}

func TestEnterStreamsReplyIntoChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"message":{"content":"a"},"done":false}`)
		fmt.Fprintln(w, `{"message":{"content":"b"},"done":true}`)
	}))
	defer srv.Close()
	old := ollamaURL
	ollamaURL = srv.URL
	defer func() { ollamaURL = old }()

	var m tea.Model = initialModel()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for m.(model).sending {
		m, cmd = m.Update(cmd())
	}

	msgs := m.(model).messages
	if len(msgs) != 2 || !msgs[0].user || msgs[0].text != "hi" || msgs[1].text != "ab" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
}

func longReply(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "reply line %d\n", i)
	}
	return b.String()
}

func TestViewFitsTerminalAndFollowsNewText(t *testing.T) {
	var m tea.Model = initialModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 15})

	mm := m.(model)
	mm.messages = []message{{text: "q", user: true}, {text: "", user: false}}
	mm.sending = true
	m = mm
	m, _ = m.Update(chunkMsg(longReply(40)))

	view := m.(model).View()
	if got := strings.Count(view, "\n") + 1; got != 15 {
		t.Fatalf("view is %d lines, want 15", got)
	}
	if !strings.Contains(view, "reply line 40") {
		t.Fatal("newest text not visible while following the reply")
	}
	if strings.Contains(view, "reply line 1\n") {
		t.Fatal("oldest text should have scrolled out of view")
	}
}

func TestScrolledUpPositionSurvivesNewText(t *testing.T) {
	var m tea.Model = initialModel()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 50, Height: 15})
	mm := m.(model)
	mm.messages = []message{{text: "q", user: true}, {text: longReply(40), user: false}}
	mm.sending = true
	mm.refreshChat(true)
	m = mm

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	before := m.(model).chat.YOffset
	m, _ = m.Update(chunkMsg("more text\n"))
	if got := m.(model).chat.YOffset; got != before {
		t.Fatalf("YOffset moved from %d to %d while scrolled up", before, got)
	}
	if !strings.Contains(m.(model).View(), "newer messages") {
		t.Fatal("expected a hint that newer messages are below")
	}

	// Typing still goes to the input while the chat is scrolled.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	if got := m.(model).input.Value(); got != "j" {
		t.Fatalf("input = %q, want %q", got, "j")
	}
}
