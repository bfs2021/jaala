package nl

import (
	"strings"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/internal/gbnf"
)

// Every reply the pick grammar allows is none or a call of one of the callables that parses and
// checks, a closed domain's argument a variable or one of its values. control: each callable and none
// are reached, and a domain value is written.
func TestEveryCallThePickGrammarWritesChecks(t *testing.T) {
	v := grammarVocabulary(t)
	cs := []Callable{{Relation: "edge"}, {Relation: "kind"}, {Relation: "graph.reach"}}
	rules, err := pickRules(cs, v)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	var s *gbnf.Sampler
	for i := 0; i < 3000; i++ {
		if i%300 == 0 {
			s = gbnf.NewSampler(table(rules), int64(i), 3+i/300%5)
		}
		text := s.Sample("root")
		if text == ActionNone {
			seen[ActionNone]++
			continue
		}
		rel, _, _ := strings.Cut(text, "(")
		seen[rel]++
		if strings.HasPrefix(text, `kind(`) && (strings.HasSuffix(text, `, "function")`) || strings.HasSuffix(text, `, "type")`)) {
			seen["domain"]++
		}
		q, err := datalog.Parse(text)
		if err == nil {
			err = datalog.Validate(q, v)
		}
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
	}
	for _, k := range []string{ActionNone, "edge", "kind", "graph.reach", "domain"} {
		if seen[k] == 0 {
			t.Errorf("control: no sample reached %s (%v)", k, seen)
		}
	}
	if len(seen) != 5 {
		t.Errorf("the grammar wrote something else: %v", seen)
	}
}
