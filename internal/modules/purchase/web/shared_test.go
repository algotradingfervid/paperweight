package purchaseweb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-PO P15-0 (CD-280 D6): the trailing count of unreadable rows, singular
// for one, and nothing for none.
func TestOtherCount(t *testing.T) {
	for _, c := range []struct {
		k          int
		noun, want string
	}{
		{0, "order", ""},
		{1, "order", "1 other order"},
		{2, "order", "2 other orders"},
		{1, "goods receipt", "1 other goods receipt"},
		{3, "goods receipt", "3 other goods receipts"},
	} {
		if got := otherCount(c.k, c.noun); got != c.want {
			t.Errorf("otherCount(%d, %q) = %q, want %q", c.k, c.noun, got, c.want)
		}
	}
}

// MOD-PO P15-0 (CD-258 Q12): a list in words, the last two joined by "and".
func TestJoinAnd(t *testing.T) {
	for _, c := range []struct {
		items []string
		want  string
	}{
		{nil, ""},
		{[]string{"15 Bag"}, "15 Bag"},
		{[]string{"15 Bag", "120 kg"}, "15 Bag and 120 kg"},
		{[]string{"15 Bag", "120 kg", "4 Nos"}, "15 Bag, 120 kg and 4 Nos"},
	} {
		if got := joinAnd(c.items); got != c.want {
			t.Errorf("joinAnd(%q) = %q, want %q", c.items, got, c.want)
		}
	}
}

// p150Ctx is a docscreen.Ctx for p on r, with the real services' Deps.
func p150Ctx(env *purchasetest.Env, p auth.Principal, r *http.Request, view *docscreen.DocView) docscreen.Ctx {
	return docscreen.Ctx{Req: r, Principal: p, View: view, Deps: docscreen.Deps{
		DB: env.DB, Engine: env.Engine, Auth: env.Auth,
	}}
}

// p150Text renders c and returns its text, spaces collapsed.
func p150Text(t *testing.T, c templ.Component) string {
	t.Helper()
	var b strings.Builder
	if err := c.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	q, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(strings.Fields(q.Text()), " ")
}

// MOD-PO P15-0 (UX §5.8 item 8, PUX-006): the Owner card and its Hand over
// link show only to the owner; an Administrator, who may write, never reads
// "You own this order".
func TestOwnerCard_OwnerOnly(t *testing.T) {
	env := purchasetest.New(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	view := &docscreen.DocView{Info: doctype.Info{CreatedBy: env.Priya.ID}}
	card := ownerCard(p150Ctx(env, env.Priya, req, view), 57)
	if card == nil {
		t.Fatal("owner: ownerCard = nil, want the card")
	}
	var b strings.Builder
	if err := card.Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	q, _ := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if got := q.Find("h2").Text(); got != ownerCardTitle {
		t.Errorf("owner card title = %q, want %q", got, ownerCardTitle)
	}
	if got := q.Find("p").Text(); got != "You own this order. Only you can change or submit it." {
		t.Errorf("owner card text = %q", got)
	}
	if a := q.Find("a"); a.Text() != handOverLabel || a.AttrOr("href", "") != "/modules/purchase/orders/57/handover" {
		t.Errorf("owner card link = %q to %q, want %q to the hand-over page", a.Text(), a.AttrOr("href", ""), handOverLabel)
	}
	if c := ownerCard(p150Ctx(env, env.Admin, req, view), 57); c != nil {
		t.Errorf("Administrator: ownerCard = %q, want nil", p150Text(t, c))
	}
	if c := ownerCard(p150Ctx(env, env.Priya, req, nil), 57); c != nil {
		t.Errorf("no View: ownerCard = %q, want nil", p150Text(t, c))
	}
}

// MOD-PO P15-0: ownerName is the user's name.
func TestOwnerName(t *testing.T) {
	env := purchasetest.New(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	got, err := ownerName(p150Ctx(env, env.Ravi, req, nil), env.Priya.ID)
	if err != nil || got != env.Priya.Name {
		t.Errorf("ownerName(Priya) = %q, %v; want %q", got, err, env.Priya.Name)
	}
}

// MOD-PO P15-0 (CD-248 family): readableLabel names a record only to a
// viewer who may read it. Priya is restricted to her project and site, so
// the masters' LinkRead (no document dimensions) would refuse her own PO.
func TestReadableLabel(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	label, ok, err := readableLabel(p150Ctx(env, env.Priya, req, nil), purchase.DocType, po.ID)
	if want := "Draft " + strconv.FormatInt(po.ID, 10); err != nil || !ok || label != want {
		t.Errorf("Priya: readableLabel = %q, %v, %v; want %q, true", label, ok, err, want)
	}
	label, ok, err = readableLabel(p150Ctx(env, env.Deepak, req, nil), purchase.DocType, po.ID)
	if err != nil || ok || label != "" {
		t.Errorf("Deepak: readableLabel = %q, %v, %v; want \"\", false, nil", label, ok, err)
	}
	// A missing document is ok false with no error (the auth.ErrNotFound branch).
	label, ok, err = readableLabel(p150Ctx(env, env.Priya, req, nil), purchase.DocType, 999999)
	if err != nil || ok || label != "" {
		t.Errorf("missing PO: readableLabel = %q, %v, %v; want \"\", false, nil", label, ok, err)
	}
}

// MOD-PO P15-0 (UX 5.10): commentsCard lists the record's comments with the
// box, and puts a refused no-script post's text and message back in it.
func TestCommentsCard(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	if _, err := env.Comments.AddComment(context.Background(), env.Ravi, purchase.DocType, po.ID, "Is this still needed?"); err != nil {
		t.Fatal(err)
	}
	sm := scs.New()
	m := &module{d: Deps{Comments: env.Comments, Sessions: sm, CSRF: func(*http.Request) string { return "p150-token" }}}
	var html, refused string
	var cardErr error
	h := sm.LoadAndSave(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// modkit/web's session keys for a refused post (TakeCommentDraft).
		k := "comment-draft:" + purchase.DocType + "/" + strconv.FormatInt(po.ID, 10)
		sm.Put(r.Context(), k+":typed", "p150 typed")
		sm.Put(r.Context(), k+":msg", "p150 message")
		c, err := m.commentsCard(p150Ctx(env, env.Priya, r, nil), purchase.DocType, po.ID)
		if err != nil {
			cardErr = err
			return
		}
		var b strings.Builder
		cardErr = c.Render(r.Context(), &b)
		html = b.String()
		_, cardErr2 := m.commentsCard(p150Ctx(env, env.Deepak, r, nil), purchase.DocType, po.ID)
		if cardErr2 == nil {
			refused = "no error"
		}
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	for _, want := range []string{"Comments", "Is this still needed?", "p150 typed", "p150 message", "p150-token"} {
		if !strings.Contains(html, want) {
			t.Errorf("comments card lacks %q:\n%s", want, html)
		}
	}
	if refused != "" {
		t.Error("Deepak, who can't read the PO: commentsCard gave no error")
	}
}
