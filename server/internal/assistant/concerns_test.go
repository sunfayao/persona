package assistant

import ()

// Concerns (concerns.go, #3561): the owner's concern is a to-do of kind concern that closes only once the main
// conversation reported its outcome (a decision of source reported), never on its own, and extensions see it in
// todo.changed. conv1 is the main conversation; ids are placeholders.
