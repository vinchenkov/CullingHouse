package verbs

import (
	"strings"
	"testing"
)

// The landing target is a BARE local branch name, and this is a correction to
// step 3 rather than a new decision. Step 3's envelope only checked
// `TargetRef != ""`, and its fixture used "refs/heads/main" — a shape guessed
// at, not read off the spine. `tasks.target_ref` is free-form text (schema.sql:786,
// 1..512) whose real values are bare names like "main", and for the FIRST-TASK
// setup arm it is a rev to resolve, where even "HEAD" is legitimate.
//
// For landing it cannot be free-form: the lander merges into
// `refs/heads/<target>` in the real operator repository. "HEAD" is meaningless
// there, a `refs/`-prefixed value would produce `refs/heads/refs/heads/main`,
// and the option-like and glob-like shapes below are the ones that turn a ref
// name into an argument or a pattern. Tightening the grammar is safe in the
// fail-closed direction: nothing produces a landing instruction yet, so the
// only effect is that the validator refuses more.

func TestLandingTargetBranchGrammarIsClosed(t *testing.T) {
	for _, ok := range []string{
		"main", "master", "develop", "release-2.1", "feature/login",
		"a", "v1.0.x", "team/sub/topic",
	} {
		if !validLandingTargetBranch(ok) {
			t.Fatalf("%q is a valid local branch name", ok)
		}
	}
	bad := map[string]string{
		"empty":            "",
		"head":             "HEAD",
		"fully-qualified":  "refs/heads/main",
		"remote-qualified": "refs/remotes/origin/main",
		"option":           "-delete",
		"dotdot":           "main..evil",
		"at-brace":         "main@{0}",
		"tilde":            "main~1",
		"caret":            "main^2",
		"colon":            "main:evil",
		"question":         "main?",
		"star":             "main*",
		"bracket":          "main[0]",
		"backslash":        "main\\evil",
		"space":            "main evil",
		"tab":              "main\tevil",
		"newline":          "main\nevil",
		"nul":              "main\x00evil",
		"control":          "main\x01",
		"del":              "main\x7f",
		"trailing-dot":     "main.",
		"lock-suffix":      "main.lock",
		"leading-slash":    "/main",
		"trailing-slash":   "main/",
		"double-slash":     "main//evil",
		"dot-component":    "team/.hidden",
		"leading-dot":      ".main",
		"lone-at":          "@",
		"too-long":         strings.Repeat("a", 256),
	}
	for name, value := range bad {
		if validLandingTargetBranch(value) {
			t.Fatalf("%s: %q was accepted as a landing target branch", name, value)
		}
	}
}

// The target must not be the task's own sealed branch. Landing merges the task
// branch INTO the target; if they were the same the merge would be a no-op
// against itself, and the CAS ref creation at ADR-017:748 would be creating the
// very ref it then merges from.
func TestLandingRefusesTheTaskBranchAsItsOwnTarget(t *testing.T) {
	env := landingEnvelope()
	env.TargetRef = taskAssignmentBranch(env.TaskID)
	if err := validateSetupEnvelope(env); err == nil {
		t.Fatal("a landing envelope targeted its own task branch")
	}
	plan := landingPlanFixture()
	plan.Landing.TargetRef = taskAssignmentBranch(plan.Landing.TaskID)
	if err := validatePrivateMountPlan(plan); err == nil {
		t.Fatal("a landing plan targeted its own task branch")
	}
}

// Both sides of the crossing enforce the same grammar. A shape the envelope
// refuses must not be reachable by way of the carrier, and vice versa.
func TestLandingTargetGrammarIsEnforcedOnBothSidesOfTheCrossing(t *testing.T) {
	for _, value := range []string{"refs/heads/main", "HEAD", "-delete", "main evil", "main.lock"} {
		env := landingEnvelope()
		env.TargetRef = value
		if err := validateSetupEnvelope(env); err == nil {
			t.Fatalf("envelope accepted target %q", value)
		}
		plan := landingPlanFixture()
		plan.Landing.TargetRef = value
		if err := validatePrivateMountPlan(plan); err == nil {
			t.Fatalf("carrier accepted target %q", value)
		}
	}
}
