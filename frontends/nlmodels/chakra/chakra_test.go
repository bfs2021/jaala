package chakra

import (
	"context"
	"errors"
	"testing"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
)

// recorder is a Provider answering every Generate with res, or err, keeping each request and
// reporting caps as what it can send.
type recorder struct {
	res  agent.ProviderResponse
	err  error
	caps agent.Capabilities
	reqs []agent.ProviderRequest
}

func (r *recorder) Generate(_ context.Context, req agent.ProviderRequest) (*agent.ProviderResponse, error) {
	r.reqs = append(r.reqs, req)
	if r.err != nil {
		return nil, r.err
	}
	res := r.res
	return &res, nil
}

func (r *recorder) Stream(context.Context, agent.ProviderRequest) (agent.Stream, error) {
	return nil, errors.New("not streamed")
}

func (r *recorder) ProviderCapabilities() agent.Capabilities { return r.caps }

var prompt = nl.Prompt{
	Prefix:  "the card",
	Turns:   []nl.Turn{{Role: nl.RoleUser, Text: "q"}, {Role: nl.RoleModel, Text: "draft"}, {Role: nl.RoleUser, Text: "fix it"}},
	Grammar: `root ::= "x"`,
}

func TestCompleteSendsThePromptAsOneCall(t *testing.T) {
	r := &recorder{res: agent.ProviderResponse{Text: "edge(?a, ?b) => ?a",
		Usage: &agent.Usage{InputTokens: 120, OutputTokens: 9, CachedInputTokens: 100}}}
	m := New("qwen", r)
	out, err := m.Complete(context.Background(), prompt)
	if err != nil {
		t.Fatal(err)
	}
	if out != (nl.Completion{Text: "edge(?a, ?b) => ?a", TokensIn: 120, TokensOut: 9, TokensCached: 100}) {
		t.Errorf("completion %+v", out)
	}
	req := r.reqs[0]
	want := []agent.Message{{Role: agent.RoleUser, Text: "q"}, {Role: agent.RoleAssistant, Text: "draft"}, {Role: agent.RoleUser, Text: "fix it"}}
	if req.Instructions != "the card" || len(req.Messages) != len(want) {
		t.Fatalf("request %+v", req)
	}
	for i := range want {
		if req.Messages[i].Role != want[i].Role || req.Messages[i].Text != want[i].Text {
			t.Errorf("message %d: %+v, want %+v", i, req.Messages[i], want[i])
		}
	}
	if req.MaxTokens != 512 || req.Temperature == nil || *req.Temperature != 0 {
		t.Errorf("New's model sent max tokens %d, temperature %v, want 512 at 0", req.MaxTokens, req.Temperature)
	}
	if m.Name() != "qwen" {
		t.Errorf("name %q", m.Name())
	}
}

func TestCompleteSendsWhatTheProviderCanSend(t *testing.T) {
	drawn := prompt
	drawn.Draw = 1 // a vote's draws count from 1
	plain := prompt
	plain.Grammar = ""
	for _, c := range []struct {
		name          string
		caps          agent.Capabilities
		grammar, seed bool
	}{
		{"llama.cpp", agent.Capabilities{Guided: []string{agent.GuidedGrammar, agent.GuidedJSONSchema}, Seed: true}, true, true},
		{"ollama", agent.Capabilities{Guided: []string{agent.GuidedJSONSchema}, Seed: true}, false, true},
		{"anthropic", agent.Capabilities{}, false, false},
	} {
		r := &recorder{res: agent.ProviderResponse{Text: "x"}, caps: c.caps}
		m := New(c.name, r)
		for _, p := range []nl.Prompt{prompt, drawn, plain} {
			if _, err := m.Complete(context.Background(), p); err != nil {
				t.Fatal(err)
			}
		}
		first, second, third := r.reqs[0], r.reqs[1], r.reqs[2]
		if sent := first.Guided != nil && first.Guided.Grammar == prompt.Grammar; sent != c.grammar || (first.Guided != nil && !sent) {
			t.Errorf("%s: grammar %+v, want sent %v", c.name, first.Guided, c.grammar)
		}
		if first.Seed != nil {
			t.Errorf("%s: seed %v outside a vote", c.name, *first.Seed)
		}
		if sent := second.Seed != nil && *second.Seed == 1; sent != c.seed || (second.Seed != nil && !sent) {
			t.Errorf("%s: a vote's second draft was sent seed %v, want sent %v", c.name, second.Seed, c.seed)
		}
		if third.Guided != nil {
			t.Errorf("%s: a prompt with no grammar was sent %+v", c.name, third.Guided)
		}
	}
}

func TestCompletePassesTheProvidersRefusalOn(t *testing.T) {
	refused := errors.New("agent: provider returned HTTP 400: failed to parse grammar")
	if _, err := New("l", &recorder{err: refused}).Complete(context.Background(), prompt); !errors.Is(err, refused) {
		t.Fatalf("err %v", err)
	}
	out, err := New("l", &recorder{res: agent.ProviderResponse{Text: "x"}}).Complete(context.Background(), prompt)
	if err != nil || out != (nl.Completion{Text: "x"}) {
		t.Fatalf("a reply with no usage: %+v, %v", out, err)
	}
}
