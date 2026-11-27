package extension

import (
	"errors"
	"testing"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// ParseCatalog's failures are all verification failures, and keep their localized message.
func TestParseCatalogErrorsAreUnverified(t *testing.T) {
	key, _ := testKey(t)
	_, err := ParseCatalog([]byte(`{"market_version":1,"name":"x","extensions":[]}`), key)
	if !errors.Is(err, ErrCatalogUnverified) || errors.Is(err, ErrCatalogUnavailable) {
		t.Fatalf("unsigned official catalog: %v", err)
	}
	var m *i18n.Message
	if !errors.As(err, &m) || m.Key != "extension-market_security.signature" || err.Error() != m.Error() {
		t.Fatalf("the localized reason was lost: %q", err.Error())
	}
}
