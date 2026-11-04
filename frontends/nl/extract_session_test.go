package nl_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/nl"
)

// xverdict names which of an ExtractVerdict's outcomes is set, failing on one setting none or several.
func xverdict(t *testing.T, vd nl.ExtractVerdict) string {
	t.Helper()
	var set []string
	if vd.Extraction != nil {
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

const linked = "a links to b."

// A session asks with the text, holds replies to ExtractGrammar, and accepts a reply whose facts
// check and quote the text, its fence taken off, citing the quoted words.
func TestAnExtractSessionAcceptsFactsTheTextStates(t *testing.T) {
	v := vocabulary(t)
	s, err := nl.NewExtractSession(linked, v, nl.ExtractOptions{})
	if err != nil {
		t.Fatal(err)
	}
	g, _ := nl.ExtractGrammar(v)
	if s.Ask() != "Text:\n"+linked || s.Grammar() != g || !strings.Contains(s.Prefix(), "# edge(from, to)") || s.MaxTries() != 3 {
		t.Errorf("ask %q, grammar %v, tries %d, prefix:\n%s", s.Ask(), s.Grammar() == g, s.MaxTries(), s.Prefix())
	}
	vd := s.NewConversation().Judge(context.Background(), nl.Try{Model: "m", Text: "```\n+ edge(\"a\", \"b\") from \"a links to b\"\n```"})
	if xverdict(t, vd) != "accept" || vd.Extraction.Model != "m" || len(vd.Extraction.Facts) != 1 || vd.Extraction.Facts[0].Span != "a links to b" || vd.Try.Err != nil {
		t.Errorf("verdict %+v", vd)
	}
	if plain, _ := nl.NewExtractSession(linked, v, nl.ExtractOptions{NoGrammar: true}); plain.Grammar() != "" {
		t.Errorf("NoGrammar sends %q", plain.Grammar())
	}
}

// A refused reply continues with the line it failed on and ExtractFix; the same reply again stops,
// wrapping the first refusal, and so does the last try. control: a different reply continues.
func TestAnExtractConversationContinuesThenStops(t *testing.T) {
	v := vocabulary(t)
	s, _ := nl.NewExtractSession(linked, v, nl.ExtractOptions{MaxTries: 3})
	bad := `+ edge("a", "b") from "nowhere in the text"`
	c := s.NewConversation()
	vd := c.Judge(context.Background(), nl.Try{Text: bad})
	if xverdict(t, vd) != "continue" || vd.Try.Kind != nl.KindSpan || !strings.HasSuffix(vd.Continue, nl.DefaultPrompts().ExtractFix) || !strings.Contains(vd.Continue, "line 1") {
		t.Fatalf("verdict %+v", vd)
	}
	vd = c.Judge(context.Background(), nl.Try{Text: bad})
	if xverdict(t, vd) != "stop" || vd.Try.Kind != nl.KindRepeat || !errors.Is(vd.Stop, vd.Try.Err) {
		t.Errorf("the repeat %+v", vd)
	}
	c = s.NewConversation()
	c.Judge(context.Background(), nl.Try{Text: bad})
	if vd := c.Judge(context.Background(), nl.Try{Text: `+ nope("a") from "a links to b"`}); xverdict(t, vd) != "continue" {
		t.Errorf("control: a different reply %+v", vd)
	}
	vd = c.Judge(context.Background(), nl.Try{Text: `+ edge("a") from "a links to b"`})
	var ce *datalog.CheckError
	if xverdict(t, vd) != "stop" || !errors.As(vd.Stop, &ce) {
		t.Errorf("the last try %+v", vd)
	}
}
