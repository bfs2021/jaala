package nl_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/ns"
	"github.com/panyam/jaala/stdlib"
)

// partsBase holds thirteen parts of two kinds, ratings whose first argument repeats, a number with a
// unit, an absent value and a long one, and a relation with no facts.
func partsBase(t *testing.T) *datalog.Base {
	t.Helper()
	src := ns.NewMemSource().
		DeclareSchema("part", ns.Schema{Arity: 2, Labels: []string{"ref", "kind"}}).
		DeclareSchema("rating", ns.Schema{Arity: 2, Labels: []string{"ref"}, Types: []ns.ArgType{{}, {Type: ns.TypeNumber}}}).
		Declare("note", "text")
	for i := 1; i <= 13; i++ {
		kind := "resistor"
		if i > 9 {
			kind = "capacitor"
		}
		src.Add("part", ns.Tuple{Vals: []ns.Value{ns.S(fmt.Sprintf("R%d", i)), ns.S(kind)}})
	}
	src.Add("rating", ns.Tuple{Vals: []ns.Value{ns.S("R1"), ns.NU(5, "V")}})
	src.Add("rating", ns.Tuple{Vals: []ns.Value{ns.S("R1"), ns.Absent()}})
	src.Add("rating", ns.Tuple{Vals: []ns.Value{ns.S(strings.Repeat("x", 45)), ns.NU(5, "V")}})
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
	b, err := datalog.NewBase(v, src)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The data card lists each relation's facts, in the Source's order, and each argument's values, commonest first, summarized
// past twelve.
func TestTheDataCardSaysWhatTheDataHolds(t *testing.T) {
	got, err := nl.DataCard(context.Background(), partsBase(t))
	if err != nil {
		t.Fatal(err)
	}
	want := `# The data: each relation's facts, and each argument's values with how many facts hold each (or how many values, and the commonest, when there are many).
# note(text): 0 facts
# part(ref, kind): 13 facts
#   ref: 13 values, commonest "R1" (1)
#   kind: "resistor" (9), "capacitor" (4)
# rating(ref, arg2): 3 facts
#   ref: "R1" (2), "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"… (1)
#   arg2: 5V (2), absent (1)
`
	if got != want {
		t.Errorf("card:\n%s\nwant:\n%s", got, want)
	}
	if _, err := nl.DataCard(context.Background(), partsBase(t), datalog.Budget(5)); err == nil || !strings.Contains(err.Error(), "counting part's ref") {
		t.Errorf("a budget too small: %v", err)
	}
}
