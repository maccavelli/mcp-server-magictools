package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pterm/pterm"

	"github.com/maccavelli/mcplib/wizard"
)

// ptermPrompter renders wizard.Prompter over pterm, so the canonical LLM
// configuration flow in mcplib keeps this binary's existing look and feel.
//
// mcplib deliberately does not depend on pterm: nine of its twelve consumers
// are headless MCP servers that will never draw a menu. The rendering therefore
// lives here, beside the wizard that already uses it.
type ptermPrompter struct{}

var _ wizard.Prompter = ptermPrompter{}

// promptMenuHeight bounds a scrolling menu. Nine providers plus a manual-entry
// option fit without scrolling; a live model listing does not, and pterm
// scrolls it.
const promptMenuHeight = 12

// optionText renders one choice as a numbered entry, matching the numbering
// this menu has always used ("1. Gemini").
func optionText(i int, c wizard.Choice) string {
	if c.Detail == "" {
		return fmt.Sprintf("%d. %s", i+1, c.Label)
	}
	return fmt.Sprintf("%d. %s — %s", i+1, c.Label, c.Detail)
}

// optionIndex recovers a choice index from its rendered text. pterm returns the
// selected string rather than its position, and two choices may share a label,
// so the index is carried in the rendering rather than recovered by matching it.
func optionIndex(text string) (int, bool) {
	dot := strings.IndexByte(text, '.')
	if dot <= 0 {
		return 0, false
	}
	n, err := strconv.Atoi(text[:dot])
	if err != nil || n < 1 {
		return 0, false
	}
	return n - 1, true
}

func optionTexts(choices []wizard.Choice) []string {
	out := make([]string, len(choices))
	for i, c := range choices {
		out[i] = optionText(i, c)
	}
	return out
}

func (ptermPrompter) Select(title string, choices []wizard.Choice, defaultIdx int) (int, error) {
	opts := optionTexts(choices)
	def := ""
	if defaultIdx >= 0 && defaultIdx < len(opts) {
		def = opts[defaultIdx]
	}
	selected, err := pterm.DefaultInteractiveSelect.
		WithMaxHeight(promptMenuHeight).
		WithOptions(opts).
		WithDefaultOption(def).
		Show(title)
	if err != nil {
		return 0, err
	}
	idx, ok := optionIndex(selected)
	if !ok || idx >= len(choices) {
		return 0, fmt.Errorf("unrecognised selection %q", selected)
	}
	return idx, nil
}

func (ptermPrompter) MultiSelect(title string, choices []wizard.Choice, preselected []int) ([]int, error) {
	opts := optionTexts(choices)
	var defaults []string
	for _, i := range preselected {
		if i >= 0 && i < len(opts) {
			defaults = append(defaults, opts[i])
		}
	}
	selected, err := pterm.DefaultInteractiveMultiselect.
		WithMaxHeight(promptMenuHeight).
		WithOptions(opts).
		WithDefaultOptions(defaults).
		WithDefaultText(title).
		Show()
	if err != nil {
		return nil, err
	}
	out := make([]int, 0, len(selected))
	for _, s := range selected {
		idx, ok := optionIndex(s)
		if !ok || idx >= len(choices) {
			return nil, fmt.Errorf("unrecognised selection %q", s)
		}
		out = append(out, idx)
	}
	return out, nil
}

func (ptermPrompter) Confirm(question string, def bool) (bool, error) {
	return pterm.DefaultInteractiveConfirm.WithDefaultValue(def).Show(question)
}

func (ptermPrompter) Input(prompt, def string) (string, error) {
	return pterm.DefaultInteractiveTextInput.WithDefaultValue(def).Show(prompt)
}

// Secret delegates to mcplib's text prompter rather than using pterm's
// WithMask. pterm masks every rune uniformly, so a user pasting a key sees no
// evidence it arrived and cannot tell which key it is; the mcplib
// implementation reveals a masked tail for exactly that reason.
func (ptermPrompter) Secret(prompt string) (string, error) {
	return wizard.NewTextPrompter().Secret(prompt)
}

func (ptermPrompter) Notify(level wizard.Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	switch level {
	case wizard.LevelWarn:
		pterm.Warning.Println(msg)
	case wizard.LevelError:
		pterm.Error.Println(msg)
	case wizard.LevelInfo:
		pterm.Info.Println(msg)
	default:
		pterm.Info.Println(msg)
	}
}
