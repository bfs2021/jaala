package cypher_test

import (
	"testing"

	"github.com/panyam/jaala/frontends/cypher"
	"github.com/panyam/jaala/internal/guide"
)

// The card names the graph in Cypher's terms, one label or type a line, so the model reads every name
// it may use.
func TestTheFrontEndsCardIsTheGraph(t *testing.T) {
	v, _ := guide.Deps(t)
	card, err := cypher.NewFrontEnd(depsGraph).Card(v)
	if err != nil {
		t.Fatal(err)
	}
	want := `# The graph a Cypher query reads. Node labels, with their properties:
#   (:Package {license, name})
# Relationship types:
#   -[:IMPORTS]->
#   -[:DEPENDS_ON]->
`
	if card != want {
		t.Errorf("card:\n%s\nwant:\n%s", card, want)
	}
}
