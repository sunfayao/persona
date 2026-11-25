package extmgr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nwverse/persona/server/internal/extension"
	"github.com/nwverse/persona/server/internal/i18n"
)

// extensions.download_mirrors: where the extension market downloads from when its own addresses are slow or blocked
// (Settings › Hub used to edit it; GET / PUT …/market-settings). The same record and the same check as the route.
func (h *Manager) downloadMirrorsSetting() selfSetting {
	title := ss(h.userLang(), "title_download_mirrors")
	return selfSetting{key: "extensions.download_mirrors", title: title,
		about: "mirrors the extension market downloads through: the registry's and the catalog's, each an https address or \"\" for none (the market's own address); " +
			"applies to the next market read",
		value: `{"registry_mirror": "https://…" or "", "catalog_mirror": "https://…" or ""}; both fields, "" clears one`,
		get: func(ctx context.Context) (string, error) {
			m, err := h.marketSettings(ctx)
			if err != nil {
				return "", err
			}
			raw, _ := json.Marshal(m)
			return string(raw), nil
		},
		set: func(ctx context.Context, v json.RawMessage) (*selfChange, error) {
			var in extension.MarketMirrors
			dec := json.NewDecoder(bytes.NewReader(v))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&in); err != nil {
				return nil, errors.New(`value must be {"registry_mirror", "catalog_mirror"}`)
			}
			if err := extension.ValidateMarketMirrors(in); err != nil {
				return nil, fmt.Errorf("a mirror must be a valid https address: %w", err)
			}
			before, err := h.marketSettings(ctx)
			if err != nil {
				return nil, err
			}
			l := h.userLang()
			if before == in {
				return &selfChange{summary: ss(l, "no_change", i18n.Args{"title": title})}, nil
			}
			if h.store == nil {
				return nil, errors.New("no database on this manager")
			}
			show := func(m extension.MarketMirrors) string {
				return fmt.Sprintf("registry %s, catalog %s", orNone(m.RegistryMirror), orNone(m.CatalogMirror))
			}
			return &selfChange{summary: changeSummary(l, title, show(before), show(in)),
				apply: func(ctx context.Context, _ map[string]string) (string, error) {
					if err := h.store.PutSetting(ctx, marketSettingsKey, in); err != nil {
						return "", err
					}
					return title + " saved.", nil
				}}, nil
		}}
}
