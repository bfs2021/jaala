package translator_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/panyam/jaala/frontends/nl"
)

// The small model's drafts: right first, a missing module, a wrong arity, an unbound variable, an
// uppercase constant, a program answering nothing, columns in another order, and a relation the
// vocabulary doesn't hold. The fallback gets the ones the small model gives up on.
var smallDrafts = map[string][]string{
	"what does app import directly?":            {`imports("app", ?d) => ?d`},
	"what does app depend on, directly or not?": {`depends_on("app", ?d) => ?d`, `deps.depends_on("app", ?d) => ?d`},
	"what license does util have?":              {`pkg("util") => ?l`, `pkg("util", ?l) => ?l`},
	"which packages have no license stated?":    {`pkg(?p, ?l), absent(?x) => ?p`, `pkg(?p, ?l), absent(?l) => ?p`},
	"how many packages does app depend on?":     {`deps.depends_on(App, ?d) => count(?d)`, `deps.depends_on("app", ?d) => count(?d)`},
	"what imports util?":                        {`imports("util", ?x) => ?x`},
	"list every package with its license":       {`pkg(?n, ?l) => ?l, ?n`},
	"who maintains api?":                        {`maintainer("api", ?m) => ?m`, `maintainer("api", ?m) => ?m`, `owner("api", ?m) => ?m`},
}

var bigDrafts = map[string][]string{
	"what imports util?": {`imports(?x, "util") => ?x`},
	"who maintains api?": {`pkg("api", ?m) => ?m`},
}

// TestEval is mission #322's exercise until Declaire's translator-alone arm runs: Eval over the
// guide's questions with a scripted small model and fallback, its report pinned in testdata/eval.golden
// (-update rewrites it). -v prints the card's size and the report.
func TestEval(t *testing.T) {
	v, b := depsBase(t)
	card, err := nl.Card(v)
	if err != nil {
		t.Fatal(err)
	}
	small, big := drafter("small", smallDrafts), drafter("big", bigDrafts)
	rep := nl.Eval(context.Background(), evalCases, b, func(ctx context.Context, q string) (nl.Program, nl.Trace, error) {
		return translate(ctx, q, v, nil, small, nl.Options{Base: b}, big)
	})
	got := rep.String()
	t.Logf("card: %d bytes, %d lines\n%s", len(card), strings.Count(card, "\n"), got)
	const golden = "testdata/eval.golden"
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
	if rep.Passed != 7 || rep.PassedFirstTry != 2 || rep.Checked == 0 || rep.Fixed == 0 {
		t.Errorf("passed %d, %d first try, %d of %d check errors with a fix", rep.Passed, rep.PassedFirstTry, rep.Fixed, rep.Checked)
	}
	if r := rep.Cases[7]; r.Pass || r.Err != nil || r.Program.Model != "big" || len(r.Got) != 1 || r.Got[0][0] != "absent" {
		t.Errorf("missing: pass %v, %v, by %s, got %v; want the fallback's wrong answer", r.Pass, r.Err, r.Program.Model, r.Got)
	}
}

// A row matches by its values whatever its columns' order, rows as a multiset unless Ordered, and a
// duplicate row counts.
func TestEvalMatchesRowsByTheirValues(t *testing.T) {
	v, b := depsBase(t)
	for _, c := range []struct {
		draft   string
		want    [][]string
		ordered bool
		pass    bool
	}{
		{`imports(?a, ?b) => ?b, ?a`, [][]string{{"api", "app"}, {"log", "api"}, {"util", "log"}}, false, true},
		{`imports(?a, ?b) => ?a, ?b`, [][]string{{"log", "util"}, {"app", "api"}, {"api", "log"}}, false, true},
		{`imports(?a, ?b) => ?a, ?b order by ?a`, [][]string{{"log", "util"}, {"app", "api"}, {"api", "log"}}, true, false},
		{`imports(?a, ?b) => ?a, ?b order by ?a`, [][]string{{"api", "log"}, {"app", "api"}, {"log", "util"}}, true, true},
		{`imports(?a, ?b) => ?a`, [][]string{{"app"}, {"api"}}, false, false},
		{`imports(?a, ?b) => ?a`, [][]string{{"app"}, {"api"}, {"log"}, {"log"}}, false, false},
	} {
		m := drafter("small", map[string][]string{"q": {c.draft}})
		rep := nl.Eval(context.Background(), []nl.Case{{Name: "q", Question: "q", Want: c.want, Ordered: c.ordered}}, b, func(ctx context.Context, q string) (nl.Program, nl.Trace, error) {
			return translate(ctx, q, v, nil, m, nl.Options{})
		})
		if rep.Cases[0].Pass != c.pass {
			t.Errorf("%s against %v (ordered %v): pass %v, got %v", c.draft, c.want, c.ordered, rep.Cases[0].Pass, rep.Cases[0].Got)
		}
	}
}
