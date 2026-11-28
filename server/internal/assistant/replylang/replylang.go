// Package replylang decides the language a model is told to answer and write in: the owner's reply-language setting
// when one is set, else the interface language.
package replylang

import (
	"slices"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// Choices are the values the setting takes besides "" (the interface language); prompts.AnswerLanguage reads them.
var Choices = []string{"English", "Simplified Chinese", "Japanese"}

// Interface is the language of the owner's interface by the name prompts ask for: its own name, "" for Simplified
// Chinese (the default, which prompts.AnswerLanguage reads as such).
func Interface(locale string) string {
	l := i18n.Lang(locale)
	if !i18n.Valid(l) || l == i18n.ZhHans {
		return ""
	}
	return l.Name()
}

// Resolve is what a prompt is given as the reply language: setting when it is one of Choices, else the interface
// language of locale.
func Resolve(setting, locale string) string {
	if Set(setting) {
		return setting
	}
	return Interface(locale)
}

// Set says whether setting names a reply language of its own.
func Set(setting string) bool { return slices.Contains(Choices, setting) }
