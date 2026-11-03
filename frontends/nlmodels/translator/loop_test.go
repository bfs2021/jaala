package translator

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/internal/guide"
)

// metered is a script whose every call reports in tokens in and out tokens out, in plus out more
// on each call than the last, so a Try or Step shows which calls it was charged.
type metered struct {
	*script
	in, out, calls int
}

func (m *metered) usage() *agent.Usage {
	m.calls++
	return &agent.Usage{InputTokens: m.in * m.calls, OutputTokens: m.out * m.calls}
}

func (m *metered) Generate(ctx context.Context, req agent.ProviderRequest) (*agent.ProviderResponse, error) {
	res, err := m.script.Generate(ctx, req)
	if res != nil {
		res.Usage = m.usage()
	}
	return res, err
}

func (m *metered) Stream(ctx context.Context, req agent.ProviderRequest) (agent.Stream, error) {
	st, err := m.script.Stream(ctx, req)
	if err != nil {
		return nil, err
	}
	var ds []agent.Delta
	for {
		d, err := st.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if d.Kind == agent.DeltaFinish {
			ds = append(ds, agent.Delta{Kind: agent.DeltaUsage, Usage: m.usage()})
		}
		ds = append(ds, d)
	}
	return agent.NewStubProvider(agent.StubTurn{Deltas: ds}).Stream(ctx, req)
}

// A Step carries its own call's tokens, and a Try its reply's and the guided call that rewrote it.
// control: the turn's tokens are all on one Step or Try, none twice.
func TestEachStepAndTryCarriesItsOwnTokens(t *testing.T) {
	v, b := guide.Deps(t)
	m := &metered{script: scripted("small", map[string][]string{mitQ: {"look pkg", mit}}), in: 10, out: 1}
	tr, err := New(v, nil, Config{Model: Role{Provider: m, Label: "small"}, Base: b, Actions: &nl.Actions{}})
	if err != nil {
		t.Fatal(err)
	}
	_, trace, err := tr.Ask(context.Background(), mitQ)
	if err != nil {
		t.Fatal(err)
	}
	// calls: 1 the look (10, 1), 2 the free answer (20, 2), 3 its guided rewrite (30, 3)
	if len(trace.Steps) != 1 || trace.Steps[0].TokensIn != 10 || trace.Steps[0].TokensOut != 1 {
		t.Errorf("step %+v", trace.Steps)
	}
	if len(trace.Tries) != 1 || trace.Tries[0].TokensIn != 50 || trace.Tries[0].TokensOut != 5 {
		t.Errorf("try %+v", trace.Tries)
	}
	if m.calls != 3 {
		t.Errorf("control: %d calls", m.calls)
	}
}

// A token budget the first answers spend ends Ask with a BudgetSpent, and so does a time budget;
// Abstain's Budget turns either into an abstention. control: a budget left unspent answers.
func TestABudgetStopsTheRunnerLoop(t *testing.T) {
	v, b := guide.Deps(t)
	bad := map[string][]string{mitQ: {`pkg(?p, "nope") => ?p`, `pkg(?p, "zz") => ?p`, mit}}
	ask := func(budget nl.Budget, abstain *nl.Abstain) (nl.Trace, error) {
		m := &metered{script: scripted("small", bad), in: 100, out: 10}
		tr, err := New(v, nil, Config{Model: Role{Provider: m}, Base: b, Budget: budget, Abstain: abstain})
		if err != nil {
			t.Fatal(err)
		}
		_, trace, err := tr.Ask(context.Background(), mitQ)
		return trace, err
	}
	var spent *nl.BudgetSpent
	if trace, err := ask(nl.Budget{Tokens: 150}, nil); !errors.As(err, &spent) || spent.Tokens < 150 || len(trace.Tries) == 0 || len(trace.Tries) > 2 {
		t.Errorf("tokens: %v after %d tries", err, len(trace.Tries))
	}
	if _, err := ask(nl.Budget{Time: time.Nanosecond}, nil); !errors.As(err, &spent) {
		t.Errorf("time: %v", err)
	}
	var abstained *nl.Abstained
	if _, err := ask(nl.Budget{Tokens: 150}, &nl.Abstain{Budget: true}); !errors.As(err, &abstained) || abstained.Reason != nl.AbstainBudget {
		t.Errorf("abstain: %v", err)
	}
	if _, err := ask(nl.Budget{Tokens: 100000}, nil); err != nil {
		t.Errorf("control: an unspent budget: %v", err)
	}
}

// A model with no answer accepted abstains under Abstain's NoAnswer, with its closest draft.
// control: without Abstain it fails with ErrNoProgram.
func TestTheRunnerLoopAbstains(t *testing.T) {
	v, b := guide.Deps(t)
	replies := map[string][]string{mitQ: {`pkg(?p, "nope") => ?p`, `pkg(?p, "zz") => ?p`, `pkg(?p, "yy") => ?p`}}
	tr, err := New(v, nil, Config{Model: Role{Provider: scripted("small", replies)}, Base: b, Abstain: &nl.Abstain{NoAnswer: true}})
	if err != nil {
		t.Fatal(err)
	}
	var abstained *nl.Abstained
	if _, _, err := tr.Ask(context.Background(), mitQ); !errors.As(err, &abstained) || abstained.Reason != nl.AbstainNoAnswer || abstained.Draft.Text == "" {
		t.Errorf("Ask: %v", err)
	}
	plain, _ := New(v, nil, Config{Model: Role{Provider: scripted("small", replies)}, Base: b})
	if _, _, err := plain.Ask(context.Background(), mitQ); !errors.Is(err, nl.ErrNoProgram) {
		t.Errorf("control: %v", err)
	}
}

// The actions are tools only while the model has steps left, and a Continue is the user's turn.
func TestTheRunnerOffersToolsUntilTheStepsRunOut(t *testing.T) {
	v, b := guide.Deps(t)
	m := scripted("small", map[string][]string{mitQ: {"look pkg", `pkg(?p, "nope") => ?p`, mit}})
	rec := &recordingProvider{Provider: m}
	tr, err := New(v, nil, Config{Model: Role{Provider: rec}, Base: b, Actions: &nl.Actions{Steps: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tr.Ask(context.Background(), mitQ); err != nil {
		t.Fatal(err)
	}
	if len(rec.reqs) < 3 || len(rec.reqs[0].Tools) != 3 || len(rec.reqs[1].Tools) != 0 || rec.reqs[1].Guided == nil {
		t.Fatalf("requests: %d, tools %d then %d", len(rec.reqs), len(rec.reqs[0].Tools), len(rec.reqs[1].Tools))
	}
	last := rec.reqs[len(rec.reqs)-1].Messages
	if note := last[len(last)-1]; note.Role != agent.RoleUser {
		t.Errorf("the refusal went back as %s", note.Role)
	}
}

// recordingProvider keeps each request it passes on.
type recordingProvider struct {
	agent.Provider
	reqs []agent.ProviderRequest
}

func (r *recordingProvider) Stream(ctx context.Context, req agent.ProviderRequest) (agent.Stream, error) {
	r.reqs = append(r.reqs, req)
	return r.Provider.Stream(ctx, req)
}

func (r *recordingProvider) Generate(ctx context.Context, req agent.ProviderRequest) (*agent.ProviderResponse, error) {
	r.reqs = append(r.reqs, req)
	return r.Provider.Generate(ctx, req)
}

func (r *recordingProvider) ProviderCapabilities() agent.Capabilities { return llamaCaps }
