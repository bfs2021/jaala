package translator_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/frontends/nlmodels/translator"
)

// served answers every chat as an OpenAI-compatible server streams one, with reply and its usage
// (120 tokens in, 100 of them cached, 9 out), keeping each request body decoded.
func served(t *testing.T, reply string) (*httptest.Server, func() []map[string]any) {
	t.Helper()
	var mu sync.Mutex
	var got []map[string]any
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("asked %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("not JSON: %v", err)
		}
		mu.Lock()
		got = append(got, body)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for _, chunk := range []any{
			map[string]any{"choices": []any{map[string]any{"delta": map[string]string{"content": reply}, "finish_reason": "stop"}}},
			map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 120, "completion_tokens": 9,
				"prompt_tokens_details": map[string]int{"cached_tokens": 100}}},
		} {
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(s.Close)
	return s, func() []map[string]any { mu.Lock(); defer mu.Unlock(); return got }
}

// chakra's presets put on the wire what jaala's own clients did, through the translator: llama-server
// gets the card as the system message, the grammar, cache_prompt, temperature 0 and 512 tokens, and
// each of a vote's draws its own seed; Ollama the same but the grammar, which it can't read. A try
// reports the server's tokens, its cached ones included.
func TestThePresetsSendWhatTheServerReads(t *testing.T) {
	v := vocabulary(t)
	s, got := served(t, `edge(?a, ?b) => ?a`)
	p, err := agent.NewLlamaCppProvider(agent.LlamaCppConfig{BaseURL: s.URL + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	tr, err := translator.New(v, nil, translator.Config{Model: translator.Role{Provider: p, Label: "llama.cpp"}, Vote: &nl.Vote{Drafts: 3}})
	if err != nil {
		t.Fatal(err)
	}
	_, trace, err := tr.Ask(context.Background(), "what links to what?")
	if err != nil {
		t.Fatal(err)
	}
	if try := trace.Tries[0]; try.TokensIn != 120 || try.TokensOut != 9 || try.TokensCached != 100 {
		t.Errorf("try %+v", try)
	}
	bodies := got()
	if len(bodies) != 3 {
		t.Fatalf("%d requests, want the answer and two draws", len(bodies))
	}
	first := bodies[0]
	if g, _ := first["grammar"].(string); g == "" || first["cache_prompt"] != true || first["max_tokens"] != 512.0 {
		t.Errorf("llama-server was sent %v", first)
	}
	if temp, sent := first["temperature"]; !sent || temp != 0.0 {
		t.Errorf("temperature %v (sent %v): llama-server sent none samples at 0.8", temp, sent)
	}
	if _, sent := first["seed"]; sent {
		t.Errorf("the answer was sent a seed: %v", first["seed"])
	}
	msgs, _ := first["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("messages %v", msgs)
	}
	if bodies[1]["seed"] != 0.0 || bodies[2]["seed"] != 1.0 {
		t.Errorf("the draws' seeds %v and %v", bodies[1]["seed"], bodies[2]["seed"])
	}

	o, err := agent.NewOllamaProvider(agent.OllamaConfig{BaseURL: s.URL + "/v1", Model: "qwen2.5-coder:7b"})
	if err != nil {
		t.Fatal(err)
	}
	tr, err = translator.New(v, nil, translator.Config{Model: translator.Role{Provider: o}, Vote: &nl.Vote{Drafts: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tr.Ask(context.Background(), "what links to what?"); err != nil {
		t.Fatalf("ollama: %v", err)
	}
	ollama := got()[3:]
	for _, k := range []string{"grammar", "response_format", "cache_prompt"} {
		if _, sent := ollama[0][k]; sent {
			t.Errorf("ollama was sent %s: %v", k, ollama[0])
		}
	}
	if ollama[0]["model"] != "qwen2.5-coder:7b" || ollama[0]["temperature"] != 0.0 || ollama[1]["seed"] != 0.0 {
		t.Errorf("ollama was sent %v, then %v", ollama[0], ollama[1])
	}
}
