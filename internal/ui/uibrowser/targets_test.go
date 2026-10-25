//go:build e2e

package uibrowser

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// Pages for screens.TargetSizeJS and labelled choices (AD decision B): a
// labelled radio or checkbox's target is its full-row label.
const (
	targetHead     = `<!doctype html><html><head><meta name="viewport" content="width=device-width"><style>body{margin:0}input{width:24px;height:24px;margin:0}label{display:flex;align-items:center;block-size:48px}label.thin{block-size:20px}</style></head><body>`
	labelledRadio  = targetHead + `<label><input type="radio" id="r" name="r"> Own</label></body></html>`
	labelledCheck  = targetHead + `<label for="c">Team</label><input type="checkbox" id="c">` + `</body></html>`
	bareRadio      = targetHead + `<input type="radio" id="bare" name="r" aria-label="Own"></body></html>`
	thinLabelRadio = targetHead + `<label class="thin"><input type="radio" id="thin" name="r"> Own</label></body></html>`
)

func TestTargetSize_LabelledChoiceUsesLabel(t *testing.T) {
	pages := map[string]string{"/labelled": labelledRadio, "/for": labelledCheck, "/bare": bareRadio, "/thin": thinLabelRadio}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(pages[r.URL.Path]))
	}))
	defer srv.Close()
	ctx := New(t)

	for _, c := range []struct {
		path string
		want []string
	}{
		{"/labelled", nil},                      // 24 px radio in a 48 px full-row label
		{"/for", nil},                           // label[for] around nothing, 48 px full row
		{"/bare", []string{"input#bare 24x24"}}, // no label: the radio itself
	} {
		if err := At(ctx, 360, srv.URL+c.path); err != nil {
			t.Fatal(err)
		}
		got, err := SmallTargets(ctx)
		if err != nil {
			t.Fatalf("SmallTargets(%s): %v", c.path, err)
		}
		if len(got) != len(c.want) || (len(got) == 1 && got[0] != c.want[0]) {
			t.Errorf("SmallTargets(%s) = %q, want %q", c.path, got, c.want)
		}
	}

	// A label shorter than 44 px is still a small target.
	if err := At(ctx, 360, srv.URL+"/thin"); err != nil {
		t.Fatal(err)
	}
	if got, err := SmallTargets(ctx); err != nil || len(got) != 1 {
		t.Errorf("SmallTargets(/thin) = %q, %v; want one entry for the radio in a 20 px label", got, err)
	}
}
