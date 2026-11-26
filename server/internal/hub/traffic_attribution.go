package hub

import (
	"net/http"
	"strings"

	"github.com/sunfayao/persona/server/internal/compat"
)

// Attribution is observational, never an authentication or authorization input.
type trafficAttribution struct {
	platform, source, platformBasis, sourceBasis string
}

func (h *Hub) requestTrafficAttribution(r *http.Request, p principal) trafficAttribution {
	a := trafficAttribution{platform: "unknown", source: "unknown", platformBasis: "unknown", sourceBasis: "unknown"}
	client := requestClientOf(r.Context())
	if compat.HeaderConflict(r.Header, "X-Persona-Client") {
		client = requestClient{}
	} else if client.Platform == "" {
		client = parseRequestClient(compat.Header(r.Header, "X-Persona-Client"))
	}
	if platform := trafficPlatform(client.Platform); platform != "unknown" {
		a.platform, a.platformBasis = platform, "client-header"
	}
	ua := r.UserAgent()
	if len(ua) > 512 {
		ua = ""
	} // do not retain an oversized, client-controlled string
	ua = strings.ToLower(ua)
	diagnostic := strings.HasPrefix(ua, "curl/") || strings.HasPrefix(ua, "python-") || strings.HasPrefix(ua, "go-http-client/") || strings.HasPrefix(ua, "wget/")
	browser := strings.Contains(ua, "mozilla/") || r.Header.Get("Sec-Fetch-Mode") != "" || r.Header.Get("Sec-CH-UA") != ""
	if a.platform == "unknown" {
		switch {
		case browser:
			a.platform, a.platformBasis = "web", "user-agent"
		case !diagnostic && p.Kind == "web":
			a.platform, a.platformBasis = "web", "credential"
		}
	}
	if a.source == "unknown" {
		switch {
		case diagnostic:
			a.source, a.sourceBasis = "diagnostic", "user-agent"
		case browser || a.platform == "web":
			a.source, a.sourceBasis = "app", "browser"
		}
	}
	return a
}
