package app

import "context"

// LanguageSetting reads and changes the language the CLI prints in. Which
// codes are valid is the CLI's business: it owns the translations.
type LanguageSetting struct {
	Settings SettingsRepository
}

// Get returns the saved language code, or "" when the system decides.
func (h LanguageSetting) Get(ctx context.Context) (string, error) {
	if h.Settings == nil {
		return "", nil
	}
	return h.Settings.Language(ctx)
}

// Set saves code; "" goes back to following the system.
func (h LanguageSetting) Set(ctx context.Context, code string) error {
	return h.Settings.SetLanguage(ctx, code)
}
