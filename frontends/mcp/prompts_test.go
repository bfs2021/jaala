package mcp_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/panyam/jaala/datalog"
	"github.com/panyam/jaala/frontends/mcp"
	"github.com/panyam/jaala/frontends/nl"
)

// heard is a scripted model keeping each prompt it is sent.
type heard struct {
	scripted
	prompts []nl.Prompt
}

func (h *heard) Complete(ctx context.Context, p nl.Prompt) (nl.Completion, error) {
	h.prompts = append(h.prompts, p)
	return h.scripted.Complete(ctx, p)
}

// The ask tool translates with the pack's Prompts, so a host's wording reaches its model through the
// server too. control: without them, jaala's instructions.
func TestAskSendsThePacksPrompts(t *testing.T) {
	for _, own := range []bool{true, false} {
		p := pack
		if own {
			p.Prompts = &nl.Prompts{Instructions: "# H-INSTRUCTIONS\n"}
		}
		m := &heard{scripted: scripted{name: "small", replies: []string{`trip.reach("a", ?t) => ?t`}}}
		call(t, dial(t, p, mcp.Options{Translator: translating(m)}), "ask", map[string]any{"question": "where can a get to?"})
		if len(m.prompts) != 1 || strings.Contains(m.prompts[0].Prefix, "H-INSTRUCTIONS") != own || strings.Contains(m.prompts[0].Prefix, nl.DefaultPrompts().Instructions) == own {
			t.Errorf("own prompts %v: asked %d times: %+v", own, len(m.prompts), m.prompts)
		}
	}
}

// A pack's tool descriptions, argument docs and agent line replace jaala's word for word (#485), and
// ToolDescriptions gives jaala's as written for the pack. control: the tools left alone keep jaala's.
func TestAPacksWordsReplaceTheServersText(t *testing.T) {
	own := pack
	own.ToolDescriptions = map[string]string{"query": "Ask the road atlas."}
	own.ArgDescriptions = map[string]map[string]string{"query": {"limit": "How many roads, at most."}}
	own.AgentLine = "Use roads for anything about roads."
	c := dial(t, own, mcp.Options{})
	defaults := mcp.ToolDescriptions(pack)
	for _, tool := range c.ListTools() {
		want := defaults[tool.Name]
		if tool.Name == "query" {
			want = "Ask the road atlas."
		}
		if tool.Description != want {
			t.Errorf("%s's description %q, want %q", tool.Name, tool.Description, want)
		}
		data, _ := json.Marshal(tool.InputSchema)
		limit := `"description":"How many roads, at most."`
		if strings.Contains(string(data), limit) != (tool.Name == "query") {
			t.Errorf("%s's schema: %s", tool.Name, data)
		}
	}
	if got := mcp.AgentLine(own); got != own.AgentLine {
		t.Errorf("agent line %q", got)
	}
	if got := mcp.AgentLine(pack); !strings.HasPrefix(got, "For questions about roads between towns") {
		t.Errorf("control: agent line %q", got)
	}
	if d := defaults["query"]; !strings.HasPrefix(d, "Run a Datalog program over roads between towns") {
		t.Errorf("control: jaala's query description %q", d)
	}
}

// A description for a tool the server doesn't offer, or an argument the tool doesn't take, is refused
// when the server starts.
func TestAPacksWordsMustNameItsToolsAndArguments(t *testing.T) {
	v, b := roads(t)
	o := mcp.Options{Base: func(context.Context) (*datalog.Base, error) { return b, nil }}
	for _, c := range []struct {
		edit func(*mcp.Pack)
		want string
	}{
		{func(p *mcp.Pack) { p.ToolDescriptions = map[string]string{"quer": "x"} }, `describes a tool "quer"`},
		{func(p *mcp.Pack) { p.ToolDescriptions = map[string]string{"cypher": "x"} }, `describes a tool "cypher"`},
		{func(p *mcp.Pack) { p.ArgDescriptions = map[string]map[string]string{"quer": {"limit": "x"}} }, `arguments of a tool "quer"`},
		{func(p *mcp.Pack) { p.ArgDescriptions = map[string]map[string]string{"query": {"limt": "x"}} }, `query's argument "limt": it takes no such argument`},
	} {
		p := pack
		c.edit(&p)
		if _, err := mcp.New(v, p, o); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("err %v, want %q", err, c.want)
		}
	}
}
