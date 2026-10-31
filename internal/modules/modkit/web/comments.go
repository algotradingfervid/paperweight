package modkitweb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	platformweb "github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

const (
	// bodyField is the comment box's form field.
	bodyField = "body"
	// regionID wraps the entries and the box: the htmx post redraws it.
	regionID = "comments"
	// statusID is the polite announcement after a post.
	statusID = "comments-posted"

	msgPosted = "Comment posted."
	boxLabel  = "Add a comment"
	boxHint   = "Everyone who can open this %s sees it. Comments can't be changed later."
)

// region is what commentsRegion draws.
type region struct {
	dt       string
	id       int64
	comments []modkit.Comment
	csrf     string
	typed    string // the box's text: kept on a refusal, empty otherwise
	msg      string // the refusal's message
	focus    bool   // an htmx redraw puts focus in the box
	posted   bool   // the redraw follows a saved comment
}

// box is the comment box's field.
func (v region) box() components.FieldProps {
	noun := "document"
	if dt, ok := doctype.Lookup(v.dt); ok {
		noun = strings.ToLower(dt.Label)
	}
	p := components.FieldProps{
		ID: components.FieldID(bodyField), Name: bodyField, Label: boxLabel,
		Hint: fmt.Sprintf(boxHint, noun), Value: v.typed, Error: v.msg,
	}
	if v.focus {
		p.Attrs = templ.Attributes{"autofocus": true} // htmx focuses it after the swap
	}
	return p
}

// announceAttrs are the polite announcement's attributes; oob swaps it in
// from an htmx answer. They live here, out of the scanned template.
func announceAttrs(id string, oob bool) templ.Attributes {
	a := templ.Attributes{"id": id, "role": "status"}
	if oob {
		a["hx-swap-oob"] = "true"
	}
	return a
}

func commentPath(dt string, id int64) string {
	return "/modules/comments/" + dt + "/" + strconv.FormatInt(id, 10)
}

// Session keys for a refused post without htmx, per document.
func draftKeys(dt string, id int64) (typed, msg string) {
	k := "comment-draft:" + dt + "/" + strconv.FormatInt(id, 10)
	return k + ":typed", k + ":msg"
}

// TakeCommentDraft returns and clears the typed text and message of a comment
// post refused without htmx (UX 5.10), for the document page to pass to
// CommentsPanelDraft. ctx must be inside sm.LoadAndSave.
func TakeCommentDraft(ctx context.Context, sm *scs.SessionManager, dt string, id int64) (typed, msg string) {
	tk, mk := draftKeys(dt, id)
	return sm.PopString(ctx, tk), sm.PopString(ctx, mk)
}

// addComment is POST /modules/comments/{doctype}/{id} (PO spec 22, UX 5.10).
// With htmx it redraws the entries and the box (cleared, or on a refused body
// keeping the typed text with its message, 422) with focus in the box.
// Without htmx it redirects to the document page's panel; a refused body is first
// put in the session for TakeCommentDraft. A bad id is the same 404 as a
// missing record (CD-245).
func (h *handlers) addComment(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	dt := chi.URLParam(r, "doctype")
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		platformweb.ErrorFrom(w, r, auth.ErrNotFound)
		return
	}
	c := modkit.NewComments(modkit.CommentsDeps{DB: h.d.DB, Auth: h.d.Auth, Info: h.d.Info, Clock: h.d.Clock})
	typed := r.PostFormValue(bodyField)
	_, addErr := c.AddComment(r.Context(), p, dt, id, typed)
	var se interface{ Status() int }
	if addErr != nil && (!errors.As(addErr, &se) || se.Status() != http.StatusUnprocessableEntity) {
		platformweb.ErrorFrom(w, r, addErr)
		return
	}
	docURL := "/doc/" + dt + "/" + strconv.FormatInt(id, 10)
	if !ui.IsHTMX(r) {
		if addErr != nil {
			// Into the session before the response starts: scs saves at the first write.
			tk, mk := draftKeys(dt, id)
			h.d.Sessions.Put(r.Context(), tk, typed)
			h.d.Sessions.Put(r.Context(), mk, addErr.Error())
		}
		http.Redirect(w, r, docURL+"#"+regionID, http.StatusSeeOther) //nolint:gosec // G710: a local /doc/ path; AddComment got past its checks, so dt is a DocType registered for comments, and id is numeric
		return
	}
	cs, err := c.ListComments(r.Context(), p, dt, id)
	if err != nil {
		platformweb.ErrorFrom(w, r, err)
		return
	}
	v := region{dt: dt, id: id, comments: cs, focus: true, posted: addErr == nil}
	status := http.StatusOK
	if addErr != nil {
		v.typed, v.msg, status = typed, addErr.Error(), http.StatusUnprocessableEntity
	}
	ui.Render(w, r, status, nil, commentsRedraw(v))
}
