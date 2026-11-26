package conformance

import (
	"encoding/json"

	"github.com/sunfayao/persona/sdk/go/wire"
	"github.com/sunfayao/persona/server/internal/connector"
)

func checkCatalog(raw json.RawMessage) (connector.CatalogResult, error) {
	var result connector.CatalogResult
	if err := decodeWire(raw, &result); err != nil {
		return result, err
	}
	if err := wire.CheckConnectorCatalogJSON(raw); err != nil {
		return result, err
	}
	return result, nil
}
