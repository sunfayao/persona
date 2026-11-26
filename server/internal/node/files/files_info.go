package files

import (
	"errors"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sunfayao/persona/server/internal/filemeta"
	"github.com/sunfayao/persona/server/internal/pathutil"
)

// The file page's Info sheet (#5827, docs/protocol/protocol.md "File details and links"): fs.stat with detail adds what
// Finder's Get Info shows: the permission bits, the owner and group, the times, a folder's number of items, and for a
// symlink what it says, where it leads and whether that is there, and for a file the type the file page opens it as (its
// first bytes read where the name cannot tell, #7974). Nothing is read that fs.stat would refuse: the path passes the
// same guard, and a link is looked at as itself first (its folder resolved, the link not followed).

// maxInfoItems bounds the count of a folder's entries (a folder of millions is "more than 10 000").
const maxInfoItems = 10000

// fileOwner is the uid, gid and the times only the system's own stat has (fileInfoSys, per platform).
type fileOwner struct {
	uid, gid   int
	ok         bool
	ctime      time.Time
	birth      time.Time // zero where the file system or the platform does not keep it
	birthKnown bool
}

func (n *Service) statDetail(asked, cwd string) PathStat {
	s := PathStat{Path: asked, Kind: "missing"}
	p := pathutil.Resolve(asked, cwd, n.config.Home)
	if p == "" {
		return s
	}
	p = pathutil.Abs(p) // its own name spelled out too, not only its folder's (a Windows short name, #9891)
	// The link itself: its folder through the guard, its own name not followed.
	dir, err := n.config.RealPath(filepath.Dir(p))
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			s.Kind = "denied"
		}
		return s
	}
	self := filepath.Join(dir, filepath.Base(p))
	if err := n.config.OffLimits(self); err != nil {
		return s
	}
	lst, err := os.Lstat(self)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			s.Kind = "denied"
		}
		return s
	}
	info, at := lst, self
	if lst.Mode()&fs.ModeSymlink != 0 {
		s.Symlink = true
		if target, err := os.Readlink(self); err == nil {
			s.LinkTarget = target
		}
		real, st, err := n.config.StatPath(p)
		switch {
		case err == nil:
			s.LinkReal, info, at = real, st, real
		case errors.Is(err, fs.ErrNotExist) || errors.Is(err, n.config.TooManyLinks):
			s.LinkBroken = true
		case errors.Is(err, fs.ErrPermission):
			s.Kind = "denied"
			return s
		default: // a link into a secret or iCloud Drive: what it says, nothing about what it leads to
			return s
		}
	}
	switch {
	case s.LinkBroken:
		s.Kind = "link"
	case info.IsDir():
		s.Kind = "dir"
		s.Items, s.ItemsMore = countItems(at)
	default:
		s.Kind = "file"
		s.Size = info.Size()
		if info.Mode().IsRegular() {
			s.Mime = filemeta.MIMEAt(p, at)
		}
	}
	mode := filemeta.UnixMode(info.Mode())
	s.Mode = &mode
	mt := info.ModTime()
	s.ModTime = &mt
	if o := fileInfoSys(info, at); o.ok {
		s.Owner, s.Group = n.userName(o.uid), n.groupName(o.gid)
		if !o.ctime.IsZero() {
			s.ChangeTime = &o.ctime
		}
		if o.birthKnown && !o.birth.IsZero() {
			s.BirthTime = &o.birth
		}
	} else if bt := o.birth; o.birthKnown && !bt.IsZero() { // Windows: no owner here, a creation time
		s.BirthTime = &bt
	}
	return s
}

// countItems counts a folder's entries, up to maxInfoItems (more: true).
func countItems(dir string) (*int, bool) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	count := 0
	for count <= maxInfoItems {
		names, err := f.Readdirnames(1000)
		count += len(names)
		if err != nil {
			break
		}
	}
	if count > maxInfoItems {
		c := maxInfoItems
		return &c, true
	}
	return &count, false
}

// userName is a uid's name: this process's own user from user.Current (which also works without cgo), others from the
// system's tables, else the number.
func (n *Service) userName(uid int) string {
	n.namesMu.Lock()
	defer n.namesMu.Unlock()
	if name, ok := n.users[uid]; ok {
		return name
	}
	name := strconv.Itoa(uid)
	if uid == os.Getuid() {
		if u, err := user.Current(); err == nil && u.Username != "" {
			name = u.Username
		}
	} else if u, err := user.LookupId(name); err == nil && u.Username != "" {
		name = u.Username
	}
	n.users[uid] = name
	return name
}

func (n *Service) groupName(gid int) string {
	n.namesMu.Lock()
	defer n.namesMu.Unlock()
	if name, ok := n.groups[gid]; ok {
		return name
	}
	name := strconv.Itoa(gid)
	if g, err := user.LookupGroupId(name); err == nil && g.Name != "" {
		name = g.Name
	}
	n.groups[gid] = name
	return name
}
