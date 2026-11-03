package nl_test

const (
	mitQ   = "which packages are MIT licensed?"
	mit    = `pkg(?p, "MIT") => ?p`
	mitToo = `pkg(?x, "MIT") => ?x` // other names, the same rows
	apache = `pkg(?p, "Apache-2.0") => ?p`
	broken = `nope(?p) => ?p`
)
