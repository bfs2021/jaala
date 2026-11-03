package translator

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/ns"
	"github.com/panyam/jaala/stdlib"
)

// recorder is a Provider replying with replies in turn, keeping each request, and reporting caps.
type recorder struct {
	replies []string
	caps    agent.Capabilities
	reqs    []agent.ProviderRequest
}

func (r *recorder) Generate(_ context.Context, req agent.ProviderRequest) (*agent.ProviderResponse, error) {
	r.reqs = append(r.reqs, req)
	if len(r.reqs) > len(r.replies) {
		return nil, errors.New("out of replies")
	}
	return &agent.ProviderResponse{Text: r.replies[len(r.reqs)-1]}, nil
}

func (r *recorder) Stream(context.Context, agent.ProviderRequest) (agent.Stream, error) {
	return nil, errors.New("not streamed")
}

func (r *recorder) ProviderCapabilities() agent.Capabilities { return r.caps }

// llamaCaps is what chakra's llama.cpp preset reports.
var llamaCaps = agent.Capabilities{Guided: []string{agent.GuidedGrammar, agent.GuidedJSONSchema}, Seed: true}

func replying(replies ...string) *recorder { return &recorder{replies: replies, caps: llamaCaps} }

// graph is a vocabulary of edges a to b to c, and a Base over it.
func graph(t *testing.T) (*ns.Vocabulary, *datalog.Base) {
	t.Helper()
	src := ns.NewMemSource().Declare("edge", "from", "to")
	src.Add("edge", ns.Tuple{Vals: []ns.Value{ns.S("a"), ns.S("b")}})
	src.Add("edge", ns.Tuple{Vals: []ns.Value{ns.S("b"), ns.S("c")}})
	v, err := ns.NewVocabulary(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddLanguage(datalog.Language); err != nil {
		t.Fatal(err)
	}
	if err := stdlib.Register(v); err != nil {
		t.Fatal(err)
	}
	return v, datalog.MustBase(v, src)
}

func temperature(req agent.ProviderRequest) string {
	if req.Temperature == nil {
		return "none"
	}
	return fmt.Sprint(*req.Temperature)
}

// The model answers greedily with the grammar and 512 tokens; the fallback, asked when the model
// fails, samples at 0.7. Both are named by role and answered over the Base.
func TestAskTunesEachRole(t *testing.T) {
	v, b := graph(t)
	model := replying(`nope(?x) => ?x`, `nope(?x) => ?x`)
	fallback := replying(`edge("a", ?x) => ?x`)
	tr, err := New(v, nil, Config{Model: Role{Provider: model}, Fallback: Role{Provider: fallback}, Base: b})
	if err != nil {
		t.Fatal(err)
	}
	p, trace, err := tr.Ask(context.Background(), "where does a lead?")
	if err != nil || p.Text != `edge("a", ?x) => ?x` || p.Model != "fallback" || len(p.Rows) != 1 {
		t.Fatalf("Ask = %+v, %v (%d tries)", p, err, len(trace.Tries))
	}
	first, fb := model.reqs[0], fallback.reqs[0]
	if temperature(first) != "0" || first.MaxTokens != MaxTokens || first.Guided == nil || first.Guided.Grammar == "" {
		t.Errorf("model asked at temperature %s, %d tokens, grammar %v", temperature(first), first.MaxTokens, first.Guided != nil)
	}
	if temperature(fb) != "0.7" || fb.MaxTokens != MaxTokens {
		t.Errorf("fallback asked at temperature %s, %d tokens", temperature(fb), fb.MaxTokens)
	}
	if trace.Tries[0].Model != "model" || tr.Model().Name() != "model" || tr.Fallback().Name() != "fallback" {
		t.Errorf("names %q %q %q", trace.Tries[0].Model, tr.Model().Name(), tr.Fallback().Name())
	}
}

// A Role's own temperature, token limit and label replace the defaults, and NoTemperature sends none.
// control: without a Fallback provider there is no fallback.
func TestARoleOverridesTheDefaults(t *testing.T) {
	v, _ := graph(t)
	warm := 0.3
	model, fallback := replying(`nope(?x) => ?x`), replying(`edge("a", ?x) => ?x`)
	tr, err := New(v, nil, Config{
		Model:    Role{Provider: model, Label: "qwen", Temperature: &warm, MaxTokens: 99},
		Fallback: Role{Provider: fallback, Label: "claude", NoTemperature: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tr.Ask(context.Background(), "q"); err != nil {
		t.Fatal(err)
	}
	if req := model.reqs[0]; temperature(req) != "0.3" || req.MaxTokens != 99 || tr.Model().Name() != "qwen" {
		t.Errorf("model: temperature %s, %d tokens, %q", temperature(req), req.MaxTokens, tr.Model().Name())
	}
	if req := fallback.reqs[0]; req.Temperature != nil || tr.Fallback().Name() != "claude" {
		t.Errorf("fallback: temperature %s, %q", temperature(req), tr.Fallback().Name())
	}
	alone, err := New(v, nil, Config{Model: Role{Provider: replying()}})
	if err != nil || alone.Fallback() != nil {
		t.Errorf("control: no fallback named gave %v, %v", alone.Fallback(), err)
	}
	if _, err := New(v, nil, Config{Fallback: Role{Provider: fallback}}); err == nil || !strings.Contains(err.Error(), "Config.Model needs a Provider") {
		t.Errorf("no model: %v", err)
	}
}

// A thinking model gets no grammar, the think line and room to think, and only the last fenced block
// outside <think> is its draft. control: the same reply from a model that doesn't think is refused.
func TestAThinkingModelThinksBeforeItsProgram(t *testing.T) {
	v, b := graph(t)
	reply := "<think>maybe ```\nnope(?x) => ?x\n```</think>\nFirst I tried:\n```datalog\nnode(?x) => ?x\n```\nso the answer is\n```datalog\nedge(\"a\", ?x) => ?x\n```"
	model := replying(reply)
	tr, err := New(v, nil, Config{Model: Role{Provider: model, Thinking: true}, Base: b})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := tr.Ask(context.Background(), "where does a lead?")
	if err != nil || p.Text != `edge("a", ?x) => ?x` {
		t.Fatalf("Ask = %q, %v", p.Text, err)
	}
	req := model.reqs[0]
	if req.Guided != nil || req.MaxTokens != ThinkingMaxTokens || !strings.HasSuffix(req.Instructions, DefaultThinkPrompt) || temperature(req) != "0" {
		t.Errorf("thinking model asked with grammar %v, %d tokens, temperature %s, instructions ending %q",
			req.Guided != nil, req.MaxTokens, temperature(req), req.Instructions[max(0, len(req.Instructions)-80):])
	}
	plain, err := New(v, nil, Config{Model: Role{Provider: replying(reply)}, Base: b})
	if err != nil {
		t.Fatal(err)
	}
	if p, _, err := plain.Ask(context.Background(), "where does a lead?"); err == nil {
		t.Errorf("control: a model that doesn't think answered %q from the same reply", p.Text)
	}
}

// A Role's ThinkPrompt replaces the think line word for word (#485).
func TestAThinkingModelTakesTheHostsThinkLine(t *testing.T) {
	v, b := graph(t)
	model := replying("```\nedge(\"a\", ?x) => ?x\n```")
	tr, err := New(v, nil, Config{Model: Role{Provider: model, Thinking: true, ThinkPrompt: "\n# H-THINK\n"}, Base: b})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tr.Ask(context.Background(), "where does a lead?"); err != nil {
		t.Fatal(err)
	}
	if ins := model.reqs[0].Instructions; !strings.HasSuffix(ins, "\n# H-THINK\n") || strings.Contains(ins, DefaultThinkPrompt) {
		t.Errorf("instructions end %q", ins[max(0, len(ins)-80):])
	}
}

// Config's nl options reach Translate, and Options runs last, so what it sets wins.
func TestAskPassesTheOptionsOn(t *testing.T) {
	v, b := graph(t)
	model := replying(`edge("z", ?x) => ?x`, `edge("a", ?x) => ?x`)
	var seen nl.Options
	actions, vote, abstain := &nl.Actions{Steps: 2}, &nl.Vote{Drafts: 3}, &nl.Abstain{}
	tr, err := New(v, &nl.Pack{About: "a graph of roads"}, Config{
		Model: Role{Provider: model}, Base: b, Budget: nl.Budget{Tokens: 12345},
		Actions: actions, Vote: vote, Escalate: 0.5, Abstain: abstain,
		Options: func(o *nl.Options) {
			seen = *o
			o.MaxTries = 1
			o.Actions, o.Vote, o.Escalate, o.Abstain = nil, nil, 0, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, trace, err := tr.Ask(context.Background(), "where does a lead?")
	if err == nil || len(trace.Tries) != 1 || len(model.reqs) != 1 {
		t.Errorf("MaxTries 1 from Options: %v after %d tries", err, len(trace.Tries))
	}
	if seen.Actions != actions || seen.Vote != vote || seen.Escalate != 0.5 || seen.Abstain != abstain {
		t.Errorf("Config's nl options didn't reach Translate: %+v", seen)
	}
	if seen.Base != b || seen.Budget.Tokens != 12345 || seen.Pack == nil || !strings.Contains(model.reqs[0].Instructions, "a graph of roads") {
		t.Errorf("options seen %+v; instructions carry the pack: %v", seen, strings.Contains(model.reqs[0].Instructions, "a graph of roads"))
	}
}

func TestProgramInReadsTheLastFenceOutsideThinking(t *testing.T) {
	for reply, want := range map[string]string{
		"x => ?y":                                 "x => ?y",
		"```\na\n```\n```datalog\nb\n```":         "b",
		"<think>```\na\n```</think>no fence here": "no fence here",
	} {
		if got := programIn(reply); got != want {
			t.Errorf("programIn(%q) = %q, want %q", reply, got, want)
		}
	}
}
