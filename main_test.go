package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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
