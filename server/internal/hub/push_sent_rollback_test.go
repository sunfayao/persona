package hub

// sentRecordStore is what the delivered pushes use of the hub's store: its settings and the hub_push_sent table.
type sentRecordStore interface {
	Store
	pushSentStore
}

// oldBuild is the hub from before hub_push_sent as far as the delivered pushes go (push_grading.go on main before #8257):
// at start it reads only the hub.push_sent setting and keeps the entries inside the 14 days (loadPushGradingState), and every
// delivered push writes the whole list back (addSentLocked, saveIntercepts: PutSetting). It knows nothing of the
// table. Its pusher has no store, so the list is in memory and the daily counts are the real reader's.
type oldBuild struct {
	p     *Pusher
	store sentRecordStore
}

// failingAdopt is a store whose start could not read the old setting into the table.
type failingAdopt struct{ *gradingStore }
