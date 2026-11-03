package nl_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/internal/guide"
)

func abstained(t *testing.T, err error) *nl.Abstained {
	t.Helper()
	var a *nl.Abstained
	if !errors.As(err, &a) {
		t.Fatalf("err %v, want an *Abstained", err)
	}
	return a
}

// An answer whose vote agreed less than MinAgreement comes back as an abstention carrying it, also
// when Escalate kept it because the fallback had nothing better. control: a unanimous vote answers,
// and so does the disputed one without Abstain.
func TestAbstainOnADisputedAnswer(t *testing.T) {
	v, b := guide.Deps(t)
	disputed := []string{mit, apache, broken}
	o := nl.Options{Base: b, Vote: &nl.Vote{Drafts: 3}, Abstain: &nl.Abstain{MinAgreement: 0.6}}

	p, _, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", disputed...), o)
	a := abstained(t, err)
	if a.Reason != nl.AbstainLowAgreement || a.Draft.Text != mit || a.Draft.Confidence.Agreed != 1 || a.Draft.Confidence.Drawn != 3 || a.Err != nil {
		t.Errorf("abstained %+v", a)
	}
	if a.Diagnosis != "1 of 3 drafts agreed on its rows" || p.Text != "" {
		t.Errorf("diagnosis %q, program %q", a.Diagnosis, p.Text)
	}

	esc := o
	esc.Escalate, esc.Fallback = 0.6, model("big", "!fail")
	_, tr, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", disputed...), esc)
	if a := abstained(t, err); a.Draft.Model != "small" || a.Draft.Text != mit || len(tr.Escalations) != 1 {
		t.Errorf("escalated and kept: %+v, %d escalations", a, len(tr.Escalations))
	}

	if p, _, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", mit, mitToo, mit), o); err != nil || p.Text != mit {
		t.Errorf("control: unanimous, %q %v", p.Text, err)
	}
	o.Abstain = nil
	if p, _, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", disputed...), o); err != nil || p.Text != mit {
		t.Errorf("control: without Abstain, %q %v", p.Text, err)
	}
}

// When the tries run out, NoAnswer abstains with the draft that came closest: one that ran and
// answered nothing over one that didn't check, the later among equals. The abstention still wraps
// the error Translate would have returned. control: without NoAnswer it's that error alone.
func TestAbstainWhenNoDraftIsAccepted(t *testing.T) {
	v, b := guide.Deps(t)
	empty, empty2 := `pkg(?p, "mit") => ?p`, `pkg(?q, "mit") => ?q`
	o := nl.Options{Base: b, Abstain: &nl.Abstain{NoAnswer: true}}
	for _, c := range []struct {
		name    string
		replies []string
		best    string
	}{
		{"ran beats didn't check", []string{broken, empty, `nada(?p) => ?p`}, empty},
		{"the later of equals", []string{empty, broken, empty2}, empty2},
	} {
		_, tr, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", c.replies...), o)
		a := abstained(t, err)
		if a.Reason != nl.AbstainNoAnswer || a.Draft.Text != c.best || a.Draft.Model != "small" || len(tr.Tries) != 3 {
			t.Errorf("%s: %+v after %d tries", c.name, a, len(tr.Tries))
		}
		if !strings.Contains(a.Diagnosis, "matched nothing") || !strings.Contains(err.Error(), "not confident (no answer)") {
			t.Errorf("%s: diagnosis %q, error %q", c.name, a.Diagnosis, err)
		}
		if !errors.Is(err, tr.Tries[2].Err) || !strings.Contains(err.Error(), "no program checked after 3 tries") {
			t.Errorf("%s: %v doesn't wrap the last try's error", c.name, err)
		}
	}
	judged := o
	judged.Pack = &nl.Pack{Check: func(context.Context, nl.Program) error { return errors.New("not a list of packages") }}
	_, _, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", broken, mit, `nada(?p) => ?p`), judged)
	if a := abstained(t, err); a.Draft.Text != mit || !strings.Contains(a.Diagnosis, "not a list of packages") {
		t.Errorf("one the pack's Check refused beats one that didn't check: %+v", a)
	}
	o.Abstain = &nl.Abstain{MinAgreement: 0.6, Budget: true}
	_, _, err = nl.Translate(context.Background(), mitQ, v, nil, model("small", broken, empty, broken), o)
	var a *nl.Abstained
	if err == nil || errors.As(err, &a) {
		t.Errorf("control: without NoAnswer, %v", err)
	}
}

