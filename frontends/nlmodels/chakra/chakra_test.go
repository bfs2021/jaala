package chakra

import (
	"context"
	"errors"
	"testing"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
)

// recorder is a Provider answering every Generate with res, or err, and keeping each request.
type recorder struct {
	res  agent.ProviderResponse
	err  error
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
	if req.MaxTokens != 512 || req.Temperature != nil {
		t.Errorf("New's model sent max tokens %d, temperature %v", req.MaxTokens, req.Temperature)
	}
	if req.Guided != nil || req.Seed != nil {
		t.Errorf("a model sending neither sent grammar %+v, seed %v", req.Guided, req.Seed)
	}
	if m.Name() != "qwen" {
		t.Errorf("name %q", m.Name())
	}
}

func TestCompleteSendsTheGrammarAndAVotesSeedWhenAsked(t *testing.T) {
	r := &recorder{res: agent.ProviderResponse{Text: "x"}}
	temp := 0.0
	m := &Model{Provider: r, Label: "l", Temperature: &temp, Grammar: true, Seed: true}
	drawn := prompt
	drawn.Draw = 2
	plain := prompt
	plain.Grammar = ""
	for _, p := range []nl.Prompt{prompt, drawn, plain} {
		if _, err := m.Complete(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	first, second, third := r.reqs[0], r.reqs[1], r.reqs[2]
	if first.Guided == nil || first.Guided.Grammar != prompt.Grammar || first.Seed != nil {
		t.Errorf("first prompt: grammar %+v, seed %v (no seed outside a vote)", first.Guided, first.Seed)
	}
	if second.Seed == nil || *second.Seed != 2 {
		t.Errorf("a vote's second draft was sent seed %v", second.Seed)
	}
	if third.Guided != nil {
		t.Errorf("a prompt with no grammar was sent %+v", third.Guided)
	}
	if first.Temperature == nil || *first.Temperature != 0 {
		t.Errorf("temperature %v, want 0 sent", first.Temperature)
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
