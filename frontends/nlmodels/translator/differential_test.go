package translator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/internal/guide"
)

// script answers each question with its own replies in turn, as a model making the same mistakes
// every run would, keyed by the first line of the conversation's first message. It is an nl.Model
// for nl.Translate and an agent.Provider for the Runner loop, so the two loops can be held to one
// script (#496). A reply that is a text action (look NAME, values TARGET, find NAME, try and a draft)
// is a tool call when the request offers tools, and text otherwise. A guided request offering no
// tools right after a text answer to a request that offered them is chakra's finalizing call, and
// gets that answer again.
type script struct {
	name    string
	replies map[string][]string
	mu      sync.Mutex
	asked   map[string]int
	final   map[string]string // the text answer a finalizing call repeats, per question
	tooled  int               // requests that offered tools
	longest int               // the most messages one request carried
	asks    []string          // each request as the model read it: instructions, grammar, messages
}

// ask is a request as the model reads it, the same for nl's Prompt and chakra's ProviderRequest.
func ask(instructions, grammar string, msgs [][2]string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "instructions %q\ngrammar %d bytes %x\n", instructions, len(grammar), len(grammar))
	for _, m := range msgs {
		fmt.Fprintf(&b, "%s: %q\n", m[0], m[1])
	}
	return b.String()
}

func scripted(name string, replies map[string][]string) *script {
	return &script{name: name, replies: replies, asked: map[string]int{}, final: map[string]string{}}
}

func (s *script) next(q string) (string, error) {
	n := s.asked[q]
	s.asked[q]++
	if rs := s.replies[q]; n < len(rs) {
		return rs[n], nil
	}
	return "", errors.New("no reply left")
}

// Name and Complete are the nl.Model.
func (s *script) Name() string { return s.name }

func (s *script) Complete(_ context.Context, p nl.Prompt) (nl.Completion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var msgs [][2]string
	for _, t := range p.Turns {
		role := "user"
		if t.Role == nl.RoleModel {
			role = "assistant"
		}
		msgs = append(msgs, [2]string{role, t.Text})
	}
	s.asks = append(s.asks, ask(p.Prefix, p.Grammar, msgs))
	q, _, _ := strings.Cut(p.Turns[0].Text, "\n")
	r, err := s.next(q)
	return nl.Completion{Text: r}, err
}

// Generate, Stream and ProviderCapabilities are the agent.Provider, which reports llama.cpp's
// capabilities so the grammar goes with every answer.
func (s *script) Generate(_ context.Context, req agent.ProviderRequest) (*agent.ProviderResponse, error) {
	t, err := s.turn(req)
	if err != nil {
		return nil, err
	}
	return &agent.ProviderResponse{Text: t.Text, ToolCalls: t.ToolCalls}, nil
}

func (s *script) Stream(ctx context.Context, req agent.ProviderRequest) (agent.Stream, error) {
	t, err := s.turn(req)
	if err != nil {
		return nil, err
	}
	return agent.NewStubProvider(t).Stream(ctx, req)
}

func (s *script) ProviderCapabilities() agent.Capabilities { return llamaCaps }

func (s *script) turn(req agent.ProviderRequest) (agent.StubTurn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(req.Tools) > 0 {
		s.tooled++
	}
	s.longest = max(s.longest, len(req.Messages))
	var msgs [][2]string
	for _, m := range req.Messages {
		msgs = append(msgs, [2]string{string(m.Role), m.Text})
	}
	grammar := ""
	if req.Guided != nil {
		grammar = req.Guided.Grammar
	}
	s.asks = append(s.asks, ask(req.Instructions, grammar, msgs))
	q, _, _ := strings.Cut(req.Messages[0].Text, "\n")
	if text, ok := s.final[q]; ok && req.Guided != nil && len(req.Tools) == 0 {
		delete(s.final, q)
		return agent.StubTurn{Text: text}, nil
	}
	delete(s.final, q)
	r, err := s.next(q)
	if err != nil {
		return agent.StubTurn{}, err
	}
	if len(req.Tools) == 0 {
		return agent.StubTurn{Text: r}, nil
	}
	if name, arg, ok := textAction(r); ok {
		key := map[string]string{nl.ActionLook: "name", nl.ActionFind: "name", nl.ActionValues: "target", nl.ActionTry: "draft"}[name]
		var tc agent.ToolCall
		raw, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("call-%d", s.asked[q]), "name": name, "args": map[string]string{key: arg}})
		if err := json.Unmarshal(raw, &tc); err != nil {
			return agent.StubTurn{}, err
		}
		return agent.StubTurn{ToolCalls: []agent.ToolCall{tc}}, nil
	}
	s.final[q] = r
	return agent.StubTurn{Text: r}, nil
}

