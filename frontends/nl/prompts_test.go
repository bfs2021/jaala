package nl_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/panyam/jaala/frontends/nl"
)

// hostPrompts replaces every Prompts field with a marker naming it, templates keeping their
// placeholders, so a test can tell which text reached the model.
func hostPrompts() *nl.Prompts {
	return &nl.Prompts{
		Instructions:         "H-INSTRUCTIONS",
		FrontEndInstructions: map[string]string{"shout": "H-SHOUT"},
		Actions:              "\nH-ACTIONS %s|%s\n",
		Find:                 "H-FIND",
		AskedAgain:           "H-ASKED",
		LastLook:             "H-LAST",
		ToolLook:             "H-TOOLLOOK",
		ToolValues:           "H-TOOLVALUES %s",
		ToolTry:              "H-TOOLTRY",
		ToolFind:             "H-TOOLFIND",
		Fix:                  "H-FIX %s",
		FixEmpty:             "H-FIXEMPTY %s",
		EmptyFallback:        "H-EMPTYFALLBACK",
		Reask:                "H-REASK %[4]s|%[1]s|%[3]s|%[2]s",
		Grounded:             "H-GROUNDED",
		Pick:                 "H-PICK\n",
		PickRetry:            "H-PICKRETRY",
		CompactFound:         "H-FOUND",
		CompactRefused:       "H-REFUSED",
		Extract:              "\nH-EXTRACT\n",
		ExtractFix:           "H-EXTRACTFIX",
	}
}

func with(p *nl.Pack, pr *nl.Prompts) *nl.Pack {
	q := *p
	q.Prompts = pr
	return &q
}

// Extract's instructions and its retry line are a pack's too.
func TestAPacksPromptsReplaceExtractsText(t *testing.T) {
	v, b := owned(t)
	m := model("m", `+ imports("app", "api")`, `+ imports("app", "api") from "app imports api"`)
	if _, _, err := nl.Extract(context.Background(), "app imports api.", v, m, nl.ExtractOptions{Base: b, Pack: with(ownedPack(), hostPrompts())}); err != nil {
		t.Fatal(err)
	}
	if msg := want(m.asked[0].Prefix, "\nH-EXTRACT\n") + absent(m.asked[0].Prefix, "# Write the facts the text states") + want(m.asked[1].Turns[2].Text, "\n\nH-EXTRACTFIX"); msg != "" {
		t.Error(msg)
	}
}

func want(s string, parts ...string) string {
	var miss []string
	for _, p := range parts {
		if !strings.Contains(s, p) {
			miss = append(miss, fmt.Sprintf("lacks %q", p))
		}
	}
	if miss == nil {
		return ""
	}
	return strings.Join(miss, "; ") + fmt.Sprintf(" in %q", s)
}

func absent(s string, parts ...string) string {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return fmt.Sprintf("still holds %q; ", p)
		}
	}
	return ""
}
