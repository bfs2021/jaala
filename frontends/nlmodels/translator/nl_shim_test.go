package translator_test

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/frontends/nlmodels/internal/modeltest"
	"github.com/panyam/jaala/internal/guide"
	"github.com/panyam/jaala/ns"
	"github.com/panyam/jaala/stdlib"
)

// The tests in this package are nl.Translate's, moved here when the translator's loop moved onto
// chakra Runners and nl.Translate went (#496). They drive a Translator through translate, which takes
// what nl.Translate took, with the fallback as its last argument. Their models are still nl.Models
// that record what they were asked as nl.Prompts, run as chakra Providers by modeltest, so a test
// reads what a model was asked as it did before.

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// scripted is a Model replying with its replies in turn, an error for "!fail", recording each Prompt
// (frontends/nl's fixture).
type scripted struct {
	name    string
	replies []string
	asked   []nl.Prompt
	offered [][]string        // the tools each request offered, as asked
	descs   map[string]string // the tools' descriptions, as last offered
}

func (s *scripted) Offer(names []string, descs map[string]string) {
	s.offered = append(s.offered, names)
	s.descs = descs
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

// byQuestion is a Model answering each question with its own drafts in turn, as a model that makes
// the same mistakes every run would. Its tokens are the prompt's and the reply's length over four.
type byQuestion struct {
	name   string
	drafts map[string][]string
	asked  map[string]int
}

func (m *byQuestion) Name() string { return m.name }

func (m *byQuestion) Complete(_ context.Context, p nl.Prompt) (nl.Completion, error) {
	q, _, _ := strings.Cut(p.Turns[0].Text, "\n")
	ds := m.drafts[q]
	n := m.asked[q]
	m.asked[q]++
	if n >= len(ds) {
		return nl.Completion{}, errors.New("no draft left")
	}
	in := len(p.Prefix)
	for _, t := range p.Turns {
		in += len(t.Text)
	}
	return nl.Completion{Text: ds[n], TokensIn: in / 4, TokensCached: len(p.Prefix) / 4, TokensOut: len(ds[n]) / 4}, nil
}

func drafter(name string, drafts map[string][]string) *byQuestion {
	return &byQuestion{name: name, drafts: drafts, asked: map[string]int{}}
}

func kinds(tr nl.Trace) []string {
	var out []string
	for _, t := range tr.Tries {
		out = append(out, t.Kind)
	}
	return out
}

func modes(tr nl.Trace) []string {
	var out []string
	for _, t := range tr.Tries {
		out = append(out, t.Mode)
	}
	return out
}

// vocabulary is a small graph with docs and types, the standard vocabulary, and two modules: one
// whose relation infers a type and takes an input, and one nested under it (frontends/nl's fixture).
func vocabulary(t *testing.T) *ns.Vocabulary {
	t.Helper()
	node := ns.ArgType{Kind: "node"}
	src := ns.NewMemSource().
		DeclareSchema("edge", ns.Schema{Arity: 2, Labels: []string{"from", "to"}, Types: []ns.ArgType{node, node},
			Doc: "A directed edge between two nodes.\nThis second line stays out of the card."}).
		DeclareSchema("node", ns.Schema{Arity: 1, Labels: []string{"name"}, Types: []ns.ArgType{node}, Doc: "Every node."}).
		DeclareSchema("weight", ns.Schema{Arity: 2, Labels: []string{"node", "w"}, Types: []ns.ArgType{node, {Type: ns.TypeNumber}}})
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
	for _, m := range [][2]string{
		{"graph", `# reach is every node a node gets to by edges.
reach(?a, ?b) :- edge(?a, ?b);
reach(?a, ?c) :- reach(?a, ?b), edge(?b, ?c);
# heavy is a node weighing more than min.
heavy(?n, ?w, ?min) :- weight(?n, ?w), ?w > ?min;
`},
		{"graph.loops", `self(?n: node) :- edge(?n, ?n);`},
	} {
		if err := v.AddModule(m[0], datalog.LanguageName, m[1], ""); err != nil {
			t.Fatal(err)
		}
	}
	return v
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

// shout is a toy front end: a draft is "SAY " and a jaala program. "WHISPER" is a construct it
// refuses as a gap, and anything else without SAY is a compile error. Its explanations say so, so a
// test can tell them from nl.Explain's.
type shout struct{ examples []nl.Example }

func (shout) Language() string { return "shout" }
func (shout) Card(*ns.Vocabulary) (string, error) {
	return "# SHOUT CARD: edge(from, to), node(name)\n", nil
}
func (shout) Instructions() string     { return "# Say SAY, then the program.\n" }
func (s shout) Examples() []nl.Example { return s.examples }
func (shout) Grammar(*ns.Vocabulary) (string, error) {
	return `root ::= "SAY " [a-z(", ?=>)]+` + "\n", nil
}
func (shout) Compile(_ *ns.Vocabulary, text string) (nl.Compiled, error) {
	c := nl.Compiled{Explain: func(err error) string { return "SHOUT ERROR: " + err.Error() }}
	switch {
	case strings.HasPrefix(text, "WHISPER"):
		c.Kind = "gap"
		return c, errors.New("whispering isn't supported yet")
	case !strings.HasPrefix(text, "SAY "):
		return c, errors.New("a draft starts with SAY")
	}
	c.Program = strings.TrimPrefix(text, "SAY ")
	q, err := datalog.Parse(c.Program)
	c.Query = q
	return c, err
}

// edges is a pack whose one callable is edge, described in the host's words.
func edges() *nl.Pack {
	return &nl.Pack{About: "a small graph", Examples: []nl.Example{{Question: "what reaches a?", Program: `graph.reach(?x, "a") => ?x`}}, Callables: []nl.Callable{
		{Relation: "edge", Doc: "where an edge leads", Args: []string{"the node it starts at", "the node it leads to"}},
	}}
}

// depsBase is the guide's example in miniature (internal/guide).
func depsBase(t *testing.T) (*ns.Vocabulary, *datalog.Base) { return guide.Deps(t) }

func nlCases(cs []guide.Case) []nl.Case {
	out := make([]nl.Case, len(cs))
	for i, c := range cs {
		out[i] = nl.Case{Name: c.Name, Question: c.Question, Want: c.Want}
	}
	return out
}

// evalCases are the guide's questions, which the Cypher front end's TestEval asks too.
var evalCases = nlCases(guide.Cases)

// translate is what nl.Translate was, on a Translator (modeltest.Translate).
func translate(ctx context.Context, q string, v *ns.Vocabulary, g nl.Grounder, m nl.Model, o nl.Options, fallback ...nl.Model) (nl.Program, nl.Trace, error) {
	return modeltest.Translate(ctx, q, v, g, m, o, fallback...)
}