// textAction reads a reply as nl's text actions are read.
func textAction(r string) (action, arg string, ok bool) {
	first, rest, _ := strings.Cut(r, "\n")
	word, inline, _ := strings.Cut(strings.TrimSpace(first), " ")
	switch word {
	case nl.ActionLook, nl.ActionValues, nl.ActionFind:
		return word, strings.TrimSpace(inline), rest == "" && strings.TrimSpace(inline) != ""
	case nl.ActionTry:
		d := strings.TrimSpace(strings.TrimSpace(inline) + "\n" + rest)
		return word, d, d != ""
	}
	return "", "", false
}

// arm is one scripted configuration both loops run: the replies each model gives, and the options.
type arm struct {
	name         string
	small, big   map[string][]string
	pack         *nl.Pack
	opts         func(b *datalog.Base) nl.Options // without Fallback, which the arm's big replies decide
	cases        []nl.Case
	wantFirstTry int // the reference's first-try passes, so a broken script can't pass by agreeing
	wantPassed   int
	// actions sets the arm apart from the prompt comparison: offered as tools, the actions change
	// the prefix and the conversation's shape on purpose.
	actions bool
}

// The guide's questions with TestEval's drafting replies (frontends/nl's eval_test.go).
var (
	smallDrafts = map[string][]string{
		"what does app import directly?":            {`imports("app", ?d) => ?d`},
		"what does app depend on, directly or not?": {`depends_on("app", ?d) => ?d`, `deps.depends_on("app", ?d) => ?d`},
		"what license does util have?":              {`pkg("util") => ?l`, `pkg("util", ?l) => ?l`},
		"which packages have no license stated?":    {`pkg(?p, ?l), absent(?x) => ?p`, `pkg(?p, ?l), absent(?l) => ?p`},
		"how many packages does app depend on?":     {`deps.depends_on(App, ?d) => count(?d)`, `deps.depends_on("app", ?d) => count(?d)`},
		"what imports util?":                        {`imports("util", ?x) => ?x`},
		"list every package with its license":       {`pkg(?n, ?l) => ?l, ?n`},
		"who maintains api?":                        {`maintainer("api", ?m) => ?m`, `maintainer("api", ?m) => ?m`, `owner("api", ?m) => ?m`},
	}
	bigDrafts = map[string][]string{
		"what imports util?": {`imports(?x, "util") => ?x`},
		"who maintains api?": {`pkg("api", ?m) => ?m`},
	}
	callables = &nl.Pack{Callables: []nl.Callable{
		{Relation: "imports", Doc: "the packages a package imports directly", Args: []string{"the importing package", "the imported one"}},
		{Relation: "deps.depends_on", Args: []string{"the package", "a package it depends on"}},
		{Relation: "pkg", Args: []string{"the package", "its license, absent when unstated"}},
	}}
	picks = map[string][]string{
		"what does app import directly?":            {`imports("app", ?d)`},
		"what does app depend on, directly or not?": {`deps.depends_on("app", ?d)`},
		"what license does util have?":              {`pkg("util", ?l)`},
		"which packages have no license stated?":    {"none", `pkg(?p, ?l), absent(?l) => ?p`},
		"how many packages does app depend on?":     {"none", `deps.depends_on("app", ?d) => count(?d)`},
		"what imports util?":                        {`imports("util", ?x)`, `imports(?x, "util")`},
		"list every package with its license":       {`pkg(?p, ?l)`},
		"who maintains api?":                        {"none", `pkg("api", ?m) => ?m`},
	}
)

const mitQ, mit = "which packages are MIT licensed?", `pkg(?p, "MIT") => ?p`

func guideCases() []nl.Case {
	out := make([]nl.Case, len(guide.Cases))
	for i, c := range guide.Cases {
		out[i] = nl.Case{Name: c.Name, Question: c.Question, Want: c.Want}
	}
	return out
}

