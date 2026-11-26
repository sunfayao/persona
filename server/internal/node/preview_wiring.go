package node

import (
	"github.com/sunfayao/persona/server/internal/node/preview"
	"github.com/sunfayao/persona/server/internal/portprobe"
)

// previewProfileDir is a private cache folder because the profile stores dev app sign-ins.
func previewProfileDir() (string, error) { return privateCacheDir("preview-browser") }

func (n *Node) previewHost() *preview.Host {
	n.previewOnce.Do(func() {
		n.preview = preview.New(preview.Config{ProfileDir: previewProfileDir, DialPort: portprobe.Dial,
			Streams: n.streams, Logf: n.logger.Printf})
	})
	return n.preview
}
