package hub

// The Agents business is the manager's (internal/assistant, agents_*.go); these are the hub's ends: the lists show its
// report, the turns reach it, its cards are shown and answered, it fills in the starts.

// A test without a manager does not wait for one either.
func init() { managerStartWait = 0 }
