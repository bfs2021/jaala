// Package translator is jaala's translator entry point (#490): a host hands it its vocabulary, its
// nl.Pack and the chakra providers to ask, and asks questions. How the translator talks to each model
// (temperature, token limit, grammar, a thinking model's prompt) is jaala's, so every host gets the
// same tuning without rediscovering it. The host keeps its words (the pack) and its choice of models,
// which it can resolve by name through chakra's connections config.
package translator

import (
	"context"
	"errors"
	"regexp"
	"strings"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/frontends/nlmodels/chakra"
	"github.com/panyam/jaala/ns"
)

// The defaults a Role gets when it leaves a field unset.
const (
	// ModelTemperature is the model's: greedy, so its drafts repeat and a vote's differ only by seed.
	ModelTemperature = 0.0
	// FallbackTemperature is the fallback's: it is asked after the model failed, so it samples.
	FallbackTemperature = 0.7
	// MaxTokens caps a reply, and ThinkingMaxTokens a thinking model's, which writes its reasoning
	// before the program.
	MaxTokens         = 512
	ThinkingMaxTokens = 3072
)

// A Role is one model the translator asks: the provider and how to ask it.
type Role struct {
	// Provider answers the prompts. The grammar and a vote's seed are sent only when
	// agent.CapabilitiesOf says it can take them.
	Provider agent.Provider
	// Label names the model in the Trace and Program.Model; "model" or "fallback" when empty.
	Label string
	// Temperature replaces the role's default (ModelTemperature, FallbackTemperature).
	Temperature *float64
	// NoTemperature sends none, for a provider that answers 400 to any (current Anthropic models).
	// chakra can't say which providers refuse one yet (panyam/chakra#191), so the host says it here.
	NoTemperature bool
	// MaxTokens replaces the role's default (MaxTokens, or ThinkingMaxTokens when Thinking).
	MaxTokens int
	// Thinking is for a reasoning model: no grammar, which would hold the reply to a program from its
	// first token, a line asking it to think the question through and put the program in a fenced
	// block, and only the last fenced block, outside any <think> section, read as the draft.
	Thinking bool
	// ThinkPrompt replaces DefaultThinkPrompt, the line Thinking adds to the prefix, word for word
	// (#485). Like DefaultThinkPrompt it should ask for the program in a fenced block, which is what
	// is read as the draft.
	ThinkPrompt string
}

// Config is what a host decides per deployment. Everything else is jaala's default.
type Config struct {
	// Model is asked first. Its Provider is required.
	Model Role
	// Fallback, when its Provider is set, is asked once Model runs out of tries (nl.Options.Fallback).
	Fallback Role
	// Base evaluates a draft that checks, so an empty answer goes back to the model; without it a
	// program that checks is accepted.
	Base *datalog.Base
	// Actions, Vote, Escalate, Abstain and Budget are nl.Options' own, passed through.
	Actions  *nl.Actions
	Vote     *nl.Vote
	Escalate float64
	Abstain  *nl.Abstain
	Budget   nl.Budget
	// Options, when set, edits the nl.Options Ask uses after everything above is filled in, for what
	// Config doesn't name (a front end, a goal-only grammar, Eval options). What it sets wins.
	Options func(*nl.Options)
}

// A Translator answers questions over one vocabulary and pack with the models a Config names. It is
// safe for concurrent use as long as its providers are.
type Translator struct {
	v               *ns.Vocabulary
	opts            nl.Options
	model, fallback nl.Model
}

// New is a Translator over v and pack. It fails when cfg names no Model provider. pack may be nil.
func New(v *ns.Vocabulary, pack *nl.Pack, cfg Config) (*Translator, error) {
	if cfg.Model.Provider == nil {
		return nil, errors.New("translator: Config.Model needs a Provider")
	}
	t := &Translator{v: v, model: cfg.Model.build("model", ModelTemperature)}
	o := nl.Options{Pack: pack, Base: cfg.Base, Actions: cfg.Actions, Vote: cfg.Vote, Escalate: cfg.Escalate, Abstain: cfg.Abstain, Budget: cfg.Budget}
	if cfg.Fallback.Provider != nil {
		t.fallback = cfg.Fallback.build("fallback", FallbackTemperature)
		o.Fallback = t.fallback
	}
	if cfg.Options != nil {
		cfg.Options(&o)
	}
	t.opts = o
	return t, nil
}

// Ask translates question with nl.Translate: the pack's grounder, examples and checks, the model and
// then the fallback, and the Config's options. The Trace holds every try, also when Ask fails.
func (t *Translator) Ask(ctx context.Context, question string) (nl.Program, nl.Trace, error) {
	return nl.Translate(ctx, question, t.v, nil, t.model, t.opts)
}

// Model is the first model as Ask asks it, with jaala's tuning applied, for a host handing it to
// frontends/mcp's Options.Model.
func (t *Translator) Model() nl.Model { return t.model }

// Fallback is the fallback as Ask asks it, nil when the Config names none.
func (t *Translator) Fallback() nl.Model { return t.fallback }

// build is r as an nl.Model, with the role's defaults where r leaves them.
func (r Role) build(label string, temp float64) nl.Model {
	if r.Label != "" {
		label = r.Label
	}
	m := chakra.New(label, r.Provider)
	switch {
	case r.NoTemperature:
		m.Temperature = nil
	case r.Temperature != nil:
		t := *r.Temperature
		m.Temperature = &t
	default:
		m.Temperature = &temp
	}
	switch {
	case r.MaxTokens > 0:
		m.MaxTokens = r.MaxTokens
	case r.Thinking:
		m.MaxTokens = ThinkingMaxTokens
	}
	if r.Thinking {
		line := r.ThinkPrompt
		if line == "" {
			line = DefaultThinkPrompt
		}
		return thinking{m, line}
	}
	return m
}

// DefaultThinkPrompt is what a thinking model's prefix gains, unless its Role's ThinkPrompt replaces it.
const DefaultThinkPrompt = `
# Before you write the program, think it through: which things the question names and their ids, which relations connect them, which variables join them, and what the answer's columns are. Then write the program alone in a fenced block.
`

// thinking asks a reasoning model without the grammar and with think, and hands Translate only the
// program, so each draft is still checked and its error fed back.
type thinking struct {
	nl.Model
	prompt string // the line the prefix gains: Role.ThinkPrompt or DefaultThinkPrompt
}

func (t thinking) Complete(ctx context.Context, p nl.Prompt) (nl.Completion, error) {
	p.Grammar = ""
	p.Prefix += t.prompt
	c, err := t.Model.Complete(ctx, p)
	c.Text = programIn(c.Text)
	return c, err
}

var (
	thought = regexp.MustCompile(`(?s)<think>.*?</think>`)
	fenced  = regexp.MustCompile("(?s)```[a-zA-Z]*\n(.*?)```")
)

// programIn is the last fenced block of reply outside its <think> sections, or the rest of the reply
// when it has none. The last, since a model reasoning in the open drafts before it answers.
func programIn(reply string) string {
	reply = thought.ReplaceAllString(reply, "")
	if bs := fenced.FindAllStringSubmatch(reply, -1); len(bs) > 0 {
		return strings.TrimSpace(bs[len(bs)-1][1])
	}
	return strings.TrimSpace(reply)
}
