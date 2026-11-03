package ns

import (
	"context"
	"fmt"
)

// A Builtin is a predicate the engine computes rather than looks up. It is exactly one of two kinds.
//
// A FILTER (Holds set) tests arguments that must all be bound, keeping a binding when Holds is true.
// Because negation runs the same test and asks whether it yielded, `not name(...)` keeps a binding
// exactly when Holds is false, so the two directions cannot disagree. An unbound argument is an
// error, the same shape as an unbound comparison operand.
//
// A GENERATOR (Gen set) PRODUCES values, enumerating solutions from wherever the host keeps them.
// That is the property that makes clause order matter: a generator whose own input is unbound
// enumerates from every candidate, so appearing first in a rule body is a whole-dataset scan no later
// literal can undo. Modes says which bindings a generator accepts, so an engine can schedule it once
// one is satisfied and refuse a body that can never satisfy any. A generator must also only ever emit
// values drawn from
// finite host data, or evaluation stops being guaranteed to terminate.
type Builtin struct {
	// Arity is the argument count, or the minimum when MaxArity is set.
	Arity int
	// MaxArity, when non-zero, is the inclusive maximum argument count. It exists so an optional
	// trailing argument is admitted by the POSITIVE path and the NEGATION path through one test
	// (Accepts) rather than two length checks that could drift.
	MaxArity int
	// Holds is a filter's test over its (all-bound) argument values.
	Holds func(args []Value) (bool, error)
	// Modes lists the binding patterns a generator accepts: in each mode, the positions that must be
	// bound when it is called. A generator that walks out from either end of a path has two modes,
	// {true, false, ...} and {false, true, ...}. A generator that may enumerate with nothing bound
	// says so with an all-false mode, so a full scan is a declaration rather than an accident.
	//
	// It is required for a generator, with every mode as long as the generator's longest call (MaxArity
	// when set, else Arity); a position past a call's own arity is not required. A filter needs none:
	// it is always called with every argument bound.
	Modes [][]bool
	// Labels names the arguments, and Types says what each denotes, exactly as for a base relation's
	// Schema. Both are optional; they are what a host lists and what column kinds are read from.
	Labels []string
	Types  []ArgType
	// Doc is a one-line description a host shows when the predicate is listed.
	Doc string
	// Gen is a generator's enumeration. args holds each argument's value where the binding fixes
	// it. Gen calls emit once per solution with a value for EVERY argument, plus the citations that
	// justify it; the engine unifies each solution against the atom, so one that disagrees with a
	// bound argument is dropped there rather than in Gen. src is the Source the query runs over, for a generator
	// that reads the host's data through it.
	//
	// The engine copies what it needs out of vals and cites before emit returns and never keeps
	// either slice, so a generator may reuse one buffer for every solution it emits.
	//
	// ctx is the query's: when it is cancelled or its deadline passes, Gen should stop and return
	// ctx.Err(), so a long walk does not outlive the request that asked for it. emit also returns an
	// error when the query stops (cancelled, or over its work budget, which counts every emitted
	// solution), and Gen should return it as is.
	Gen func(ctx context.Context, src Source, args []Arg, emit func(vals []Value, cites []string) error) error
	// Aliased marks a generator's arguments where a bound value can name a row its enumeration lists
	// under another value, such as str.split's index, where -1 names the last part and enumerating
	// lists it by its index from 0. The engine passes such an argument to Gen bound only when the call
	// writes a constant there (a host's Bind of the goal's variable writes one). A value bound while
	// the query runs, by a join or by a rewrite passing a caller's constant down, is matched against
	// the rows Gen lists instead, so the answer doesn't depend on the order or the rewrite that reached
	// the call (#480). nil marks none. No mode may require an aliased argument.
	Aliased []bool
	// Volatile marks a generator or filter whose answers can change while its Source's facts don't,
	// such as one reading the clock, the network, or a host index updated in place. A Base keeps the
	// derived relations a query evaluated in full and reuses them in later queries, which assumes every
	// predicate is a function of its arguments and the Source; a relation that reads a Volatile one,
	// directly or through other derived relations, is never kept.
	Volatile bool
}

// An Arg is one argument as a generator sees it: its value when the binding fixes it.
type Arg struct {
	Value Value
	Bound bool
}

// IsGenerator reports whether the builtin produces values rather than filtering them.
func (bi Builtin) IsGenerator() bool { return bi.Gen != nil }

// Accepts reports whether n is a valid argument count for this builtin.
func (bi Builtin) Accepts(n int) bool {
	if bi.MaxArity == 0 {
		return n == bi.Arity
	}
	return n >= bi.Arity && n <= bi.MaxArity
}

// ArityLabel renders the accepted arity for an error message ("2" or "2 or 3").
func (bi Builtin) ArityLabel() string {
	if bi.MaxArity == 0 {
		return fmt.Sprintf("%d", bi.Arity)
	}
	return fmt.Sprintf("%d or %d", bi.Arity, bi.MaxArity)
}

// Satisfied reports whether a call whose bound positions are bound satisfies one of the builtin's
// modes. A filter is satisfied only when every argument is bound.
func (bi Builtin) Satisfied(bound []bool) bool {
	if !bi.IsGenerator() {
		for _, b := range bound {
			if !b {
				return false
			}
		}
		return true
	}
	for _, m := range bi.Modes {
		ok := true
		for i, need := range m {
			if need && i < len(bound) && !bound[i] { // a position past the call's arity is not required
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// Filter builds a filter Builtin from a boolean over its all-bound argument values.
func Filter(arity int, holds func(args []Value) (bool, error)) Builtin {
	return Builtin{Arity: arity, Holds: holds}
}
