package translator

import (
	"context"
	"errors"
	"fmt"
	"time"

	agent "github.com/panyam/chakra"
	"github.com/panyam/jaala/frontends/nl"
)

// Extract asks the Config's models for the facts text states over the vocabulary's base relations, as
// a change set the host can apply (#414, #502): an nl.ExtractSession on chakra Runners, a turn per
// model, the model then the fallback, each reply read and checked by the session's
// ExtractConversation as the turn's TurnEnd hook. A refused draft goes back with where it went wrong;
// a repeated draft, or running out of tries, ends that model's turn. Each answer is held to
// ExtractGrammar when the provider takes a grammar, and o.Budget caps the tokens and time. The roles'
// tuning is Ask's, but for a thinking model's think line, which asks for a program; its last fenced
// block is still read as the draft. A pack whose examples or keys don't check fails Extract before
// any model is asked. The Trace holds every try, also when Extract fails, and the error then wraps
// nl.ErrNoFacts and the last try's.
func (t *Translator) Extract(ctx context.Context, text string, o nl.ExtractOptions) (nl.Extraction, nl.Trace, error) {
	l := &loop{t: t, o: nl.Options{Budget: o.Budget}, start: time.Now()}
	if o.Budget.Tokens > 0 {
		ctx = agent.WithTreeBudget(ctx, agent.TreeBudget{MaxTokens: o.Budget.Tokens})
	}
	l.parent = ctx
	if o.Budget.Time > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Budget.Time)
		defer cancel()
	}
	s, err := nl.NewExtractSession(text, t.v, o)
	if err != nil {
		return nl.Extraction{}, l.tr, err
	}
	roles := []*role{t.model}
	if t.fallback != nil {
		roles = append(roles, t.fallback)
	}
	var last error
	for _, r := range roles {
		x, err := l.extractTurn(ctx, s, r)
		if errors.Is(err, errTurnOver) {
			last = l.stop
			continue
		}
		return x, l.tr, err
	}
	return nl.Extraction{}, l.tr, fmt.Errorf("%w after %d tries: %w", nl.ErrNoFacts, len(l.tr.Tries), last)
}

// extractTurn asks r for the text's facts in a fresh conversation: one Runner turn ended by the
// conversation's verdicts. It returns the accepted extraction, errTurnOver when the turn ended without
// one (l.stop says why), or an error that ends the extraction.
func (l *loop) extractTurn(ctx context.Context, s *nl.ExtractSession, r *role) (nl.Extraction, error) {
	c := s.NewConversation()
	var accepted *nl.Extraction
	l.stop = nil
	cfg := agent.RunnerConfig{
		Provider:       r.provider,
		Instructions:   s.Prefix(),
		MaxSteps:       s.MaxTries() + 1,
		Generation:     r.gen,
		ResponseGuided: r.guided(s.Grammar()),
		TurnEnd: []agent.TurnEndHook{func(ctx context.Context, info agent.TurnEndInfo) (agent.TurnEndVerdict, error) {
			if err := l.timeUp(ctx); err != nil {
				return agent.TurnEndVerdict{}, err
			}
			vd := c.Judge(ctx, l.try(r, r.answer(info.Text), info.Usage))
			l.tr.Tries = append(l.tr.Tries, vd.Try)
			switch {
			case vd.Extraction != nil:
				accepted = vd.Extraction
				return agent.TurnEndVerdict{}, nil
			case vd.Stop != nil:
				l.stop = vd.Stop
				return agent.TurnEndVerdict{Stop: vd.Stop.Error()}, nil
			}
			return agent.TurnEndVerdict{Continue: vd.Continue, AsUser: true}, nil
		}},
		StepStages: []agent.StepStage{{Name: "jaala.translator.extract", Scope: agent.ScopeRequest, Run: func(ctx context.Context, _ agent.StepInfo, v agent.StepView) (agent.StepView, error) {
			if err := l.timeUp(ctx); err != nil {
				return v, err
			}
			v.ProviderName = r.label
			return v, nil
		}}},
	}
	_, err := l.runTurn(ctx, cfg, r, "", agent.TurnRequest{History: []agent.Message{{Role: agent.RoleUser, Text: s.Ask()}}})
	if err != nil {
		return nl.Extraction{}, err
	}
	if accepted == nil {
		return nl.Extraction{}, errTurnOver
	}
	return *accepted, nil
}
