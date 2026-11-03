package llamacpp

import (
	"context"
	"os"
	"strings"
	"testing"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/internal/gbnf"
	"github.com/panyam/jaala/ns"
	"github.com/panyam/jaala/stdlib"
)

// A real llama-server reads the grammar nl renders and keeps a model to it (#330), which our own
// recognizer can't check. Any model will do, even llama.cpp's 1 MB test model, since the grammar is
// what's tested; ./selfcheck.sh runs it on that one when `make llama` has fetched it:
//
//	llama-server -m ~/.cache/jaala/stories260K.gguf --port 18089 --chat-template chatml
//	JAALA_LLAMACPP_URL=http://127.0.0.1:18089 go test ./llamacpp -run RealServer -v
//
// Every reply has to stay inside the grammar as nl's recognizer reads it, token by token. The test
// model writes nonsense into a string and rarely closes it, so its replies end at MaxTokens and are
// checked as prefixes; a reply that finishes is also parsed whole.
func TestARealServerKeepsTheModelToTheGrammar(t *testing.T) {
	url := os.Getenv("JAALA_LLAMACPP_URL")
	if url == "" {
		t.Skip("JAALA_LLAMACPP_URL is not set")
	}
	src := ns.NewMemSource().Declare("edge", "from", "to").Declare("node", "name")
	v, err := ns.NewVocabulary(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddLanguage(datalog.Language); err != nil {
		t.Fatal(err)
	}
	if err := stdlib.Register(v); err != nil {
		t.Fatal(err)
	}
	text, err := nl.GrammarFor(v)
	if err != nil {
		t.Fatal(err)
	}
	ours, err := gbnf.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	c := New(url)
	m, err := c.model() // the provider Complete sends through, asked directly for its finish reason
	if err != nil {
		t.Fatal(err)
	}
	prov := m.Provider
	temp := 1.2
	stopped := 0
	for i := 0; i < 24; i++ {
		res, err := prov.Generate(ctx, agent.ProviderRequest{Instructions: "Write a program.",
			Messages: []agent.Message{{Role: agent.RoleUser, Text: "what calls what?"}}, Guided: &agent.GuidedDecoding{Grammar: text},
			MaxTokens: 160, Temperature: &temp})
		if err != nil {
			t.Fatalf("the server refused the grammar: %v", err)
		}
		reply := res.Text
		if !ours.AcceptsPrefix(reply) {
			t.Errorf("the reply leaves the grammar as nl reads it: %q", reply)
		}
		if res.FinishReason != "stop" {
			continue // cut off at MaxTokens: a prefix, checked above
		}
		stopped++
		if _, err := datalog.Parse(reply); err != nil || !ours.Accepts(reply) {
			t.Errorf("a finished reply isn't a program: %q: %v", reply, err)
		}
	}
	t.Logf("%d of 24 replies finished on their own", stopped)
	_, err = c.Complete(ctx, nl.Prompt{Turns: []nl.Turn{{Role: nl.RoleUser, Text: "q"}}, Grammar: `root ::= (`})
	if err == nil || !strings.Contains(err.Error(), "400") {
		t.Fatalf("control: a broken grammar answered %v, so the server may not read grammars at all", err)
	}
}
