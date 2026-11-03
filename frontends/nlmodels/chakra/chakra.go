// Package chakra is an nl.Model over any chakra Provider: OpenAI-compatible servers (llama.cpp, vLLM,
// Ollama, hosted APIs) and Anthropic. The llamacpp and ollama packages are built on it, and a host
// with a provider of its own wraps it with New.
package chakra

import (
	"context"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
)

// A Model asks one chakra Provider. The zero value isn't usable; New fills in the defaults.
type Model struct {
	// Provider answers each prompt with one Generate call.
	Provider agent.Provider
	// Label names the model in the Trace and Program.Model.
	Label string
	// MaxTokens caps a reply's length, 512 when New made the Model; 0 leaves it to the provider.
	MaxTokens int
	// Temperature is sent when set. Leave it nil for a provider that refuses one (current Anthropic
	// models). A llama-server left to its default samples at 0.8, so set it to 0 there for repeatable
	// drafts.
	Temperature *float64
	// Grammar sends a Prompt's Grammar as a guided-decoding grammar. Set it only for a provider
	// configured with a GuidedDialect that takes GBNF (llama.cpp): chakra refuses a grammar it can't
	// send rather than dropping it, and nl.Translate checks every draft anyway.
	Grammar bool
	// Seed sends a vote's Prompt.Draw as the sampler's seed, so the vote's drafts differ. Leave it off
	// for a provider that refuses a seed (Anthropic).
	Seed bool
}

// New is a Model named label over p, replying with at most 512 tokens at the provider's
// temperature, with no grammar and no seed.
func New(label string, p agent.Provider) *Model {
	return &Model{Provider: p, Label: label, MaxTokens: 512}
}

// Name is the Label.
func (m *Model) Name() string { return m.Label }

// Complete sends p as one call: its Prefix as the instructions and its Turns as user and assistant
// messages, with the grammar and the seed when the Model sends them. The tokens are the provider's
// usage, cached ones included where the provider reports them. A refusal is chakra's error, which
// carries the server's status and body (an unparseable grammar, say).
func (m *Model) Complete(ctx context.Context, p nl.Prompt) (nl.Completion, error) {
	req := agent.ProviderRequest{Instructions: p.Prefix, MaxTokens: m.MaxTokens, Temperature: m.Temperature}
	for _, t := range p.Turns {
		role := agent.RoleUser
		if t.Role == nl.RoleModel {
			role = agent.RoleAssistant
		}
		req.Messages = append(req.Messages, agent.Message{Role: role, Text: t.Text})
	}
	if m.Grammar && p.Grammar != "" {
		req.Guided = &agent.GuidedDecoding{Grammar: p.Grammar}
	}
	if m.Seed && p.Draw > 0 {
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
