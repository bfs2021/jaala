package nl

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/panyam/jaala/ns"
)

// A Callable is a relation of the host's that answers a kind of question on its own once its arguments
// are filled (#484): callers of a function, tests reaching a file, routes in a repo. With Pack.Callables
// set, Translate first asks the model to pick one and fill its arguments, a much smaller reply than a
// program and one a grammar holds to the catalogue, and drafts a program only when none fits.
type Callable struct {
	// Relation is the member's path in the vocabulary, base or derived.
	Relation string
	// Doc says what a call answers, in the host's words. "" uses the first line of the member's own
	// doc, as the card does.
	Doc string
	// Args says what each argument is, one line each in the member's order, so the model knows which
	// to fill from the question and which to leave a variable. nil uses the members' argument names.
	Args []string
}

// ActionNone is a Step's Action for a pick the model declined, replying none: no relation answers the
// question, and Translate drafts a program.
const ActionNone = "none"

// pickTries is how many calls a model may pick before Translate drafts a program instead: the first,
// and one more after its refusal comes back.
const pickTries = 2

// validateCallables checks every callable names a member of v that isn't a module, with as many
// argument lines as the member takes, or none.
func validateCallables(cs []Callable, v *ns.Vocabulary) error {
	for i, c := range cs {
		e, err := v.Lookup(c.Relation)
		if err != nil || e.Kind == ns.EntryModule {
			return fmt.Errorf("nl: the pack's callable %d names %s, which isn't a relation of the vocabulary", i+1, c.Relation)
		}
		if c.Args != nil && len(c.Args) != len(e.Args) {
			return fmt.Errorf("nl: the pack's callable %s describes %d arguments, and it takes %d", c.Relation, len(c.Args), len(e.Args))
		}
	}
	return nil
}

// pickPrefix is the pick's Prefix: what to reply, the pack's About and Card, then each callable as a
// call of variables named for its arguments, with its doc and a line per argument.
func pickPrefix(cs []Callable, v *ns.Vocabulary, p *Pack) (string, error) {
	var b strings.Builder
	b.WriteString(p.prompts().Pick)
	about := *p
	about.Examples = nil
	b.WriteString(about.Prefix())
	b.WriteString("\n# Relations:\n")
	for _, c := range cs {
		e, err := v.Lookup(c.Relation)
		if err != nil {
			return "", err
		}
		names := make([]string, len(e.Args))
		for i, a := range e.Args {
			names[i] = a.Name
			if names[i] == "" {
				names[i] = fmt.Sprintf("arg%d", i+1)
			}
		}
		doc := strings.TrimSpace(c.Doc)
		if doc == "" {
			doc, _, _ = strings.Cut(strings.TrimSpace(e.Doc), "\n") // its first line, as the card shows it
		}
		fmt.Fprintf(&b, "# %s(?%s)", c.Relation, strings.Join(names, ", ?"))
		if doc != "" {
			b.WriteString(": " + strings.Join(strings.Fields(doc), " "))
		}
		b.WriteString("\n")
		for i, a := range c.Args {
			if a = strings.Join(strings.Fields(a), " "); a != "" {
				fmt.Fprintf(&b, "#   ?%s: %s\n", names[i], a)
			}
		}
	}
	return b.String(), nil
}

// pickInstructions open the pick's prefix.
const pickInstructions = `# Answer the question with one call to one of the relations below, or reply none when none of them answers it.
# Write each argument as a value the question names, quoted ("pkg/a"), or as a variable (?name) for what the answer lists.
`

// pickGrammar holds a reply to none or one call of a callable, at its arity, each argument a term, or
// for a closed domain a variable or one of its values, as GrammarFor writes them.
func pickGrammar(cs []Callable, v *ns.Vocabulary) (string, error) {
	rules, err := pickRules(cs, v)
	if err != nil {
		return "", err
	}
	return render(rules), nil
}

