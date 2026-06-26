// Package prompt assembles the final instruction string sent to the model.
//
// The native llama.cpp /completion endpoint does not apply a model's chat
// template — it takes a raw string. So grease is responsible for framing
// system/user content in whatever format the loaded model expects. That framing
// lives behind the Template interface so swapping models with a different
// template is a one-line change, not a hunt through prompt-building code.
//
// The grammar enforces output shape independently of the template, so a
// template mismatch degrades draft quality but cannot corrupt the extraction
// struct's shape. The failure mode is soft, which is why a sensible default
// (ChatML) is acceptable rather than requiring per-model configuration up front.
package prompt

import "strings"

// Template frames a system instruction and a user message into the single
// prompt string a completion endpoint expects, including whatever trailing
// tokens cue the model to begin its response.
type Template interface {
	// Render produces the full prompt. An empty system string omits the system
	// turn entirely (some models prefer no default system message).
	Render(system, user string) string
	// Stop returns the strings that mark the end of the model's turn for this
	// template, suitable for passing as stop sequences so generation halts
	// cleanly at the turn boundary.
	Stop() []string
}

// ChatML implements the ChatML format used by Qwen2.5 (and compatible models):
//
//	<|im_start|>system\n{system}<|im_end|>\n<|im_start|>user\n{user}<|im_end|>\n<|im_start|>assistant\n
//
// This is the shipped default. For a model using a different template (e.g.
// Llama 3.1's <|start_header_id|> format), supply a different Template
// implementation, or front the model with llama.cpp's /apply-template endpoint.
type ChatML struct{}

func (ChatML) Render(system, user string) string {
	var b strings.Builder
	if strings.TrimSpace(system) != "" {
		b.WriteString("<|im_start|>system\n")
		b.WriteString(system)
		b.WriteString("<|im_end|>\n")
	}
	b.WriteString("<|im_start|>user\n")
	b.WriteString(user)
	b.WriteString("<|im_end|>\n")
	b.WriteString("<|im_start|>assistant\n")
	return b.String()
}

func (ChatML) Stop() []string {
	// Halt at the end-of-turn marker so generation doesn't run on into a
	// hallucinated next turn.
	return []string{"<|im_end|>"}
}
