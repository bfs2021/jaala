package nl_test

import (
	"strings"
	"testing"

	"github.com/panyam/jaala/frontends/nl"
)

// edges is a pack whose one callable is edge, described in the host's words.
func edges() *nl.Pack {
	return &nl.Pack{About: "a small graph", Examples: []nl.Example{{Question: "what reaches a?", Program: `graph.reach(?x, "a") => ?x`}}, Callables: []nl.Callable{
		{Relation: "edge", Doc: "where an edge leads", Args: []string{"the node it starts at", "the node it leads to"}},
	}}
}

// A pack's callable must be a relation of the vocabulary, described at its arity.
func TestAPacksCallablesAreChecked(t *testing.T) {
	v := vocabulary(t)
	for _, c := range []struct {
		c    nl.Callable
		want string
	}{
		{nl.Callable{Relation: "edges"}, "callable 1 names edges, which isn't a relation of the vocabulary"},
		{nl.Callable{Relation: "graph"}, "callable 1 names graph, which isn't a relation of the vocabulary"},
		{nl.Callable{Relation: "edge", Args: []string{"from"}}, "callable edge describes 1 arguments, and it takes 2"},
	} {
		err := (&nl.Pack{Callables: []nl.Callable{c.c}}).Validate(v)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: %v, want %q", c.c, err, c.want)
		}
	}
	if err := (&nl.Pack{Callables: []nl.Callable{{Relation: "graph.reach"}, {Relation: "edge", Args: []string{"a", "b"}}}}).Validate(v); err != nil {
		t.Errorf("control: %v", err)
	}
}
