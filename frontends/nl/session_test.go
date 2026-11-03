package nl_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/internal/guide"
)

func reply(text string) nl.Try { return nl.Try{Model: "small", Text: text} }

// verdict names which of a Verdict's outcomes is set, failing on a Verdict setting none or several.
func verdict(t *testing.T, vd nl.Verdict) string {
	t.Helper()
	var set []string
	if vd.Program != nil {
		set = append(set, "accept")
	}
	if vd.Continue != "" {
		set = append(set, "continue")
	}
	if vd.Stop != nil {
		set = append(set, "stop")
	}
	if len(set) != 1 {
		t.Fatalf("verdict sets %v: %+v", set, vd)
	}
	return set[0]
}

// A refused answer continues with what Explain says about it and the fix line, and an answer that
// checks is accepted, the fence taken off.
func TestAConversationContinuesWithWhatExplainSays(t *testing.T) {
	v := vocabulary(t)
	s, err := nl.NewSession(context.Background(), "q", v, nil, nl.Options{})
	if err != nil {
		t.Fatal(err)
	}
	c := s.NewConversation(nil, true)
	bad := `reach("a", ?b) => ?b`
	vd := c.Judge(context.Background(), reply(bad))
	if verdict(t, vd) != "continue" || len(vd.Tries) != 1 || vd.Tries[0].Kind != string(datalog.KindUnknown) {
		t.Fatalf("verdict %+v", vd)
	}
	if want := strings.TrimRight(nl.Explain(bad, vd.Tries[0].Err), "\n"); !strings.HasPrefix(vd.Continue, want+"\n\n") || !strings.HasSuffix(vd.Continue, "Write the whole program again, fixed.") {
		t.Errorf("continue %q, want %q and the fix line", vd.Continue, want)
	}
	vd = c.Judge(context.Background(), reply("```\ngraph.reach(\"a\", ?b) => ?b\n```"))
	if verdict(t, vd) != "accept" || vd.Program.Text != `graph.reach("a", ?b) => ?b` || vd.Program.Model != "small" {
		t.Errorf("verdict %+v", vd)
	}
}

// A repeated draft stops the turn, wrapping the first one's error. control: a different draft
// continues.
func TestAConversationStopsOnARepeat(t *testing.T) {
	v := vocabulary(t)
	s, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{})
	c := s.NewConversation(nil, true)
	c.Judge(context.Background(), reply(`nope(?a) => ?a`))
	vd := c.Judge(context.Background(), reply("```\nnope(?a) => ?a\n```"))
	var ce *datalog.CheckError
	if verdict(t, vd) != "stop" || vd.Tries[0].Kind != nl.KindRepeat || !errors.As(vd.Stop, &ce) || ce.Kind != datalog.KindUnknown {
		t.Errorf("verdict %+v", vd)
	}
	c = s.NewConversation(nil, true)
	c.Judge(context.Background(), reply(`nope(?a) => ?a`))
	if vd := c.Judge(context.Background(), reply(`nope(?b) => ?b`)); verdict(t, vd) != "continue" {
		t.Errorf("control: a different draft %+v", vd)
	}
}

// An empty answer stops the turn while another model is left, and that model's first message says
// so; with none left it continues with FixEmpty.
func TestAConversationStopsOnAnEmptyAnswerOnlyWhileAFallbackIsLeft(t *testing.T) {
	v := vocabulary(t)
	b := graphBase(t, v)
	s, _ := nl.NewSession(context.Background(), "where does c lead?", v, nil, nl.Options{Base: b})
	empty := `edge("c", ?b) => ?b`
	vd := s.NewConversation(nil, false).Judge(context.Background(), reply(empty))
	var ea *nl.EmptyAnswer
	if verdict(t, vd) != "stop" || !errors.As(vd.Stop, &ea) {
		t.Fatalf("verdict %+v", vd)
	}
	if ask := s.NewConversation(vd.Stop, true).Ask(); !strings.HasPrefix(ask, "where does c lead?\n\n") || !strings.Contains(ask, "answered nothing") {
		t.Errorf("the fallback's first message %q", ask)
	}
	if ask := s.NewConversation(errors.New("x"), true).Ask(); ask != "where does c lead?" {
		t.Errorf("control: after another error the first message is %q", ask)
	}
	vd = s.NewConversation(nil, true).Judge(context.Background(), reply(empty))
	if verdict(t, vd) != "continue" || !strings.Contains(vd.Continue, "other members or values") {
		t.Errorf("control: with no fallback left %+v", vd)
	}
}

