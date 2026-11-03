package translator_test

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/panyam/jaala/frontends/nl"
)

// A question one callable answers is answered by the call the model picks, asked with the catalogue
// rather than the card, under a grammar of none or a callable's call. The program is the call itself.
func TestTranslateAnswersWithAPickedCall(t *testing.T) {
	v := vocabulary(t)
	g := &grounder{cands: []nl.Candidate{{Name: "a", Values: []string{"a"}}}}
	m := model("small", `edge("a", ?to)`)
	p, tr, err := translate(context.Background(), "where does a lead?", v, g, m, nl.Options{Base: graphBase(t, v), Pack: edges()})
	if err != nil || p.Text != `edge("a", ?to)` || len(p.Rows) != 1 || p.Rows[0].Bind["to"].S != "b" || len(m.asked) != 1 {
		t.Fatalf("Translate = %+v, %v, asked %d times", p, err, len(m.asked))
	}
	if len(tr.Tries) != 1 || tr.Tries[0].Mode != nl.ModeCall || p.Confidence.Drawn != 1 {
		t.Errorf("tries %+v, confidence %+v", tr.Tries, p.Confidence)
	}
	asked := m.asked[0]
	for _, want := range []string{"# Answer the question with one call", "# a small graph", "# edge(?from, ?to): where an edge leads",
		"#   ?from: the node it starts at", "#   ?to: the node it leads to"} {
		if !strings.Contains(asked.Prefix, want) {
			t.Errorf("the pick's prefix lacks %q:\n%s", want, asked.Prefix)
		}
	}
	if strings.Contains(asked.Prefix, "graph.reach") || strings.Contains(asked.Prefix, "what reaches a?") {
		t.Errorf("the pick's prefix carries the card or the pack's examples:\n%s", asked.Prefix)
	}
	if !strings.Contains(asked.Turns[0].Text, `a: "a"`) || !strings.Contains(asked.Grammar, `"none"`) || !strings.Contains(asked.Grammar, `"edge("`) || strings.Contains(asked.Grammar, `"node("`) {
		t.Errorf("the pick's question %q, grammar:\n%s", asked.Turns[0].Text, asked.Grammar)
	}

	undoc := model("small", "none", `edge("a", ?b) => ?b`)
	translate(context.Background(), "q", v, nil, undoc, nl.Options{Base: graphBase(t, v), Pack: &nl.Pack{Callables: []nl.Callable{{Relation: "edge"}, {Relation: "graph.reach"}}}})
	if pre := undoc.asked[0].Prefix; !strings.Contains(pre, "# edge(?from, ?to): A directed edge between two nodes.\n") ||
		!strings.Contains(pre, "# graph.reach(?a, ?b): reach is every node a node gets to by edges.\n") || strings.Contains(pre, "second line") {
		t.Errorf("callables without a doc are listed as:\n%s", pre)
	}

	none := model("small", `edge("a", ?b) => ?b`)
	translate(context.Background(), "q", v, nil, none, nl.Options{Base: graphBase(t, v), Pack: &nl.Pack{About: "a small graph"}})
	if strings.Contains(none.asked[0].Prefix, "# Answer the question with one call") || !strings.Contains(none.asked[0].Prefix, "graph.reach") {
		t.Errorf("control: a pack without callables asked a pick:\n%s", none.asked[0].Prefix)
	}
}

