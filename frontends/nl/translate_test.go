package nl_test

import (
	"context"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/ns"
)

// graphBase is a Base over the test vocabulary with a three-node chain, a->b->c.
func graphBase(t *testing.T, v *ns.Vocabulary) *datalog.Base {
	t.Helper()
	src := ns.NewMemSource().Declare("edge", "from", "to").Declare("node", "name").Declare("weight", "node", "w")
	src.Add("edge", ns.Tuple{Vals: []ns.Value{ns.S("a"), ns.S("b")}})
	src.Add("edge", ns.Tuple{Vals: []ns.Value{ns.S("b"), ns.S("c")}})
	b, err := datalog.NewBase(v, src)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type grounder struct {
	cands []nl.Candidate
	err   error
	asked []string
}

func (g *grounder) Ground(_ context.Context, q string) ([]nl.Candidate, error) {
	g.asked = append(g.asked, q)
	return g.cands, g.err
}
