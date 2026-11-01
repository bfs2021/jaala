package datalog

import (
	"testing"

	"github.com/panyam/jaala/ns"
)

// #478's program: a name read token by token, then a later token tested, through a rule as agni's
// classification reads it. str.find runs after what binds its string, as Naive needs.
func TestStrFindReadsANameTokenByToken(t *testing.T) {
	src := graph()
	src.Declare("net", "name")
	for _, n := range []string{"LPC_VCC", "VCC_MAIN", "CLK"} {
		src.Add("net", ns.Tuple{Vals: []ns.Value{ns.S(n)}})
	}
	r := std(src)
	if err := r.AddModule("n", LanguageName, `token(?n, ?i, ?t) :- net(?n), str.find(?n, "[A-Za-z0-9]+", ?i, ?t);
		later_supply(?n) :- token(?n, ?i, ?t), ?i >= 1, str.match(?t, "(?i)^V(CC|DD)");
		last(?n, ?t) :- net(?n), str.find(?n, "[A-Za-z0-9]+", -1, ?t);`, ""); err != nil {
		t.Fatal(err)
	}
	if got := col(evalReg(t, r, `n.later_supply(?n) => ?n`), "n"); got != "LPC_VCC" {
		t.Errorf("later_supply = %s, want LPC_VCC alone: VCC_MAIN's VCC is its first token", got)
	}
	if got := col(evalReg(t, r, `n.last(?n, ?t) => ?t`), "t"); got != "CLK,MAIN,VCC" {
		t.Errorf("last tokens = %s, want CLK,MAIN,VCC", got)
	}
	if got := col(evalReg(t, r, `n.token("CLK", ?i, ?t), ?i = 1 => ?t`), "t"); got != "" {
		t.Errorf("CLK has no second token, got %s", got)
	}
}