// A refused pick goes back once, as a draft's refusal does, and a second refusal, or a repeat, moves
// on to drafting a program in a fresh conversation. control: a fixed second pick is accepted.
func TestTranslateDraftsWhenNoPickedCallIsAccepted(t *testing.T) {
	v := vocabulary(t)
	b := graphBase(t, v)
	m := model("small", `edge("c", ?to)`, `edge("b", ?to)`)
	p, tr, err := translate(context.Background(), "q", v, nil, m, nl.Options{Base: b, Pack: edges()})
	if err != nil || p.Text != `edge("b", ?to)` || !slices.Equal(kinds(tr), []string{nl.KindEmpty, ""}) || !slices.Equal(modes(tr), []string{nl.ModeCall, nl.ModeCall}) {
		t.Fatalf("control: Translate = %+v, %v, kinds %v, modes %v", p, err, kinds(tr), modes(tr))
	}
	if turns := m.asked[1].Turns; len(turns) != 3 || turns[1].Text != `edge("c", ?to)` || !strings.Contains(turns[2].Text, "answered no rows") || !strings.HasSuffix(turns[2].Text, "Call another relation, or this one with other values, or reply none.") {
		t.Errorf("the refused pick went back as %+v", turns)
	}

	for _, c := range []struct {
		name    string
		replies []string
		kinds   []string
	}{
		{"two refused", []string{`edge("c", ?to)`, `edge("zz", ?to)`, `edge("a", ?b) => ?b`}, []string{nl.KindEmpty, nl.KindEmpty, ""}},
		{"a repeat", []string{`edge("c", ?to)`, `edge("c", ?to)`, `edge("a", ?b) => ?b`}, []string{nl.KindEmpty, nl.KindRepeat, ""}},
	} {
		m := model("small", c.replies...)
		p, tr, err := translate(context.Background(), "q", v, nil, m, nl.Options{Base: b, Pack: edges()})
		if err != nil || p.Text != `edge("a", ?b) => ?b` || !slices.Equal(kinds(tr), c.kinds) || !slices.Equal(modes(tr), []string{nl.ModeCall, nl.ModeCall, ""}) {
			t.Errorf("%s: Translate = %+v, %v, kinds %v, modes %v", c.name, p, err, kinds(tr), modes(tr))
			continue
		}
		if draft := m.asked[2]; len(draft.Turns) != 1 || !strings.Contains(draft.Prefix, "graph.reach") {
			t.Errorf("%s: the draft wasn't asked fresh, with the card: %+v", c.name, draft.Turns)
		}
	}
}

// A model replying none drafts a program, and the none is a Step rather than a try, so a question
// no callable fits still counts its draft as the first try.
func TestTranslateDraftsWhenTheModelPicksNone(t *testing.T) {
	v := vocabulary(t)
	m := model("small", "none", `edge("a", ?b) => ?b`)
	p, tr, err := translate(context.Background(), "q", v, nil, m, nl.Options{Base: graphBase(t, v), Pack: edges()})
	if err != nil || p.Text != `edge("a", ?b) => ?b` || len(tr.Tries) != 1 || tr.Tries[0].Mode != "" {
		t.Fatalf("Translate = %+v, %v, tries %+v", p, err, tr.Tries)
	}
	if len(tr.Steps) != 1 || tr.Steps[0].Action != nl.ActionNone || tr.Steps[0].After != 0 {
		t.Errorf("steps %+v", tr.Steps)
	}
	if !strings.Contains(m.asked[1].Prefix, "what reaches a?") {
		t.Errorf("control: the draft's prefix lacks the pack's examples")
	}
}

// A model failing on a pick is a try of its own, and the question is drafted; a pick's tokens count
// against the Budget like a draft's.
func TestAPickSpendsAndFailsAsADraftDoes(t *testing.T) {
	v := vocabulary(t)
	m := model("small", "!fail", `edge("a", ?b) => ?b`)
	_, tr, err := translate(context.Background(), "q", v, nil, m, nl.Options{Base: graphBase(t, v), Pack: edges()})
	if err != nil || !slices.Equal(kinds(tr), []string{nl.KindModel, ""}) || !slices.Equal(modes(tr), []string{nl.ModeCall, ""}) {
		t.Errorf("a failed pick: %v, kinds %v, modes %v", err, kinds(tr), modes(tr))
	}
	m = model("small", `edge("c", ?to)`, `edge("b", ?to)`)
	_, _, err = translate(context.Background(), "q", v, nil, m, nl.Options{Base: graphBase(t, v), Pack: edges(), Budget: nl.Budget{Tokens: 100}})
	var spent *nl.BudgetSpent
	if !errors.As(err, &spent) || len(m.asked) != 1 {
		t.Errorf("a pick over the budget: %v after %d asks", err, len(m.asked))
	}
}

// A picked call is voted on with the pick's prompt, and is jaala's text under a front end too.
func TestAPickedCallIsVotedOnAndIsJaalasText(t *testing.T) {
	v := vocabulary(t)
	m := model("small", `edge("a", ?to)`, `edge("a", ?to)`, `edge("a", ?x)`)
	p, tr, err := translate(context.Background(), "q", v, nil, m, nl.Options{Base: graphBase(t, v), Pack: edges(), FrontEnd: shout{}, Vote: &nl.Vote{Drafts: 3}})
	if err != nil || p.Language != "" || p.Confidence.Drawn != 3 || p.Confidence.Agreed != 3 || len(tr.Votes) != 2 {
		t.Fatalf("Translate = %+v, %v, votes %+v", p, err, tr.Votes)
	}
	for i, a := range m.asked {
		if !strings.Contains(a.Prefix, "# Answer the question with one call") || a.Draw != i {
			t.Errorf("ask %d: draw %d, prefix %.60q", i, a.Draw, a.Prefix)
		}
	}
}