// pickRules are pickGrammar's rules, rooted at root.
func pickRules(cs []Callable, v *ns.Vocabulary) ([]rule, error) {
	var extra []rule
	choices := alt{lit(ActionNone)}
	for i, c := range cs {
		e, err := v.Lookup(c.Relation)
		if err != nil {
			return nil, err
		}
		m := callee{path: c.Relation, kind: e.Kind}
		for _, a := range e.Args {
			var dom []string
			for _, d := range a.Domain {
				if !strings.ContainsAny(d, "\"\\") && !strings.ContainsFunc(d, func(r rune) bool { return r < 0x20 }) {
					dom = append(dom, d)
				}
			}
			m.domains = append(m.domains, dom)
		}
		name := fmt.Sprintf("c%d", i)
		extra = append(extra, rule{name, seq{lit(c.Relation + "("), m.args(name, len(e.Args), &extra), lit(")")}})
		choices = append(choices, ref(name))
	}
	var out []rule
	for _, r := range programRules() {
		if r.name == "root" {
			r.e = choices
		}
		out = append(out, r)
	}
	return reachable(append(out, extra...)), nil
}

// pickCall asks model to answer the question with one of the pack's callables (#484), up to pickTries
// times, each refusal going back as a draft's does. It returns the accepted program, or false when the
// model replied none or no call it picked was accepted; Translate then drafts a program. Each call is
// a Try with Mode "call", and a none a Step. A pick is jaala's text whatever o.FrontEnd is.
func pickCall(ctx context.Context, ask string, v *ns.Vocabulary, model Model, o Options, tr *Trace, spend *spending) (Program, Prompt, bool, error) {
	cs := o.Pack.Callables
	prefix, err := pickPrefix(cs, v, o.Pack)
	if err != nil {
		return Program{}, Prompt{}, false, err
	}
	grammar := ""
	if !o.NoGrammar {
		if grammar, err = pickGrammar(cs, v); err != nil {
			return Program{}, Prompt{}, false, err
		}
	}
	o.FrontEnd, o.Actions = nil, nil
	turns := []Turn{{RoleUser, ask}}
	refused := map[string]error{}
	for t := 0; t < pickTries; t++ {
		if err := ctx.Err(); err != nil {
			return Program{}, Prompt{}, false, err
		}
		if err := spend.check(); err != nil {
			return Program{}, Prompt{}, false, err
		}
		prompt := Prompt{Prefix: prefix, Turns: turns, Grammar: grammar}
		try := Try{Model: model.Name(), Mode: ModeCall}
		start := time.Now()
		c, err := model.Complete(ctx, prompt)
		try.Latency = time.Since(start)
		if err != nil {
			try.Err, try.Kind = fmt.Errorf("model %s: %w", try.Model, err), KindModel
			tr.Tries = append(tr.Tries, try)
			return Program{}, Prompt{}, false, nil
		}
		spend.add(c.TokensIn, c.TokensOut)
		try.TokensIn, try.TokensOut, try.TokensCached = c.TokensIn, c.TokensOut, c.TokensCached
		try.Text = unfence(c.Text)
		if try.Text == ActionNone {
			tr.Steps = append(tr.Steps, Step{Model: try.Model, Action: ActionNone, Text: try.Text, Result: "no relation answers it: drafting a program",
				After: len(tr.Tries), TokensIn: try.TokensIn, TokensOut: try.TokensOut, TokensCached: try.TokensCached, Latency: try.Latency})
			return Program{}, Prompt{}, false, nil
		}
		try.explain = func(err error) string { return Explain(try.Text, err) }
		var p Program
		if prev, ok := refused[try.Text]; ok {
			err = fmt.Errorf("the same draft as before: %w", prev)
			try.Err, try.Kind = err, KindRepeat
		} else {
			try, p, err = check(ctx, try, v, o, 0)
		}
		tr.Tries = append(tr.Tries, try)
		if err == nil {
			return p, prompt, true, nil
		}
		refused[try.Text] = err
		why := strings.TrimRight(try.explain(err), "\n")
		turns = append(turns, Turn{RoleModel, try.Text}, Turn{RoleUser, why + "\n\n" + o.Pack.prompts().PickRetry})
	}
	return Program{}, Prompt{}, false, nil
}

// ModeCall is a Try's Mode for a call the model picked from Pack.Callables (#484); a drafted program's
// is "".
const ModeCall = "call"
