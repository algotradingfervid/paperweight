package gallery

import (
	"strconv"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The attachments page is the mock of the document Attachments card (UX
// design 2026-10-31 §5, §6 "Gallery mock"), built from today's components
// with sample files. Its entry is in pages (routes.go).

const (
	attachAction = "/_gallery/attachments"
	attachHint   = "PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV, up to 10 MB. One file at a time."
	attachAccept = "image/*,application/pdf,.xlsx,.docx,.csv"
	attachEmpty  = "No files attached yet."
)

// longFileName is a 120-character file name made of words.
const longFileName = uitest.LongName100 + " invoice copy 01.pdf"

// attachUpload is the card's FileUpload with id id.
func attachUpload(id, errText string, files []components.Attachment) components.UploadProps {
	return components.UploadProps{ID: id, Label: "Add a file", Action: attachAction, Hint: attachHint, Accept: attachAccept, Error: errText, Files: files}
}

// threeFiles are receipt.jpg, challan.heic and quote-shree-steel.pdf, newest
// first; removable sets RemoveURL on each.
func threeFiles(removable bool) []components.Attachment {
	out := []components.Attachment{
		{ID: 1, Kind: "image", Name: "receipt.jpg", Size: "2.3 MB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 10:30", URL: brand.AppLogoURL},
		{ID: 2, Kind: "heic", Name: "challan.heic", Size: "3.1 MB", UploadedBy: "Suresh Patil", UploadedAt: "24-10-2026 10:20", URL: attachAction},
		{ID: 3, Kind: "pdf", Name: "quote-shree-steel.pdf", Size: "512 KB", UploadedBy: "Priya Shah", UploadedAt: "24-10-2026 10:05", URL: attachAction},
	}
	return withRemove(out, removable)
}

// twentyFiles are D-FILE-02's limit of files on a draft, with the longest
// name and a Devanagari name first.
func twentyFiles() []components.Attachment {
	out := []components.Attachment{
		{ID: 101, Kind: "pdf", Name: longFileName, Size: "9.9 MB", UploadedBy: uitest.LongName100, UploadedAt: "24-10-2026 10:30", URL: attachAction},
		{ID: 102, Kind: "image", Name: "बिल रसीद.jpg", Size: "2.3 MB", UploadedBy: uitest.LongDevanagari, UploadedAt: "24-10-2026 10:29", URL: brand.AppLogoURL},
	}
	for i := 3; i <= 20; i++ {
		n := strconv.Itoa(i)
		a := components.Attachment{ID: int64(100 + i), Kind: "image", Name: "receipt-" + n + ".jpg", Size: "1.2 MB", UploadedBy: "Deepak Nair", UploadedAt: "24-10-2026 09:" + strconv.Itoa(53-i), URL: brand.AppLogoURL}
		if i%3 == 0 {
			a.Kind, a.Name, a.URL = "pdf", "bill-"+n+".pdf", attachAction
		}
		out = append(out, a)
	}
	return withRemove(out, true)
}

// withRemove sets each file's RemoveURL when removable.
func withRemove(files []components.Attachment, removable bool) []components.Attachment {
	if removable {
		for i := range files {
			files[i].RemoveURL = attachAction
		}
	}
	return files
}

// removeDialogID is file a's remove confirm.
func removeDialogID(a components.Attachment) string {
	return "remove-file-" + strconv.FormatInt(a.ID, 10)
}
