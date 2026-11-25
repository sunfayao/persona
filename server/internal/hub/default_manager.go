package hub

import (
	"context"
	"net/http"
	"time"
)

// defaultManagerNames: the default manager is connected and named capability in its hello.
func (h *Hub) defaultManagerNames(capability string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return h.archiveManagerHas(h.defaultOwner(ctx), capability) == http.StatusOK
}
