package cypherarm_test

import "github.com/panyam/jaala/frontends/cypher"

// depsGraph is the guide's vocabulary (internal/guide) as a graph: packages keyed by name with their
// license as a column, and imports and the deps module's reach as relationship types.
var depsGraph = cypher.Graph{
	Nodes: []cypher.Node{{Label: "Package", Relation: "pkg", Key: 0, Props: map[string]int{"name": 0, "license": 1}}},
	Edges: []cypher.Edge{
		{Type: "IMPORTS", Relation: "imports", From: 0, To: 1},
		{Type: "DEPENDS_ON", Relation: "deps.depends_on", From: 0, To: 1},
	},
}
