// Package tui constructs and runs huh forms over a Registry and owns
// the lipgloss styles used for interactive output.
//
// # huh v2 Select pre-fill caveat
//
// huh v2's Select[T].Value(ptr) wires its internal Accessor at
// construction time, which writes the initially highlighted (first)
// option's value into the bound pointer before .Run() ever exits.
// The practical consequence is that an "untouched" single-pick comes
// back with the first option's id rather than the empty string, so
// the package treats single-pick fields as always-bound and lets
// ApplyDefaults' second pass act only on keys we genuinely omit
// (empty single-pick or empty multi-pick). MultiSelect does not have
// this quirk — an untouched multi-pick stays empty.
//
// The caveat is pinned in three places that any future maintainer
// should keep in lockstep when huh v2 evolves:
//
//   - the construction-time docstring in form.go / form2.go
//   - the long comment on SecondaryForm.Picks (form2.go)
//   - TestBuildSecondaryForm_SeedsDefaults (form2_test.go)
//
// See SecondaryForm.Picks's long comment for the full treatment plus
// the tri-state interaction.
package tui
