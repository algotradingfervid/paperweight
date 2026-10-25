package print

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Render writes the full print page for info: the shell decides the
// watermark, badge, number and title from info alone, then wraps the
// DocType's view.
func (r *Registry) Render(ctx context.Context, tx *db.Tx, w io.Writer, info doctype.Info) error {
	view, ok := r.Lookup(info.DocType)
	if !ok {
		return fmt.Errorf("%w for DocType %s", ErrNoTemplate, info.DocType)
	}
	h := Header{
		Status: StatusFor(string(info.State)), Number: info.Display, Title: SafeTitle(info.Display),
		PageCSS: "/print/" + url.PathEscape(info.DocType) + "/" + strconv.FormatInt(info.ID, 10) + "/page.css",
	}
	ctx = withHeader(ctx, h)
	body, err := view(ctx, tx, info.ID)
	if err != nil {
		return err
	}
	return Page(h, body).Render(ctx, w)
}
