package nl_test

import (
	"context"
	"errors"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/ns"
)

// scripted is a Model replying with its replies in turn, an error for "!fail", recording each Prompt.
type scripted struct {
	name    string
	replies []string
	asked   []nl.Prompt
}

func (s *scripted) Name() string { return s.name }

func (s *scripted) Complete(_ context.Context, p nl.Prompt) (nl.Completion, error) {
	s.asked = append(s.asked, p)
	if len(s.asked) > len(s.replies) {
		return nl.Completion{}, errors.New("out of replies")
	}
	r := s.replies[len(s.asked)-1]
	if r == "!fail" {
		return nl.Completion{}, errors.New("server down")
	}
	return nl.Completion{Text: r, TokensIn: 100 * len(s.asked), TokensOut: len(r), TokensCached: 90}, nil
}

func model(name string, replies ...string) *scripted { return &scripted{name: name, replies: replies} }

func kinds(tr nl.Trace) []string {
	var out []string
	for _, t := range tr.Tries {
		out = append(out, t.Kind)
	}
	return out
}

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
