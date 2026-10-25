package web

import (
	"context"
	"encoding/gob"

	"github.com/alexedwards/scs/v2"
)

const flashKey = "flash"

type sessionsKey struct{}

// Notice is one flash message (CD-80).
type Notice struct {
	Level string `json:"level"`
	Text  string `json:"text"`
}

func init() {
	gob.Register([]Notice{})
}

func sessionsFrom(ctx context.Context) *scs.SessionManager {
	sm, _ := ctx.Value(sessionsKey{}).(*scs.SessionManager)
	return sm
}

// Flash appends a message to the session, for the next full page to show. It
// only logs a warning when the router has no session manager.
func Flash(ctx context.Context, level, text string) {
	sm := sessionsFrom(ctx)
	if sm == nil {
		loggerFrom(ctx).WarnContext(ctx, "flash dropped: the router has no session manager; set web.Options.Sessions", "level", level, "text", text)
		return
	}
	notices, _ := sm.Get(ctx, flashKey).([]Notice)
	sm.Put(ctx, flashKey, append(notices, Notice{Level: level, Text: text}))
}

// TakeFlash returns the session's messages in the order they were added and
// clears them.
func TakeFlash(ctx context.Context) []Notice {
	sm := sessionsFrom(ctx)
	if sm == nil {
		return nil
	}
	notices, _ := sm.Pop(ctx, flashKey).([]Notice)
	return notices
}
