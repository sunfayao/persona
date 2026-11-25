package assistant

import (
	"strings"
	"testing"
	"time"
)

// #10099: the relay limits keep loops and spread in check without cutting off the main conversation's dispatching.

