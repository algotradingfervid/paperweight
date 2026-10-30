package gallery

import "testing"

// The mock's stand-in confirms (removeDialogs) go once Attachments renders
// its own (UX design 2026-10-31 §6 change 2(b)): in (b) and (f) each Remove
// opens its file's confirm, and each confirm is on the page once.
func TestGallery_AttachmentsRemoveOpensConfirm(t *testing.T) {
	d := authPage(t, "attachments")
	for _, c := range []struct{ section, upload string }{{"att-b", "att-draft"}, {"att-f", "att-many"}} {
		sec := d.Find("section#" + c.section)
		files := threeFiles(true)
		if c.section == "att-f" {
			files = twentyFiles()
		}
		for _, a := range files {
			id := removeDialogID(a)
			rm := sec.Find(`button[aria-label="Remove ` + a.Name + `"]`)
			if rm.Length() != 1 {
				t.Errorf("%s: %d Remove buttons for %q, want 1", c.section, rm.Length(), a.Name)
				continue
			}
			if got, _ := rm.Attr("type"); got != "button" {
				t.Errorf("%s: Remove %q type = %q, want button", c.section, a.Name, got)
			}
			if got, _ := rm.Attr("data-open-dialog"); got != id {
				t.Errorf("%s: Remove %q data-open-dialog = %q, want %q", c.section, a.Name, got, id)
			}
			if n := d.Find(`dialog[id="` + id + `"]`).Length(); n != 1 {
				t.Errorf("%s: dialog#%s appears %d times on the page, want once", c.section, id, n)
			}
			if sec.Find("#"+c.upload+` dialog[id="`+id+`"]`).Length() != 1 {
				t.Errorf("%s: dialog#%s is not inside #%s", c.section, id, c.upload)
			}
		}
	}
	if n := d.Find(`dialog[id^="remove-file-"]`).Length(); n != 3+20 {
		t.Errorf("found %d remove confirms, want 23 (three in (b), twenty in (f))", n)
	}
}