// The first repeat of an empty draft continues, asked again and marked Reasked (#483); the second
// stops.
func TestAConversationAsksARepeatedEmptyDraftAgainOnce(t *testing.T) {
	v := vocabulary(t)
	s, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{Base: graphBase(t, v), MaxTries: 5})
	c := s.NewConversation(nil, true)
	empty := `edge("c", ?b) => ?b`
	c.Judge(context.Background(), reply(empty))
	vd := c.Judge(context.Background(), reply(empty))
	if verdict(t, vd) != "continue" || !vd.Tries[0].Reasked || vd.Tries[0].Kind != nl.KindRepeat || !strings.Contains(vd.Continue, "same program you sent before") {
		t.Fatalf("verdict %+v", vd)
	}
	if vd := c.Judge(context.Background(), reply(empty)); verdict(t, vd) != "stop" || vd.Tries[0].Reasked {
		t.Errorf("the second repeat %+v", vd)
	}
}

// A model's last try stops rather than continuing, with its refusal. control: the try before it
// continues.
func TestAConversationStopsWhenTheTriesRunOut(t *testing.T) {
	v := vocabulary(t)
	s, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{MaxTries: 2})
	c := s.NewConversation(nil, true)
	if vd := c.Judge(context.Background(), reply(`a(`)); verdict(t, vd) != "continue" {
		t.Fatalf("control: the first try %+v", vd)
	}
	vd := c.Judge(context.Background(), reply(`b(`))
	if verdict(t, vd) != "stop" || vd.Tries[0].Kind != nl.KindParse || vd.Stop != vd.Tries[0].Err {
		t.Errorf("the last try %+v", vd)
	}
	if s.MaxTries() != 2 {
		t.Errorf("MaxTries %d", s.MaxTries())
	}
}

// An answer the pack's Check refuses continues, as kind answer.
func TestAConversationContinuesWhenThePackRefusesTheAnswer(t *testing.T) {
	v := vocabulary(t)
	pack := &nl.Pack{Check: func(context.Context, nl.Program) error { return errors.New("not a list of nodes") }}
	s, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{Pack: pack})
	vd := s.NewConversation(nil, true).Judge(context.Background(), reply(`edge("a", ?b) => ?b`))
	if verdict(t, vd) != "continue" || vd.Tries[0].Kind != nl.KindAnswer || !strings.Contains(vd.Continue, "not a list of nodes") {
		t.Errorf("verdict %+v", vd)
	}
}

// A value an action showed repairs a later guess at its spelling: the verdict accepts the repair and
// records the model's own try before it. control: with no action taken, the guess goes back.
func TestAConversationAcceptsARepairOfWhatAnActionShowed(t *testing.T) {
	v, b := guide.Deps(t)
	s, _ := nl.NewSession(context.Background(), mitQ, v, nil, nl.Options{Base: b, Actions: &nl.Actions{}})
	guess := `pkg(?p, "mit") => ?p`
	if verdict(t, s.NewConversation(nil, true).Judge(context.Background(), reply(guess))) != "continue" {
		t.Fatalf("control: an unseen spelling is repaired")
	}
	if got := s.Act(context.Background(), nl.ActionValues, "pkg.license"); !strings.Contains(got, `"MIT"`) {
		t.Fatalf("values answered %q", got)
	}
	vd := s.NewConversation(nil, true).Judge(context.Background(), reply(guess))
	if verdict(t, vd) != "accept" || vd.Program.Text != mit || len(vd.Tries) != 2 || vd.Tries[0].Text != guess || vd.Tries[1].Repair != `"mit" to "MIT"` {
		t.Errorf("verdict %+v", vd)
	}
}

// Compact holds each action taken and each refusal; a new conversation starts it afresh.
func TestAConversationCompactsWhatItWasTold(t *testing.T) {
	v, b := guide.Deps(t)
	s, _ := nl.NewSession(context.Background(), mitQ, v, nil, nl.Options{Base: b, Actions: &nl.Actions{Compact: true}})
	c := s.NewConversation(nil, true)
	c.Act(context.Background(), nl.ActionLook, "pkg")
	c.Judge(context.Background(), reply(`pkg(?p, "nope") => ?p`))
	got := c.Compact()
	if len(got) != 1 || !strings.HasPrefix(got[0].Text, mitQ) || !strings.Contains(got[0].Text, "- look pkg") || !strings.Contains(got[0].Text, `refused (empty)`) {
		t.Errorf("compact %+v", got)
	}
	if got := s.NewConversation(nil, true).Compact(); len(got) != 1 || got[0].Text != mitQ {
		t.Errorf("a new conversation's compact %+v", got)
	}
}

