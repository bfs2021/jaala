package guide_test

import (
	"context"
	"errors"
	"fmt"
	agent "github.com/panyam/chakra"
	"strings"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/frontends/nlmodels/translator"
)

// Card lists the vocabulary a line per member, for a model's prompt. The
// same vocabulary always gives the same text, so it caches as a prefix.
func Example_card() {
	v, err := vocabulary(source())
	if err != nil {
		panic(err)
	}
	card, err := nl.Card(v)
	if err != nil {
		panic(err)
	}
	fmt.Print(card)
	// Output:
	// # Every relation and predicate a program can call, by module: name(arg: type, ...) # what it holds.
	// # ~type is inferred from the relation's rules, "in" marks an argument the caller must bind, [, arg] is optional.
	// # A predicate says what it needs bound before it runs: "filter" needs every argument.
	// absent(value) filter # reports whether the field carried no value at all, which is different from an empty string and from zero; `not absent(?x)` reads "this field is stated"
	// coalesce(value, fallback, result) needs value+fallback # binds the value, or the fallback when the value is absent
	// imports(from, to)
	// pkg(name, license)
	// graph.closure(relation, from, to, min: number, max: number, hops: number) needs relation+min+max # to is reachable from from in min to max steps of the two-argument relation named in quotes (max 0: no bound), hops the fewest
	// graph.paths(relation, from, to, mode, min: number, max: number, path) needs relation+mode+min+max # each path in min to max steps of the relation named in quotes, mode "trail", "acyclic", "simple", "shortest" or "walk"
	//
	// module deps
	// deps.depends_on(a, b) # Everything a package depends on, directly or through others.
	//
	// module list
	// list.at(list: list, index: number, value) needs list+index # binds the value at an index from 0, a negative one counting from the end (-1 is the last)
	// list.length(list: list, length: number) needs list # binds how many values the list holds
	// list.member(list: list, index: number, value) needs list # binds each value of the list with its index from 0; given a value, binds where it is
	//
	// module num
	// num.add(a: number, b: number, c: number) needs a+b or a+c or b+c # c = a + b, run from any two of the three; units follow the vocabulary's table
	// num.div(a: number, b: number, c: number) needs a+b or a+c or b+c # c = a / b, run from any two of the three; units follow the vocabulary's table
	// num.mul(a: number, b: number, c: number) needs a+b or a+c or b+c # c = a * b, run from any two of the three; units follow the vocabulary's table
	// num.neg(a: number, b: number) needs a or b # b = -a, run from either; the unit carries over
	// num.sub(a: number, b: number, c: number) needs a+b or a+c or b+c # c = a - b, run from any two of the three; units follow the vocabulary's table
	// num.unit(number: number, unit: string) needs number # binds the number's unit ("V", "A"), or absent for a number without one; the number must be bound
	// num.value(number: number, value: number) needs number # binds the number without its unit, so 5 V and 5 A both bind 5; the number must be bound
	//
	// module path
	// path.edge(path: string, index: number, edge) needs path # binds each edge of a path over a relation of three arguments, index from 0
	// path.edges(path: string, edges: list) needs path # binds the path's edges as a list
	// path.length(path: string, steps: number) needs path # binds how many steps the path takes
	// path.node(path: string, index: number, node) needs path # binds each node of the path, index from 0
	// path.nodes(path: string, nodes: list) needs path # binds the path's nodes as a list
	// path.step(path: string, index: number, from, to) needs path # binds each step of the path, index from 0
	//
	// module str
	// str.capitalize(string: string, capitalized: string) needs string # binds the string with its first character in upper case and the rest as written
	// str.concat(head: string, tail: string, text: string) needs head+tail or head+text or tail+text # text is head followed by tail, run from any two of the three; cutting a head or tail off holds for none when text lacks it
	// str.contains(string: string, substring: string) filter # reports whether a string contains a substring
	// str.distance(string: string, other: string, distance: number) needs string+other # binds the edit (Levenshtein) distance between two strings, in characters; both strings must be bound
	// str.find(string: string, regex: string, index: number, match: string) needs string+regex # binds each non-empty match of an unanchored regular expression with its index from 0; given an index, binds that match alone, a negative index counting from the end
	// str.glob(string: string, pattern: string) filter # reports whether the whole string matches a SQLite-style glob (`*` any run, `?` one character, `[a-z]` or `[^a-z]` one of a class, `[[]` a literal `[`)
	// str.lower(string: string, lower: string) needs string # binds the string in lower case
	// str.match(string: string, regex: string) filter # reports whether the string matches an unanchored regular expression
	// str.prefix(string: string, prefix: string) filter # reports whether a string starts with a prefix
	// str.replace(string: string, old: string, new: string, result: string) needs string+old+new # binds the string with every occurrence of old replaced by new
	// str.split(string: string, separator: string, index: number, part: string) needs string+separator # binds each part of the string between separators with its index from 0; given an index, binds that part alone, a negative index counting from the end (-1 is the last part)
	// str.suffix(string: string, suffix: string) filter # reports whether a string ends with a suffix
	// str.upper(string: string, upper: string) needs string # binds the string in upper case
}

