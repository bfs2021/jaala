// Package llamacpp is an nl.Model over llama.cpp's llama-server (#330): it sends each nl.Prompt to the
// server's OpenAI-compatible chat endpoint, through chakra's provider, with the prompt's grammar, so the model can only write text
// that parses, and with prompt caching on, so the card in the prompt's prefix is processed once. It
// runs no model; a host starts llama-server with the model it wants.
package llamacpp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/frontends/nlmodels/chakra"
)

// A Client asks one llama-server. The zero value isn't usable; New fills in the defaults.
type Client struct {
	// URL is the server's base URL, such as http://localhost:8080.
	URL string
	// Model names the model in the Trace, and is sent as the request's model ("llama.cpp" when
	// empty, since chakra requires one), which llama-server serving one model ignores.
	Model string
	// MaxTokens caps a reply's length; a program rarely needs more than the default 512.
	MaxTokens int
	// Temperature is sent as is. The default 0 decodes greedily, so a prompt gets the same draft every
	// time, which is what makes a Trace repeatable, and makes an nl.Vote's drafts agree by
	// construction: a vote wants a temperature above 0, and each of its drafts is seeded by its Draw.
	Temperature float64
	// HTTP sends the requests; http.DefaultClient by default.
	HTTP *http.Client
}

// New is a Client of the llama-server at url, replying with at most 512 tokens, greedily.
func New(url string) *Client {
	return &Client{URL: strings.TrimRight(url, "/"), MaxTokens: 512, HTTP: http.DefaultClient}
}

// Name is "llama.cpp", or "llama.cpp:" and the Model when one is set.
func (c *Client) Name() string {
	if c.Model == "" {
		return "llama.cpp"
	}
	return "llama.cpp:" + c.Model
}

// Complete sends p as a chat through chakra's OpenAI provider in its llama.cpp dialect: its Prefix as
// the system message, its Turns as user and assistant messages, its Grammar, when it has one, as the
// grammar the reply is decoded under, and a vote's Draw as the seed. Prompt caching is on, so the card
// in the prefix is processed once. The tokens it reports are the server's usage; the cached ones are
// its prompt_tokens_details, or failing that the prompt tokens its cache answered (timings.cache_n). A
// reply with a status other than 200 is an error carrying the start of the server's body, which says
// why (an unparseable grammar, say).
func (c *Client) Complete(ctx context.Context, p nl.Prompt) (nl.Completion, error) {
	m, err := c.model()
	if err != nil {
		return nl.Completion{}, err
	}
	out, err := m.Complete(ctx, p)
	if err != nil {
		return nl.Completion{}, fmt.Errorf("llamacpp: %w", err)
	}
	return out, nil
}

// model is the chakra Model the Client's fields describe, built per call since a host may change them.
// Temperature always goes out: a llama-server sent none samples at its own default, 0.8, not greedily.
func (c *Client) model() (*chakra.Model, error) {
	model := c.Model
	if model == "" {
		model = "llama.cpp"
	}
	p, err := agent.NewOpenAIProvider(agent.OpenAIConfig{
		BaseURL:       c.URL + "/v1",
		Model:         model,
		HTTPClient:    c.HTTP,
		GuidedDialect: agent.GuidedDialectLlamaCpp,
		ExtraBody:     map[string]any{"cache_prompt": true},
	})
	if err != nil {
		return nil, fmt.Errorf("llamacpp: %w", err)
	}
	temp := c.Temperature
	return &chakra.Model{Provider: p, Label: c.Name(), MaxTokens: c.MaxTokens, Temperature: &temp, Grammar: true, Seed: true}, nil
}
