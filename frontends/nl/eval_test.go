package nl_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/internal/guide"
	"github.com/panyam/jaala/ns"
)

// depsBase is the guide's example in miniature (internal/guide).
func depsBase(t *testing.T) (*ns.Vocabulary, *datalog.Base) { return guide.Deps(t) }

// Rows Translate already ran over a Base are scored as they are, not evaluated again, and a case whose
// translate fails fails alone.
func TestEvalScoresEachCaseOnItsOwn(t *testing.T) {
	_, b := depsBase(t)
	ran := nl.Program{Rows: []datalog.Row{{Bind: map[datalog.Var]ns.Value{"x": ns.S("kept")}}}} // its zero Query wouldn't evaluate
	rep := nl.Eval(context.Background(), []nl.Case{
		{Name: "fails", Question: "a", Want: [][]string{{"x"}}},
		{Name: "ran", Question: "b", Want: [][]string{{"kept"}}},
	}, b, func(_ context.Context, q string) (nl.Program, nl.Trace, error) {
		if q == "a" {
			return nl.Program{}, nl.Trace{Tries: []nl.Try{{Kind: nl.KindModel, Err: errors.New("down")}}}, errors.New("down")
		}
		return ran, nl.Trace{Tries: []nl.Try{{TokensIn: 7}}}, nil
	})
	if len(rep.Cases) != 2 || rep.Cases[0].Pass || rep.Cases[0].Err == nil || !rep.Cases[1].Pass || rep.Cases[1].Err != nil {
		t.Fatalf("cases %+v", rep.Cases)
	}
	if rep.Passed != 1 || rep.PassedFirstTry != 1 || rep.Tries != 2 || rep.TokensIn != 7 || rep.Refused[nl.KindModel] != 1 {
		t.Errorf("report %+v", rep)
	}
}

// Costliest ranks the evaluated tries by their work, most first and a tie in case and try order, and
// leaves out tries never evaluated. control: asked for more than there are, it returns them all.
func TestEvalRanksTheCostliestDrafts(t *testing.T) {
	_, b := depsBase(t)
	worked := func(w int64) nl.Try { return nl.Try{Report: &datalog.Report{Work: w}} }
	traces := map[string]nl.Trace{
		"a": {Tries: []nl.Try{worked(5), {Kind: nl.KindParse}, worked(9)}},
		"b": {Tries: []nl.Try{worked(9), worked(1)}},
	}
	rep := nl.Eval(context.Background(), []nl.Case{{Name: "a", Question: "a"}, {Name: "b", Question: "b"}}, b,
		func(_ context.Context, q string) (nl.Program, nl.Trace, error) {
			return nl.Program{}, traces[q], errors.New("scored elsewhere")
		})
	var got []string
	for _, d := range rep.Costliest(3) {
		got = append(got, fmt.Sprintf("%s%d:%d", d.Case, d.N, d.Try.Report.Work))
	}
	if want := []string{"a3:9", "b1:9", "a1:5"}; !slices.Equal(got, want) {
		t.Errorf("Costliest(3) = %v, want %v", got, want)
	}
	if n := len(rep.Costliest(10)); n != 4 {
		t.Errorf("control: Costliest(10) has %d drafts, want the 4 evaluated", n)
	}
}
