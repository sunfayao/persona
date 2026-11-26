package filemeta

import (
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// ErrChanged reports a file no longer at the revision a download or transfer pinned.
var ErrChanged = &rpc.Error{Code: proto.CodeFileChanged, Message: "file changed during download; reopen it to try again"}
