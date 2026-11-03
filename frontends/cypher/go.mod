module github.com/panyam/jaala/frontends/cypher

go 1.26.4

require (
	github.com/panyam/jaala v0.1.90
	github.com/panyam/jaala/frontends/nl v0.1.90
)

replace github.com/panyam/jaala => ../../

replace github.com/panyam/jaala/frontends/nl => ../../frontends/nl
