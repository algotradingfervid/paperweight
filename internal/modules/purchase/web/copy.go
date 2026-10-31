package purchaseweb

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// msgCopied is the Copy flash, "Draft {new} is a copy of {display}. Check it
// before you submit." (UX "After Copy or Create PO", CD-258).
const msgCopied = "Draft %d is a copy of %s. Check it before you submit."

// copyPost copies the PO (PO spec §24.1) and redirects (303) to the new
// draft's Details step with the copy flash. AUTH-099's restriction refusal
// and create's field refusals (a disabled site or project, their messages
// joined with a space) go back to the PO page as a danger flash (UX "Copy
// refused", AD-13 rulings); nothing is created. Anything else (AUTH's 403
// and 404 among them) is the platform's error page.
func (h *handlers) copyPost(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) // not a number: 0, which no PO has (404)
	p, _ := auth.PrincipalFrom(r.Context())
	newID, source, err := h.d.Service.CopyWithSource(r.Context(), p, id)
	var ve *auth.ValueError
	var he doctype.HTTPError
	switch {
	case err == nil:
		web.Flash(r.Context(), "success", fmt.Sprintf(msgCopied, newID, source))
		http.Redirect(w, r, "/doc/"+purchase.DocType+"/"+strconv.FormatInt(newID, 10)+"/edit/details", http.StatusSeeOther)
	case errors.As(err, &ve):
		web.Flash(r.Context(), "error", err.Error())
		http.Redirect(w, r, "/doc/"+purchase.DocType+"/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	case errors.As(err, &he) && he.Status() == http.StatusUnprocessableEntity && len(he.FieldErrors()) > 0:
		msgs := make([]string, len(he.FieldErrors()))
		for i, fe := range he.FieldErrors() {
			msgs[i] = fe.Message
		}
		web.Flash(r.Context(), "error", strings.Join(msgs, " "))
		http.Redirect(w, r, "/doc/"+purchase.DocType+"/"+strconv.FormatInt(id, 10), http.StatusSeeOther)
	default:
		web.ErrorFrom(w, r, err)
	}
}
