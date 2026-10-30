package docscreen

import (
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// attachHint states FILE's limits before the upload controls (D-FILE-02, -03).
const attachHint = "PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV, up to 10 MB. One file at a time."

// attachView is the Attachments card of a DocType with Attachable set (UX
// design 2026-10-31 §5.1): Upload is set for a user with write, else the
// files are shown read-only.
type attachView struct {
	Upload *components.UploadProps
	Files  []components.Attachment
}

// attachOf is v's Attachments card, or nil when dt doesn't set Attachable or
// v is cancelled (CD-243). Remove is offered where FILE allows it: write, and
// a state files.RemoveAllowed accepts (D-FILE-06).
func attachOf(dt *doctype.DocType, v DocView, rights map[auth.Right]bool) *attachView {
	if !dt.Attachable || v.Info.State == doctype.StateCancelled {
		return nil
	}
	if !rights["write"] {
		return &attachView{Files: v.Attachments}
	}
	list := v.Attachments
	if files.RemoveAllowed(v.Info.State) == nil {
		list = make([]components.Attachment, len(v.Attachments))
		for i, a := range v.Attachments {
			a.RemoveURL = "/files/" + strconv.FormatInt(a.ID, 10) + "/remove"
			list[i] = a
		}
	}
	return &attachView{Files: v.Attachments, Upload: &components.UploadProps{
		ID: "attachments", Label: "Add a file", Action: "/attachments/" + dt.Name + "/" + strconv.FormatInt(v.Info.ID, 10),
		Hint: attachHint, Accept: "image/*,application/pdf,.xlsx,.docx,.csv", Files: list,
	}}
}
