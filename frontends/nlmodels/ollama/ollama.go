// Package ollama is an nl.Model over an Ollama server's OpenAI-compatible chat endpoint (#330), through
// chakra's provider. Ollama takes no GBNF
// grammar (its format field is a JSON schema, which doesn't fit a program's text), so a Prompt's
// Grammar isn't sent and the model writes freely; nl.Translate checks every draft either way, so
// what it costs is tries. A host wanting the grammar held runs the model under llama.cpp instead
// (package llamacpp).
package ollama

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/frontends/nlmodels/chakra"
)

// A Client asks one Ollama server for one model. The zero value isn't usable; New fills in the
// defaults.
type Client struct {
	// URL is the server's base URL, such as http://localhost:11434.
	URL string
	// Model is the Ollama model to run, such as qwen2.5-coder:1.5b. Ollama requires one.
	Model string
	// MaxTokens caps a reply's length (max_tokens, Ollama's num_predict); 512 by default.
	MaxTokens int
	// Temperature is sent as is. The default 0 decodes greedily, so a Trace repeats, and an nl.Vote's
	// drafts agree by construction: a vote wants a temperature above 0, and each of its drafts is
	// seeded by its Draw.
	Temperature float64
	// HTTP sends the requests; http.DefaultClient by default.
	HTTP *http.Client
}

// New is a Client of the Ollama server at url running model, replying with at most 512 tokens,
// greedily.
func New(url, model string) *Client {
	return &Client{URL: strings.TrimRight(url, "/"), Model: model, MaxTokens: 512, HTTP: http.DefaultClient}
}

// Name is "ollama:" and the Model.
func (c *Client) Name() string { return "ollama:" + c.Model }

// Complete sends p as a chat: its Prefix as the system message, its Turns as user and assistant
// messages, and a vote's Draw as the seed. The tokens are the server's usage; Ollama reports no cached
// ones, so TokensCached stays 0. A reply with a status other than 200 is an error carrying the start
// of the server's body.
func (c *Client) Complete(ctx context.Context, p nl.Prompt) (nl.Completion, error) {
	if c.Model == "" {
		return nl.Completion{}, fmt.Errorf("ollama: no model named")
	}
	prov, err := agent.NewOpenAIProvider(agent.OpenAIConfig{BaseURL: c.URL + "/v1", Model: c.Model, HTTPClient: c.HTTP})
	if err != nil {
		return nl.Completion{}, fmt.Errorf("ollama: %w", err)
	}
	temp := c.Temperature
	m := &chakra.Model{Provider: prov, Label: c.Name(), MaxTokens: c.MaxTokens, Temperature: &temp, Seed: true}
	out, err := m.Complete(ctx, p)
	if err != nil {
		return nl.Completion{}, fmt.Errorf("ollama: %w", err)
	}
	return out, nil
}