// A picked call its vote disputes is escalated past, as a drafted one is, and the question is drafted;
// the call is kept in case nothing better comes.
func TestADisputedPickIsEscalatedToDrafting(t *testing.T) {
	v := vocabulary(t)
	m := model("small", `edge("a", ?to)`, `edge("b", ?to)`, `edge("b", ?x)`, `edge("a", ?b) => ?b`, `edge("a", ?b) => ?b`, `edge("a", ?b) => ?b`)
	p, tr, err := translate(context.Background(), "q", v, nil, m, nl.Options{Base: graphBase(t, v), Pack: edges(), Vote: &nl.Vote{Drafts: 3}, Escalate: 0.9})
	if err != nil || p.Text != `edge("a", ?b) => ?b` || len(tr.Escalations) != 1 || tr.Escalations[0].Confidence.Agreed != 2 {
		t.Fatalf("Translate = %+v, %v, escalations %+v", p, err, tr.Escalations)
	}
	m = model("small", `edge("a", ?to)`, `edge("b", ?to)`, `edge("b", ?x)`)
	p, _, err = translate(context.Background(), "q", v, nil, m, nl.Options{Base: graphBase(t, v), Pack: edges(), Vote: &nl.Vote{Drafts: 3}, Escalate: 0.9})
	if err != nil || p.Rows[0].Bind["to"].S != "c" {
		t.Errorf("with no draft left, the escalated call wasn't kept: %+v, %v", p, err)
	}
}

// The pick arm of mission #322's exercise (#484): the guide's questions with the relations that
// answer them marked callable, beside TestEval's drafting arm, its report in
// testdata/eval_call.golden. The small model picks where a relation fits, replies none where none
// does, and drafts then; it fixes one empty pick. control: the drafting arm's numbers, which picking
// should beat on first-try passes.
func TestEvalCallable(t *testing.T) {
	v, b := depsBase(t)
	pack := &nl.Pack{Callables: []nl.Callable{
		{Relation: "imports", Doc: "the packages a package imports directly", Args: []string{"the importing package", "the imported one"}},
		{Relation: "deps.depends_on", Args: []string{"the package", "a package it depends on"}},
		{Relation: "pkg", Args: []string{"the package", "its license, absent when unstated"}},
	}}
	small := drafter("small", map[string][]string{
		"what does app import directly?":            {`imports("app", ?d)`},
		"what does app depend on, directly or not?": {`deps.depends_on("app", ?d)`},
		"what license does util have?":              {`pkg("util", ?l)`},
		"which packages have no license stated?":    {"none", `pkg(?p, ?l), absent(?l) => ?p`},
		"how many packages does app depend on?":     {"none", `deps.depends_on("app", ?d) => count(?d)`},
		"what imports util?":                        {`imports("util", ?x)`, `imports(?x, "util")`},
		"list every package with its license":       {`pkg(?p, ?l)`},
		"who maintains api?":                        {"none", `pkg("api", ?m) => ?m`},
	})
	rep := nl.Eval(context.Background(), evalCases, b, func(ctx context.Context, q string) (nl.Program, nl.Trace, error) {
		return translate(ctx, q, v, nil, small, nl.Options{Base: b, Pack: pack})
	})
	got := rep.String()
	t.Log(got)
	const golden = "testdata/eval_call.golden"
	if *update {
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("report:\n%s\nwant:\n%s", got, want)
	}
	if rep.Passed != 7 || rep.PassedFirstTry != 6 {
		t.Errorf("passed %d, %d first try; want 7, 6", rep.Passed, rep.PassedFirstTry)
	}
	called := 0
	for _, r := range rep.Cases {
		if n := len(r.Trace.Tries); r.Pass && n > 0 && r.Trace.Tries[n-1].Mode == nl.ModeCall {
			called++
		}
	}
	if called != 5 {
		t.Errorf("%d cases answered by a call, want 5", called)
	}
	draftArm := drafter("small", smallDrafts)
	ctl := nl.Eval(context.Background(), evalCases, b, func(ctx context.Context, q string) (nl.Program, nl.Trace, error) {
		return translate(ctx, q, v, nil, draftArm, nl.Options{Base: b}, drafter("big", bigDrafts))
	})
	if ctl.PassedFirstTry != 2 || rep.PassedFirstTry <= ctl.PassedFirstTry {
		t.Errorf("control: drafting passes %d first try, picking %d", ctl.PassedFirstTry, rep.PassedFirstTry)
	}
}