// Explain turns a program's error into what a model fixes it from: the
// message, the place, and the fix. The CheckError says the same as data.
func Example_explainAnError() {
	v, err := vocabulary(source())
	if err != nil {
		panic(err)
	}
	for _, text := range []string{
		"depends_on(?a, ?b) => ?b",
		"deps.depends_on(?a, ?b),\n  pkg(?b) => ?b",
	} {
		err := datalog.Validate(datalog.MustParse(text), v)
		fmt.Println(nl.Explain(text, err))
		var ce *datalog.CheckError
		if errors.As(err, &ce) {
			fmt.Printf("kind %s, about %s\n\n", ce.Kind, ce.Relation)
		}
	}
	// Output:
	// query: unknown relation "depends_on"; did you mean "deps.depends_on"?
	// line 1, column 1:
	//   depends_on(?a, ?b) => ?b
	//   ^
	// fix: did you mean "deps.depends_on"?
	// kind unknown, about depends_on
	//
	// query: relation "pkg" takes 2 args, got 1
	// line 2, column 3:
	//     pkg(?b) => ?b
	//     ^
	// fix: pkg(name, license)
	// kind arity, about pkg
}

// scripted stands in for a model: chakra's StubProvider replying with the
// drafts in turn. A host's provider asks a real one, a local model through
// chakra's llama.cpp preset or a hosted one.
func scripted(drafts ...string) *agent.StubProvider {
	var turns []agent.StubTurn
	for _, d := range drafts {
		turns = append(turns, agent.StubTurn{Text: d})
	}
	return agent.NewStubProvider(turns...)
}

// told prints what the model was told after each refused draft: the last
// message of every request after the first.
func told(model *agent.StubProvider) {
	for _, req := range model.Requests()[1:] {
		fmt.Printf("told: %s\n\n", req.Messages[len(req.Messages)-1].Text)
	}
}

// The translator asks a model for a program, checks it, and hands each
// error back until one checks. With a Base it runs the program too, so an
// empty answer can go to a fallback model.
func Example_translate() {
	v, err := vocabulary(source())
	if err != nil {
		panic(err)
	}
	model := scripted(
		`depends_on("app", ?d) => ?d`,
		"```\ndeps.depends_on(\"app\", ?d) => ?d\n```",
	)
	b := datalog.MustBase(v, source())
	t, err := translator.New(v, nil, translator.Config{
		Model: translator.Role{Provider: model, Label: "scripted"},
		Base:  b,
	})
	if err != nil {
		panic(err)
	}
	p, tr, err := t.Ask(context.Background(), "what does app depend on?")
	if err != nil {
		fmt.Println(err)
		return
	}
	told(model)
	for _, t := range tr.Tries {
		fmt.Printf("try by %s: %q\n", t.Model, t.Kind)
	}
	fmt.Println(p.Text)
	for _, r := range p.Rows {
		fmt.Print(r.Bind["d"].S, " ")
	}
	fmt.Println()
	// Output:
	// told: query: unknown relation "depends_on"; did you mean "deps.depends_on"?
	// line 1, column 1:
	//   depends_on("app", ?d) => ?d
	//   ^
	// fix: did you mean "deps.depends_on"?
	//
	// Write the whole program again, fixed.
	//
	// try by scripted: "unknown"
	// try by scripted: ""
	// deps.depends_on("app", ?d) => ?d
	// api log util
}

// Grammar is the program syntax as GBNF, for a decoder to keep a model's
// output to text that parses. Its first rules say what a program is.
func Example_grammar() {
	for _, line := range strings.SplitN(nl.Grammar(), "\n", 4)[:3] {
		fmt.Println(line)
	}
	// Output:
	// root ::= (rule sep)* goal
	// sep ::= "; " | ";\n"
	// rule ::= head " :- " literals
}

// Eval scores a translator over questions with known answers: a row
// matches by its values, whatever columns the program chose.
func Example_eval() {
	v, err := vocabulary(source())
	if err != nil {
		panic(err)
	}
	b := datalog.MustBase(v, source())
	// the drafts for each case in turn: one for direct, two each for the others
	model := scripted(
		`imports("app", ?d) => ?d`,
		`pkg("util") => ?l`, `pkg("util", ?l) => ?l`,
		`imports(?p, log) => ?p`, `imports(?p, "log") => ?p`,
	)
	t, err := translator.New(v, nil, translator.Config{Model: translator.Role{Provider: model}})
	if err != nil {
		panic(err)
	}
	cases := []nl.Case{
		{Name: "direct", Question: "what does app import?", Want: [][]string{{"api"}}},
		{Name: "license", Question: "what license does util have?", Want: [][]string{{"Apache-2.0"}}},
		{Name: "importers", Question: "which packages import log?", Want: [][]string{{"api"}}},
	}
	rep := nl.Eval(context.Background(), cases, b, t.Ask)
	fmt.Print(rep)
	// Output:
	// case       pass  tries  refused
	// direct     yes   1
	// license    yes   2      arity
	// importers  yes   2      parse
	// passed 3/3, 1 on the first try; 5 tries, tokens 0 in (0 cached), 0 out
	// refused: arity 1, parse 1; check errors with a fix 1/1
}
