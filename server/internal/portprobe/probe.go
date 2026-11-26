package portprobe

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/lazyre"
)

const (
	// Wait: a dev server compiling its first page (next dev) takes seconds to answer; one that accepted the
	// connection and said nothing by then is listed without a title and asked again next time.
	Wait         = 3 * time.Second
	maxPortTitle = 60 // characters
	// UserAgent is the User-Agent of the probe's GET /: a server can tell it from its real clients (the tests'
	// servers do, as a node on the machine probes them too). A node installed before the Persona-only rename (#6079)
	// sends its original User-Agent until it is replaced, so what recognizes the probe has to accept both (the tests'
	// UserAgents). Renaming it again leaves every installed node's probe unrecognized the same way.
	UserAgent = "persona-ports"
)

// Result describes whether a listener answered HTTP, or accepted a connection without answering yet.
type Result struct {
	HTTP    bool
	Pending bool
	Title   string
}

// dialTargets are the addresses to reach a listener bound to addr, from this machine.
func dialTargets(port int, addr string) []string {
	p := strconv.Itoa(port)
	switch addr {
	case "", "0.0.0.0", "*":
		return []string{"127.0.0.1:" + p, "[::1]:" + p}
	case "::", "[::]":
		return []string{"[::1]:" + p, "127.0.0.1:" + p}
	}
	return []string{net.JoinHostPort(strings.Trim(addr, "[]"), p)}
}

func Dial(ctx context.Context, port int, addr string) (net.Conn, error) {
	var d net.Dialer
	var err error
	for _, a := range dialTargets(port, addr) {
		var c net.Conn
		if c, err = d.DialContext(ctx, "tcp", a); err == nil {
			return c, nil
		}
	}
	return nil, err
}

var titleRe = lazyre.New(`(?is)<title[^>]*>(.*?)</title>`)

// Probe asks GET / of a listener as a browser on this machine would (Host localhost:port, which dev servers'
// host checks accept) and reads the page's title.
func Probe(ctx context.Context, port int, addr string, now func() time.Time) Result {
	ctx, cancel := context.WithTimeout(ctx, Wait)
	defer cancel()
	conn, err := Dial(ctx, port, addr)
	if err != nil {
		return Result{}
	}
	defer conn.Close()
	_ = conn.SetDeadline(now().Add(Wait))
	host := "localhost:" + strconv.Itoa(port)
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\nUser-Agent: %s\r\nAccept: text/html\r\nConnection: close\r\n\r\n", host, UserAgent)
	br := bufio.NewReader(conn)
	head, err := br.Peek(5)
	if err != nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() && len(head) == 0 {
			return Result{HTTP: true, Pending: true} // accepted, still working on it
		}
		return Result{}
	}
	if string(head) != "HTTP/" {
		return Result{}
	}
	res, err := http.ReadResponse(br, nil)
	if err != nil {
		return Result{HTTP: true}
	}
	defer res.Body.Close()
	if !strings.Contains(res.Header.Get("Content-Type"), "html") {
		return Result{HTTP: true}
	}
	body, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	return Result{HTTP: true, Title: pageTitle(string(body))}
}

func pageTitle(page string) string {
	m := titleRe.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	t := strings.Join(strings.Fields(html.UnescapeString(m[1])), " ")
	if r := []rune(t); len(r) > maxPortTitle {
		t = strings.TrimSpace(string(r[:maxPortTitle])) + "…"
	}
	return t
}
