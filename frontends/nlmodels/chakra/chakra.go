// Package chakra is an nl.Model over any chakra Provider: llama.cpp's llama-server and Ollama through
// chakra's presets (agent.NewLlamaCppProvider, agent.NewOllamaProvider), other OpenAI-compatible
// servers, and Anthropic. jaala keeps no model client of its own: a host builds the provider, or
// names it in chakra's connections config, and wraps it with New.
package chakra

import (
	"context"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
)

// A Model asks one chakra Provider. The zero value isn't usable; New fills in the defaults.
type Model struct {
	// Provider answers each prompt with one Generate call. What else goes with the prompt is what
	// agent.CapabilitiesOf says it can send: the grammar when it takes a GBNF grammar (llama.cpp,
	// vLLM), and a vote's seed when it takes a seed (any OpenAI-compatible server). Ollama takes
	// only a JSON schema and Anthropic neither, so they get the prompt alone, and nl.Translate
	// checks every draft anyway.
	Provider agent.Provider
	// Label names the model in the Trace and Program.Model.
	Label string
	// MaxTokens caps a reply's length, 512 when New made the Model; 0 leaves it to the provider.
	MaxTokens int
	// Temperature is sent when set, 0 when New made the Model, so drafts repeat and a vote's
	// differ only by their seed. Set it to nil for a provider that refuses one (current Anthropic
	// models answer 400): a llama-server sent none samples at 0.8.
	Temperature *float64
}

// New is a Model named label over p, replying with at most 512 tokens at temperature 0.
func New(label string, p agent.Provider) *Model {
	zero := 0.0
	return &Model{Provider: p, Label: label, MaxTokens: 512, Temperature: &zero}
}

// Name is the Label.
func (m *Model) Name() string { return m.Label }

// Complete sends p as one call: its Prefix as the instructions and its Turns as user and assistant
// messages, with the grammar and the seed when the provider can send them. The tokens are the
// provider's usage, cached ones included where the provider reports them. A refusal is chakra's
// error, which carries the server's status and body (an unparseable grammar, say).
func (m *Model) Complete(ctx context.Context, p nl.Prompt) (nl.Completion, error) {
	req := agent.ProviderRequest{Instructions: p.Prefix, MaxTokens: m.MaxTokens, Temperature: m.Temperature}
	for _, t := range p.Turns {
		role := agent.RoleUser
		if t.Role == nl.RoleModel {
			role = agent.RoleAssistant
		}
		req.Messages = append(req.Messages, agent.Message{Role: role, Text: t.Text})
	}
	caps := agent.CapabilitiesOf(m.Provider)
	if g := (&agent.GuidedDecoding{Grammar: p.Grammar}); p.Grammar != "" && caps.CanGuide(g) {
		req.Guided = g
	}
	if caps.Seed && p.Draw > 0 {
		seed := p.Draw
		req.Seed = &seed
	}
	res, err := m.Provider.Generate(ctx, req)
	if err != nil {
		return nl.Completion{}, err
	}
	out := nl.Completion{Text: res.Text}
	if u := res.Usage; u != nil {
		out.TokensIn, out.TokensOut, out.TokensCached = u.InputTokens, u.OutputTokens, u.CachedInputTokens
	}
	return out, nil
}