// A loop that ends on a repeated draft abstains as a repeat, offering the draft it repeated.
func TestAbstainWhenTheModelRepeatsItself(t *testing.T) {
	v, b := guide.Deps(t)
	_, tr, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", broken, broken), nl.Options{Base: b, Abstain: &nl.Abstain{NoAnswer: true}})
	a := abstained(t, err)
	if a.Reason != nl.AbstainRepeat || a.Draft.Text != broken || len(tr.Tries) != 2 || tr.Tries[1].Kind != nl.KindRepeat {
		t.Errorf("%+v, kinds %v", a, kinds(tr))
	}
}

// A model that fails, a cancelled call and a grounding error are failures, never abstentions: the
// host must still be able to tell "broke" apart.
func TestAbstainNeverCoversAFailure(t *testing.T) {
	v, b := guide.Deps(t)
	all := nl.Options{Base: b, Abstain: &nl.Abstain{MinAgreement: 0.6, NoAnswer: true, Budget: true}}
	var a *nl.Abstained
	if _, _, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", broken, "!fail"), all); err == nil || errors.As(err, &a) {
		t.Errorf("a model failing: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := nl.Translate(ctx, mitQ, v, nil, model("small", mit), all); !errors.Is(err, context.Canceled) || errors.As(err, &a) {
		t.Errorf("cancelled: %v", err)
	}
	if _, _, err := nl.Translate(context.Background(), mitQ, v, failing{}, model("small", mit), all); err == nil || errors.As(err, &a) {
		t.Errorf("grounding: %v", err)
	}
}

type failing struct{}

func (failing) Ground(context.Context, string) ([]nl.Candidate, error) {
	return nil, errors.New("index down")
}

// Budget abstains when Options.Budget runs out, still wrapping the BudgetSpent. control: without
// it, the BudgetSpent alone.
func TestAbstainWhenTheBudgetRunsOut(t *testing.T) {
	v, b := guide.Deps(t)
	wrong, wrong2 := `pkg(?p, "mit") => ?p`, `pkg(?q, "mit") => ?q`
	o := nl.Options{Base: b, MaxTries: 5, Budget: nl.Budget{Tokens: 250}, Abstain: &nl.Abstain{Budget: true}}
	_, _, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", wrong, wrong2, mit), o)
	a := abstained(t, err)
	var spent *nl.BudgetSpent
	if a.Reason != nl.AbstainBudget || a.Draft.Text != wrong2 || !errors.As(err, &spent) {
		t.Errorf("%+v", a)
	}
	o.Abstain = &nl.Abstain{NoAnswer: true}
	if _, _, err := nl.Translate(context.Background(), mitQ, v, nil, model("small", wrong, wrong2, mit), o); !errors.As(err, &spent) || errors.As(err, &a) {
		t.Errorf("control: without Budget, %v", err)
	}
}

// Eval classes an abstention apart from a failure, also one wrapping a budget.
func TestEvalCountsAbstentionsApart(t *testing.T) {
	v, b := guide.Deps(t)
	cases := []nl.Case{
		{Name: "repeat", Question: mitQ, Want: [][]string{{"app"}}},
		{Name: "budget", Question: mitQ, Want: [][]string{{"app"}}},
		{Name: "fails", Question: mitQ, Want: [][]string{{"app"}}},
	}
	i := 0
	rep := nl.Eval(context.Background(), cases, b, func(ctx context.Context, q string) (nl.Program, nl.Trace, error) {
		i++
		switch i {
		case 1:
			return nl.Translate(ctx, q, v, nil, model("small", broken, broken), nl.Options{Base: b, Abstain: &nl.Abstain{NoAnswer: true}})
		case 2:
			return nl.Translate(ctx, q, v, nil, model("small", broken, mit), nl.Options{Base: b, Budget: nl.Budget{Tokens: 50}, Abstain: &nl.Abstain{Budget: true}})
		}
		return nl.Translate(ctx, q, v, nil, model("small", broken, broken), nl.Options{Base: b})
	})
	if rep.Failures[nl.ClassAbstained] != 2 || rep.Failures[nl.ClassConstruct] != 1 || len(rep.Failures) != 2 {
		t.Errorf("failures %v", rep.Failures)
	}
}