func arms() []arm {
	mitCase := []nl.Case{{Name: "mit", Question: mitQ, Want: [][]string{{"app"}}}}
	actions := func(a nl.Actions) func(*datalog.Base) nl.Options {
		return func(b *datalog.Base) nl.Options { return nl.Options{Base: b, Actions: &a} }
	}
	return []arm{
		{name: "drafting (TestEval)", small: smallDrafts, big: bigDrafts, cases: guideCases(), wantPassed: 7, wantFirstTry: 2,
			opts: func(b *datalog.Base) nl.Options { return nl.Options{Base: b} }},
		{name: "picking (TestEvalCallable)", small: picks, pack: callables, cases: guideCases(), wantPassed: 7, wantFirstTry: 6,
			opts: func(b *datalog.Base) nl.Options { return nl.Options{Base: b} }},
		{name: "actions and a repair", actions: true, small: map[string][]string{mitQ: {"look pkg", "values pkg.license", `pkg(?p, "mit") => ?p`}},
			cases: mitCase, wantPassed: 1, opts: actions(nl.Actions{})},
		{name: "an action asked twice, then out of steps", actions: true, small: map[string][]string{mitQ: {"look pkg", "look pkg", "look pkg", mit}},
			cases: mitCase, wantPassed: 1, opts: actions(nl.Actions{Steps: 2})},
		{name: "a try, compacted", actions: true, small: map[string][]string{mitQ: {"try\n" + `pkg(?p, "nope") => ?p`, `pkg(?p, "nope") => ?p`, mit}},
			cases: mitCase, wantPassed: 1, opts: actions(nl.Actions{Compact: true})},
		{name: "a vote", small: map[string][]string{mitQ: {mit, `pkg(?p, "nope") => ?p`, `pkg(?x, "MIT") => ?x`}},
			cases: mitCase, wantPassed: 1, wantFirstTry: 1,
			opts: func(b *datalog.Base) nl.Options { return nl.Options{Base: b, Vote: &nl.Vote{Drafts: 3}} }},
		{name: "a disputed vote escalated", small: map[string][]string{mitQ: {mit, `pkg(?p, "nope") => ?p`, `pkg(?p, ?l) => ?p`}},
			big: map[string][]string{mitQ: {mit, mit, mit}}, cases: mitCase, wantPassed: 1,
			opts: func(b *datalog.Base) nl.Options {
				return nl.Options{Base: b, Vote: &nl.Vote{Drafts: 3}, Escalate: 0.6}
			}},
		{name: "a model failing", small: map[string][]string{mitQ: {`nope(?p) => ?p`}}, big: map[string][]string{mitQ: {mit}},
			cases: mitCase, wantPassed: 1, opts: func(b *datalog.Base) nl.Options { return nl.Options{Base: b} }},
		{name: "a disputed pick escalated to drafting", small: map[string][]string{mitQ: {`pkg(?p, "MIT")`, `pkg(?p, "nope")`, `pkg(?p, ?l)`, mit}},
			pack: callables, cases: mitCase, wantPassed: 1,
			opts: func(b *datalog.Base) nl.Options {
				return nl.Options{Base: b, Vote: &nl.Vote{Drafts: 3}, Escalate: 0.6}
			}},
	}
}

// outcome is what the differential compares of one case: everything a loop decides, not what the
// calls cost.
func outcome(r nl.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "pass %v, got %v, by %q: %q, err %v\n", r.Pass, r.Got, r.Program.Model, r.Program.Text, r.Err != nil)
	c := r.Program.Confidence
	fmt.Fprintf(&b, "  confidence %d/%d ran %v judged %v steps %d\n", c.Agreed, c.Drawn, c.Ran, c.Judged, c.Steps)
	for _, t := range r.Trace.Tries {
		fmt.Fprintf(&b, "  try %s %q kind %q mode %q repair %q reasked %v\n", t.Model, t.Text, t.Kind, t.Mode, t.Repair, t.Reasked)
	}
	for _, s := range r.Trace.Steps {
		fmt.Fprintf(&b, "  step %s %s %q after %d: %q\n", s.Model, s.Action, s.Text, s.After, s.Result)
	}
	for _, t := range r.Trace.Votes {
		fmt.Fprintf(&b, "  vote %s %q kind %q\n", t.Model, t.Text, t.Kind)
	}
	for _, e := range r.Trace.Escalations {
		fmt.Fprintf(&b, "  escalated %s %q %d/%d\n", e.Model, e.Text, e.Confidence.Agreed, e.Confidence.Drawn)
	}
	return b.String()
}

