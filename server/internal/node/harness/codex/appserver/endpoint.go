package appserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/osproc"
)

func rejectCodexRedirect(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

func (c *Client) Endpoint(endpoint string) (string, *http.Client, error) {
	parse := endpoint
	if rest, ok := strings.CutPrefix(endpoint, "unix://"); ok && runtime.GOOS == "windows" {
		parse = "unix:///" + filepath.ToSlash(rest) // C:\… is no URL; the checks below want only its query and the like
	}
	u, err := url.Parse(parse)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", nil, errors.New("app-server 入口无效")
	}
	if u.Scheme == "unix" {
		path := strings.TrimPrefix(endpoint, "unix://")
		if !filepath.IsAbs(path) {
			return "", nil, errors.New("Unix 入口必须是绝对路径")
		}
		path, err := SocketPath(path)
		if err != nil {
			return "", nil, err
		}
		if err := privateSocket(path); err != nil {
			return "", nil, err
		}
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return c.dialUnix(ctx, "unix", path)
		}}
		return "ws://localhost/", &http.Client{Transport: tr, CheckRedirect: rejectCodexRedirect}, nil
	}
	if u.Scheme != "ws" || u.Port() == "" || u.Path != "" && u.Path != "/" {
		return "", nil, errors.New("只支持本机 Unix 或回环 WebSocket 入口")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return "", nil, errors.New("WebSocket 入口必须是回环 IP，不能是外部地址或主机名")
	}
	// Never use HTTP_PROXY for a private local endpoint.
	return endpoint, &http.Client{Transport: &http.Transport{DialContext: c.dial}, CheckRedirect: rejectCodexRedirect}, nil
}

// SocketPath is the socket a Unix control endpoint names: the path itself (0.153), or — from 0.163, where every
// control socket is an alias — the one target Codex itself recognises as its own: a socket named after the SHA-256 of
// the alias's canonical path in <canonical /tmp>/codex-daemon-<uid> (codex-rs uds/src/daemon_directory.rs,
// app-server-transport unix_socket.rs). Any other symlink is refused; the alias's directory must be the user's and not
// writable by others. The caller checks the target's own directory and mode.
func SocketPath(path string) (string, error) {
	st, err := os.Lstat(path)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		return path, nil // a missing path fails the caller's checks
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", errors.New("Unix 入口不可访问")
	}
	if pst, err := os.Stat(parent); err != nil {
		return "", errors.New("Unix 入口不可访问")
	} else if !osproc.OwnedByMe(pst) || pst.Mode().Perm()&0o022 != 0 {
		return "", errors.New("Unix 入口所在目录不属于当前用户，或别人能改")
	}
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return "", errors.New("Unix 入口不可访问")
	}
	sum := sha256.Sum256([]byte(filepath.Join(parent, filepath.Base(path))))
	want := filepath.Join(tmp, "codex-daemon-"+strconv.Itoa(os.Geteuid()), hex.EncodeToString(sum[:]))
	if target, err := os.Readlink(path); err != nil || target != want {
		return "", errors.New("Unix 入口是指向别处的符号链接")
	}
	return want, nil
}
