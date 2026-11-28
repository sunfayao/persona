package lifecycle

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/sunfayao/persona/server/internal/apierr"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/proto"
)

func (d *Domain) Register(handle func(string, http.HandlerFunc)) {
	handle(assistantwire.TodoLifecycleRoute, d.serve)
	handle(assistantwire.SessionLifecycleRoute, d.serve)
}

func (d *Domain) serve(w http.ResponseWriter, r *http.Request) {
	if !d.deps.UserOnly(w, r) {
		return
	}
	id, machine, session := r.PathValue("id"), r.PathValue("mid"), r.PathValue("sid")
	if id != "" && !d.deps.AllowTodo(r.Context(), id) || id == "" && !d.deps.AllowSession(r.Context(), machine, session) {
		apierr.WriteCode(w, http.StatusNotFound, "lifecycle_not_found", i18n.Err("assistant-lifecycle.not_found"))
		return
	}
	q := r.URL.Query()
	after, before := q.Get("after"), q.Get("before")
	cursor := firstNonEmpty(after, before)
	var rev int64
	if cursor != "" {
		var err error
		rev, err = strconv.ParseInt(cursor, 10, 64)
		if err != nil || rev < 0 || after != "" && before != "" {
			apierr.WriteCode(w, http.StatusBadRequest, "lifecycle_invalid_cursor", i18n.Err("assistant-lifecycle.invalid_cursor"))
			return
		}
	}
	p, err := d.page(r.Context(), machine, session, id, rev, q.Has("after"), 50)
	if err != nil {
		apierr.WriteCode(w, http.StatusServiceUnavailable, "lifecycle_unavailable", i18n.Err("assistant-lifecycle.unavailable"))
		return
	}
	apierr.WriteJSON(w, p)
}

func firstNonEmpty(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

func (d *Domain) page(ctx context.Context, machine, session, todo string, cursor int64, forward bool, limit int) (proto.LifecyclePage, error) {
	st := d.deps.Store()
	if st == nil {
		return proto.LifecyclePage{}, errors.New("lifecycle store unavailable")
	}
	return st.LifecyclePage(ctx, machine, session, todo, cursor, forward, limit, d.deps.Now().Add(-retention))
}
