package hub

import "time"

func (c *client) recordSocketTraffic(event, action string, bytes int64, elapsed time.Duration) {
	if c.hub == nil || c.device == "" {
		return
	}
	c.mu.Lock()
	a := c.traffic
	if platform := trafficPlatform(c.meta.Platform); platform != "unknown" {
		a.platform, a.platformBasis = platform, "client-hello"
	}
	c.mu.Unlock()
	if a.platform == "" {
		a.platform = "unknown"
	}
	c.hub.recordTraffic(trafficKey{minute: time.Now(), identity: trafficIdentity(c.device), registration: "none", platform: a.platform, source: a.source, platformBasis: a.platformBasis, sourceBasis: a.sourceBasis, route: "/api/v1/ws", event: event, action: action}, bytes, elapsed)
}
