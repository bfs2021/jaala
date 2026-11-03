package chakra

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
)

// served answers every chat as an OpenAI-compatible server would, with status and reply, keeping
// each request body decoded.
func served(t *testing.T, status int, reply string) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	var got []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("asked %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("not JSON: %v", err)
		}
		got = append(got, body)
		w.WriteHeader(status)
		io.WriteString(w, reply)
	}))
	t.Cleanup(s.Close)
	return s, &got
}

func llama(t *testing.T, url string) *Model {
	t.Helper()
	p, err := agent.NewLlamaCppProvider(agent.LlamaCppConfig{BaseURL: url + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return New("llama.cpp", p)
}

// The presets put on the wire what jaala's own clients did: llama-server gets the grammar,
// cache_prompt, temperature 0 and a vote's seed; Ollama the same but the grammar, which it can't
// read.
func TestThePresetsSendWhatTheServerReads(t *testing.T) {
	s, got := served(t, 200, `{"choices":[{"message":{"role":"assistant","content":"edge(?a, ?b) => ?a"}}],
		"usage":{"prompt_tokens":120,"completion_tokens":9,"prompt_tokens_details":{"cached_tokens":100}}}`)
	drawn := prompt
	drawn.Draw = 2
	out, err := llama(t, s.URL).Complete(context.Background(), drawn)
	if err != nil {
		t.Fatal(err)
	}
	if out != (nl.Completion{Text: "edge(?a, ?b) => ?a", TokensIn: 120, TokensOut: 9, TokensCached: 100}) {
		t.Errorf("completion %+v", out)
	}
	body := (*got)[0]
	if body["grammar"] != prompt.Grammar || body["cache_prompt"] != true || body["seed"] != 2.0 || body["max_tokens"] != 512.0 {
		t.Errorf("llama-server was sent %v", body)
	}
	if temp, sent := body["temperature"]; !sent || temp != 0.0 {
		t.Errorf("temperature %v (sent %v): llama-server sent none samples at 0.8", temp, sent)
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 4 || msgs[0].(map[string]any)["role"] != "system" || msgs[0].(map[string]any)["content"] != "the card" {
		t.Errorf("messages %v", msgs)
	}

	o, err := agent.NewOllamaProvider(agent.OllamaConfig{BaseURL: s.URL + "/v1", Model: "qwen2.5-coder:7b"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New("ollama:qwen2.5-coder:7b", o).Complete(context.Background(), drawn); err != nil {
		t.Fatalf("ollama refused a prompt carrying a grammar: %v", err)
	}
	body = (*got)[1]
	for _, k := range []string{"grammar", "response_format", "cache_prompt"} {
		if _, sent := body[k]; sent {
			t.Errorf("ollama was sent %s: %v", k, body)
		}
	}
	if body["model"] != "qwen2.5-coder:7b" || body["seed"] != 2.0 || body["temperature"] != 0.0 {
		t.Errorf("ollama was sent %v", body)
	}
}

func TestThePresetsReportCachedTokensFromTimings(t *testing.T) {
	// usage without prompt_tokens_details, as llama.cpp builds before it sent one: the timings say.
	s, got := served(t, 200, `{"choices":[{"message":{"content":"x"}}],"usage":{"prompt_tokens":9,"completion_tokens":1},"timings":{"cache_n":7}}`)
	p := prompt
	p.Grammar = ""
	out, err := llama(t, s.URL).Complete(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if _, sent := (*got)[0]["grammar"]; sent || out.TokensCached != 7 {
		t.Errorf("grammar sent: %v, cached %d (want the timings' 7)", sent, out.TokensCached)
	}
}

func TestThePresetsReportWhatTheServerRefused(t *testing.T) {
	s, _ := served(t, 400, `{"error":{"message":"failed to parse grammar"}}`)
	_, err := llama(t, s.URL).Complete(context.Background(), prompt)
	if err == nil || !strings.Contains(err.Error(), "400") || !strings.Contains(err.Error(), "failed to parse grammar") {
		t.Fatalf("err %v", err)
	}
	s2, _ := served(t, 200, `{"choices":[]}`)
	if _, err := llama(t, s2.URL).Complete(context.Background(), prompt); err == nil {
		t.Fatal("no choices answered without an error")
	}
}

func TestThePresetsStopWhenCancelled(t *testing.T) {
	block := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(func() { close(block); s.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := llama(t, s.URL).Complete(ctx, prompt); err == nil || ctx.Err() == nil {
		t.Fatalf("err %v", err)
	}
}
