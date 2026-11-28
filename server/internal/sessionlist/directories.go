package sessionlist

import (
	"strings"

	"github.com/sunfayao/persona/server/internal/proto"
)

// DirectoryAliases holds directory spellings proved equivalent on one machine.
type DirectoryAliases struct {
	home, physicalHome string
	dirs               map[string]string
}

func NewDirectoryAliases(home, physicalHome string) DirectoryAliases {
	return DirectoryAliases{home: home, physicalHome: physicalHome, dirs: make(map[string]string)}
}

// Observe records node-verified spellings. A conflicting observation cannot
// resolve another row's unverified cwd.
func (a DirectoryAliases) Observe(cwd, physical string) {
	cwd = a.clean(cwd)
	if cwd == "" || !proto.NodeAbs(physical) {
		return
	}
	physical = proto.NodeClean(physical)
	if before, ok := a.dirs[cwd]; ok && before != physical {
		physical = ""
	}
	a.dirs[cwd] = physical
}

func (a DirectoryAliases) clean(cwd string) string {
	if a.home != "" {
		if cwd == "~" {
			cwd = a.home
		} else if tail, ok := strings.CutPrefix(cwd, "~/"); ok {
			cwd = proto.NodeJoin(a.home, tail)
		}
	}
	if !proto.NodeAbs(cwd) {
		return ""
	}
	return proto.NodeClean(cwd)
}

// Path uses the node-verified path first, then the evidence from its other
// rows and the local machine's verified home alias.
func (a DirectoryAliases) Path(cwd, physical string) string {
	if proto.NodeAbs(physical) {
		return proto.NodeClean(physical)
	}
	cwd = a.clean(cwd)
	if real := a.dirs[cwd]; real != "" {
		return real
	}
	// HomeNamed rewrites from its third argument to its second. The catalog
	// displays the physical spelling shared with workspace_cwd.
	return proto.HomeNamed(cwd, a.physicalHome, a.home)
}