// Two draws answering the same rows key alike though their text differs, a refused draw comes back
// with its error, and without a Base the key is the program.
func TestADrawIsKeyedByItsRows(t *testing.T) {
	v := vocabulary(t)
	s, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{Base: graphBase(t, v)})
	_, a, err := s.Draw(context.Background(), reply(`edge("a", ?b) => ?b`))
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := s.Draw(context.Background(), reply(`edge(?a, "c") => ?a`))
	if err != nil {
		t.Fatal(err)
	}
	_, c, err := s.Draw(context.Background(), reply(`edge("b", ?b) => ?b`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Key(a) != s.Key(b) || s.Key(a) == s.Key(c) {
		t.Errorf("keys %q %q %q", s.Key(a), s.Key(b), s.Key(c))
	}
	if try, _, err := s.Draw(context.Background(), reply(`nope(?a) => ?a`)); err == nil || try.Kind != string(datalog.KindUnknown) {
		t.Errorf("a refused draw: %v %+v", err, try)
	}
	plain, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{})
	if plain.Key(a) == plain.Key(b) {
		t.Errorf("control: without a Base two programs keyed alike")
	}
}

// A pack with Callables has a pick: none stops with ErrNoCall and no try, and a pick is a call try.
// control: a pack without Callables has none.
func TestAPickStopsOnNone(t *testing.T) {
	v := vocabulary(t)
	s, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{Base: graphBase(t, v), Pack: edges()})
	pick, err := s.Pick()
	if err != nil || pick == nil || !strings.Contains(pick.Prefix(), "# Answer the question with one call") || !strings.Contains(pick.Grammar(), `"none"`) {
		t.Fatalf("pick %v %v", pick, err)
	}
	vd := pick.NewConversation(nil, true).Judge(context.Background(), reply("none"))
	if verdict(t, vd) != "stop" || !errors.Is(vd.Stop, nl.ErrNoCall) || len(vd.Tries) != 0 {
		t.Errorf("none %+v", vd)
	}
	vd = pick.NewConversation(nil, true).Judge(context.Background(), reply(`edge("a", ?to)`))
	if verdict(t, vd) != "accept" || vd.Tries[0].Mode != nl.ModeCall {
		t.Errorf("a pick %+v", vd)
	}
	plain, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{Pack: &nl.Pack{About: "a small graph"}})
	if p, err := plain.Pick(); p != nil || err != nil {
		t.Errorf("control: a pack without callables picks: %v %v", p, err)
	}
}

// A conversation counts its actions: the same one asked twice opens with AskedAgain, the last one
// MaxSteps allows ends with LastLook, and a new conversation starts the count again.
func TestAConversationCountsItsActions(t *testing.T) {
	v, b := guide.Deps(t)
	d := nl.DefaultPrompts()
	s, _ := nl.NewSession(context.Background(), mitQ, v, nil, nl.Options{Base: b, Actions: &nl.Actions{Steps: 2}})
	c := s.NewConversation(nil, true)
	first := c.Act(context.Background(), nl.ActionLook, "pkg")
	if strings.HasPrefix(first, d.AskedAgain) || strings.HasSuffix(first, d.LastLook) || c.Steps() != 1 {
		t.Errorf("first look %q, steps %d", first, c.Steps())
	}
	again := c.Act(context.Background(), nl.ActionLook, "pkg")
	if again != d.AskedAgain+" "+first+"\n\n"+d.LastLook || c.Steps() != 2 {
		t.Errorf("second look %q, steps %d", again, c.Steps())
	}
	if c := s.NewConversation(nil, true); c.Steps() != 0 || strings.HasPrefix(c.Act(context.Background(), nl.ActionLook, "pkg"), d.AskedAgain) {
		t.Errorf("a new conversation carried the count")
	}
}

// Tools are the actions to offer as tools, find only with a Grounder, values naming the front end's
// target; with AsTools the prefix and grammar leave the text actions out. control: without AsTools
// they're in.
func TestASessionOffersItsActionsAsTools(t *testing.T) {
	v := vocabulary(t)
	g := &grounder{cands: []nl.Candidate{{Name: "a", Values: []string{"a"}}}}
	s, _ := nl.NewSession(context.Background(), "q", v, g, nl.Options{Actions: &nl.Actions{AsTools: true}})
	var names []string
	for _, tl := range s.Tools() {
		names = append(names, tl.Action+"("+tl.Arg+")")
	}
	if got := strings.Join(names, " "); got != "look(name) values(target) try(draft) find(name)" {
		t.Errorf("tools %s", got)
	}
	if d := s.Tools()[1].Description; !strings.Contains(d, "relation.argument") {
		t.Errorf("values' description %q", d)
	}
	if strings.Contains(s.Prefix(), "#   look NAME") || s.ActionGrammar() != s.Grammar() {
		t.Errorf("AsTools left the text actions in")
	}
	plain, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{Actions: &nl.Actions{}})
	if len(plain.Tools()) != 3 || !strings.Contains(plain.Prefix(), "#   look NAME") || plain.ActionGrammar() == plain.Grammar() {
		t.Errorf("control: without AsTools, %d tools", len(plain.Tools()))
	}
	if none, _ := nl.NewSession(context.Background(), "q", v, nil, nl.Options{}); none.Tools() != nil {
		t.Errorf("tools without Actions")
	}
}
