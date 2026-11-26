package node

import "github.com/nwverse/persona/server/internal/node/files"

func (n *Node) fileService() *files.Service {
	n.fileOnce.Do(func() {
		n.fileBrowser = files.New(files.Config{
			Home: n.home, RealPath: n.realPath, StatPath: n.statSafe, OffLimits: n.offLimitsResolved,
			TooManyLinks: errTooManyLinks, ExplainDenied: func(path string, err error) error {
				return explainDenied(path, n.home, err)
			},
			GitCommand: gitCmd, IgnoreCommand: fileIgnoreCommand,
		})
	})
	return n.fileBrowser
}
