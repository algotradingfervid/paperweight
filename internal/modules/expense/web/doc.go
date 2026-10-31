// Package expenseweb holds the Expense module's screens, docscreen hooks and
// module routes. It registers plain handlers; internal/app puts AUTH's guard
// from Guard on each route (CD-46). routes.go doesn't change after E1: each
// route calls a handler declared in its owner's stubs file, which that task
// replaces; until then the route answers 501, never success.
package expenseweb