// The Runner loop decides what nl.Translate decides (#496): over TestEval's and TestEvalCallable's
// scripts, and scripts taking actions, repairing a spelling, voting and escalating, every case passes
// or fails alike, with the same answer, tries, steps, votes and escalations. control: each arm's
// reference scores what it was scripted to, so the arms exercise what they're named for.
func TestTheRunnerLoopDecidesAsTranslateDoes(t *testing.T) {
	v, b := guide.Deps(t)
	for _, a := range arms() {
		o := a.opts(b)
		o.Pack = a.pack
		var refBig nl.Model = (*script)(nil)
		if a.big != nil {
			refBig = scripted("big", a.big)
		}
		small := scripted("small", a.small)
		ref := nl.Eval(context.Background(), a.cases, b, func(ctx context.Context, q string) (nl.Program, nl.Trace, error) {
			ro := o
			if a.big != nil {
				ro.Fallback = refBig
			}
			return nl.Translate(ctx, q, v, nil, small, ro)
		})
		if ref.Passed != a.wantPassed || ref.PassedFirstTry != a.wantFirstTry {
			t.Errorf("%s: control: the reference passed %d, %d first try", a.name, ref.Passed, ref.PassedFirstTry)
		}
		runSmall, runBig := scripted("small", a.small), scripted("big", a.big)
		cfg := Config{Model: Role{Provider: runSmall, Label: "small"}, Base: b,
			Options: func(p *nl.Options) { *p = o }}
		if a.big != nil {
			cfg.Fallback = Role{Provider: runBig, Label: "big"}
		}
		tr, err := New(v, a.pack, cfg)
		if err != nil {
			t.Fatal(err)
		}
		got := nl.Eval(context.Background(), a.cases, b, tr.Ask)
		for i := range a.cases {
			if w, g := outcome(ref.Cases[i]), outcome(got.Cases[i]); w != g {
				t.Errorf("%s, %s:\nnl.Translate:\n%sRunner:\n%s", a.name, a.cases[i].Name, w, g)
			}
		}
		if a.actions {
			continue
		}
		for _, m := range []struct {
			name     string
			ref, got *script
		}{{"small", small, runSmall}, {"big", refBig.(*script), runBig}} {
			if m.ref == nil {
				continue
			}
			if len(m.ref.asks) != len(m.got.asks) {
				t.Errorf("%s: %s asked %d times by nl.Translate, %d by the Runner", a.name, m.name, len(m.ref.asks), len(m.got.asks))
				continue
			}
			for i := range m.ref.asks {
				if m.ref.asks[i] != m.got.asks[i] {
					t.Errorf("%s: %s's ask %d:\nnl.Translate:\n%sRunner:\n%s", a.name, m.name, i, m.ref.asks[i], m.got.asks[i])
					break
				}
			}
		}
	}
}

// An arm's script reaches the paths it is named for in the Runner loop: tool calls, the repair, a
// compacted request, the vote's draws and the escalation.
func TestTheDifferentialsArmsReachTheirPaths(t *testing.T) {
	v, b := guide.Deps(t)
	run := func(a arm) (nl.Trace, *script) {
		small := scripted("small", a.small)
		cfg := Config{Model: Role{Provider: small, Label: "small"}, Base: b, Options: func(p *nl.Options) { *p = a.opts(b) }}
		if a.big != nil {
			cfg.Fallback = Role{Provider: scripted("big", a.big), Label: "big"}
		}
		tr, err := New(v, a.pack, cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, trace, _ := tr.Ask(context.Background(), a.cases[0].Question)
		return trace, small
	}
	byName := map[string]arm{}
	for _, a := range arms() {
		byName[a.name] = a
	}
	trace, _ := run(byName["actions and a repair"])
	if len(trace.Steps) != 2 || len(trace.Tries) != 2 || trace.Tries[1].Repair == "" {
		t.Errorf("actions and a repair: %d steps, tries %+v", len(trace.Steps), trace.Tries)
	}
	trace, _ = run(byName["an action asked twice, then out of steps"])
	if len(trace.Steps) != 2 || !strings.HasPrefix(trace.Steps[1].Result, nl.DefaultPrompts().AskedAgain) || len(trace.Tries) != 2 || trace.Tries[0].Kind != nl.KindParse {
		t.Errorf("out of steps: steps %+v, tries %+v", trace.Steps, trace.Tries)
	}
	trace, small := run(byName["a try, compacted"])
	if len(trace.Steps) != 1 || small.tooled == 0 || small.longest != 1 {
		t.Errorf("compacted: %d steps, %d requests with tools, longest %d messages", len(trace.Steps), small.tooled, small.longest)
	}
	trace, _ = run(byName["a vote"])
	if len(trace.Votes) != 2 || trace.Votes[0].Kind != nl.KindEmpty {
		t.Errorf("vote: %+v", trace.Votes)
	}
	trace, _ = run(byName["a disputed vote escalated"])
	if len(trace.Escalations) != 1 || !slices.ContainsFunc(trace.Tries, func(t nl.Try) bool { return t.Model == "big" }) {
		t.Errorf("escalation: %+v", trace.Escalations)
	}
}
