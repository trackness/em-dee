// Package tui owns the huh form construction for em-dee's interactive
// flow. The flow is **two sequential forms**: form 1 resolves the
// language, form 2 (built in Task 4.3) presents the chosen language's
// sub-categories + cross-cutting categories + a confirm group. Two
// .Run() calls, not one form with dynamic options.

package tui

import (
	"errors"
	"fmt"

	"charm.land/huh/v2"

	"github.com/trackness/em-dee/internal/registry"
)

// ErrCancelled is the sentinel returned by the form runners when the
// user aborts the form via Ctrl-C / Esc. Wrapped over huh's own
// ErrUserAborted so callers can switch on `errors.Is(err, ErrCancelled)`
// without needing to import huh directly.
var ErrCancelled = errors.New("cancelled by user")

// BuildLanguageForm constructs the form-1 select: a single huh.Form
// with one huh.Group containing one huh.Select[string] populated from
// the language category's options in registry order. The chosen value
// is bound to *out. No pre-selection — huh.Select still highlights one
// row but does not commit a value until Enter is pressed.
//
// Construction is split from Run so unit tests can inspect the form
// without needing a TTY (per the testing constraint in the plan).
func BuildLanguageForm(reg *registry.Registry, out *string) (*huh.Form, error) {
	if reg == nil {
		return nil, errors.New("BuildLanguageForm: nil registry")
	}
	if out == nil {
		return nil, errors.New("BuildLanguageForm: nil out pointer")
	}

	var lang *registry.Category
	for _, c := range reg.Categories {
		if c.ID == registry.LanguageCategoryID {
			lang = c
			break
		}
	}
	if lang == nil {
		return nil, errors.New("BuildLanguageForm: registry has no language category")
	}
	if len(lang.Options) == 0 {
		return nil, errors.New("BuildLanguageForm: language category has no options")
	}

	opts := make([]huh.Option[string], 0, len(lang.Options))
	for _, o := range lang.Options {
		opts = append(opts, huh.NewOption(o.DisplayName, o.ID))
	}

	sel := huh.NewSelect[string]().
		Title(lang.DisplayName).
		Description("Choose the primary language for this project.").
		Options(opts...).
		Value(out).
		Validate(func(s string) error {
			// Belt-and-braces: huh.Select with non-empty options can't
			// actually commit "" via Enter, but if huh's behaviour
			// drifts we still surface a clean error.
			if s == "" {
				return errors.New("language is required")
			}
			return nil
		})

	return huh.NewForm(huh.NewGroup(sel)), nil
}

// RunLanguageForm runs the form-1 select and returns the chosen
// language id. On user cancellation (Ctrl-C / Esc) it returns
// ErrCancelled — wrapped over huh.ErrUserAborted so callers don't need
// to import huh. Any other error propagates.
//
// The bound `out` variable starts empty; the spec mandates "no
// pre-selected value." huh's Select still highlights the first row on
// open, but the user must press Enter to commit.
func RunLanguageForm(reg *registry.Registry) (string, error) {
	var chosen string
	form, err := BuildLanguageForm(reg, &chosen)
	if err != nil {
		return "", err
	}
	if err := form.Run(); err != nil {
		if errors.Is(err, huh.ErrUserAborted) {
			return "", ErrCancelled
		}
		return "", fmt.Errorf("run language form: %w", err)
	}
	return chosen, nil
}
