package slashcommands

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
	"golang.org/x/sync/singleflight"
)

const (
	freshFor = 30 * time.Second
	retryIn  = 4 * time.Second
)

type Target struct {
	Agent, Launcher, Cwd, Profile, Fingerprint string
}

type Command func(context.Context, ...string) (*exec.Cmd, error)
type Skills func(context.Context) []proto.SlashCommand

type cached struct {
	rows   []proto.SlashCommand
	expiry time.Time
}

type Catalog struct {
	now     func() time.Time
	mu      sync.Mutex
	entries map[Target]cached
	flight  singleflight.Group
	workers chan struct{}
}

func New(now func() time.Time) *Catalog {
	return &Catalog{now: now, entries: make(map[Target]cached), workers: make(chan struct{}, 4)}
}

func (c *Catalog) get(key Target) (cached, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.entries[key]
	return v, ok && c.now().Before(v.expiry)
}

func (c *Catalog) List(ctx context.Context, key Target, command Command, local Skills) []proto.SlashCommand {
	if v, ok := c.get(key); ok {
		return v.rows
	}
	res := c.flight.DoChan(strings.Join([]string{key.Agent, key.Launcher, key.Cwd, key.Profile, key.Fingerprint}, "\x00"), func() (any, error) {
		if v, ok := c.get(key); ok {
			return v.rows, nil
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		select {
		case c.workers <- struct{}{}:
			defer func() { <-c.workers }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		rows := discover(ctx, key, command, local)
		ttl := freshFor
		if len(rows) == 0 {
			ttl = retryIn
		}
		c.mu.Lock()
		if len(c.entries) >= 128 {
			clear(c.entries)
		}
		c.entries[key] = cached{rows, c.now().Add(ttl)}
		c.mu.Unlock()
		return rows, nil
	})
	select {
	case v := <-res:
		if v.Err == nil {
			return v.Val.([]proto.SlashCommand)
		}
	case <-ctx.Done():
	}
	return []proto.SlashCommand{}
}

var commandName = lazyre.New(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)

func normalize(sets ...[]proto.SlashCommand) []proto.SlashCommand {
	rows := []proto.SlashCommand{}
	seen := map[string]bool{}
	for _, set := range sets {
		for _, row := range set {
			name := strings.TrimPrefix(row.Name, "/")
			folded := strings.ToLower(name)
			if !commandName.MatchString(name) || seen[folded] {
				continue
			}
			seen[folded] = true
			row.Name = "/" + name
			row.Description = strings.Join(strings.FieldsFunc(row.Description, func(r rune) bool {
				return unicode.IsSpace(r) || unicode.IsControl(r)
			}), " ")
			if chars := []rune(row.Description); len(chars) > 220 {
				row.Description = string(chars[:219]) + "…"
			}
			rows = append(rows, row)
			if len(rows) == 512 {
				return rows
			}
		}
	}
	return rows
}

var ErrTarget = errors.New("native CLI target is unavailable")
