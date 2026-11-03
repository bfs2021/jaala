package translator

import (
	"context"
	"os"
	"testing"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
	"github.com/panyam/jaala/internal/gbnf"
	"github.com/panyam/jaala/ns"
)

// A real llama-server keeps the Runner loop's answers to nl's grammar (#496), which chakra sends on
// each answering step as ResponseGuided. ./selfcheck.sh runs it beside chakra's own server test, on
// llama.cpp's 1 MB test model:
//
//	llama-server -m ~/.cache/jaala/stories260K.gguf --port 18089 --chat-template chatml
//	JAALA_LLAMACPP_URL=http://127.0.0.1:18089 go test ./translator -run RealServer -v
//
// The test model writes nonsense, so every answer is refused and its tries run out; each refused
// answer must still be text the grammar accepts, as a prefix when the model was cut off.
func TestARealServerHoldsTheRunnersAnswersToTheGrammar(t *testing.T) {
	url := os.Getenv("JAALA_LLAMACPP_URL")
	if url == "" {
		t.Skip("JAALA_LLAMACPP_URL is not set")
	}
	// No stdlib, so the card fits the test model's 2048-token context, which llama-server won't raise.
	src := ns.NewMemSource().Declare("edge", "from", "to")
	src.Add("edge", ns.Tuple{Vals: []ns.Value{ns.S("a"), ns.S("b")}})
	v, err := ns.NewVocabulary(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := v.AddLanguage(datalog.Language); err != nil {
		t.Fatal(err)
	}
	b := datalog.MustBase(v, src)
	text, err := nl.GrammarFor(v)
	if err != nil {
		t.Fatal(err)
	}
	ours, err := gbnf.Parse(text)
	if err != nil {
		t.Fatal(err)
	}
	p, err := agent.NewLlamaCppProvider(agent.LlamaCppConfig{BaseURL: url + "/v1"})
	if err != nil {
		t.Fatal(err)
	}
	warm := 1.2
	tr, err := New(v, nil, Config{Model: Role{Provider: p, Temperature: &warm, MaxTokens: 80}, Base: b,
		Options: func(o *nl.Options) { o.MaxTries = 3 }})
	if err != nil {
		t.Fatal(err)
	}
	_, trace, _ := tr.Ask(context.Background(), "what calls what?")
	answered := 0
	for _, try := range trace.Tries {
		if try.Kind == nl.KindModel {
			t.Fatalf("the server refused a call: %v", try.Err)
		}
		if try.Kind != nl.KindRepeat {
			answered++
		}
		if !ours.AcceptsPrefix(try.Text) {
			t.Errorf("an answer leaves the grammar as nl reads it: %q", try.Text)
		}
	}
	if answered == 0 {
		t.Fatalf("control: no answer was judged: %+v", trace.Tries)
	}
	if !t.Failed() {
		t.Logf("%d answers judged, all inside the grammar", answered)
	}
}
