package files

// MaxFileBytes is the largest upload the default policy takes (10 MB).
const MaxFileBytes int64 = 10_485_760

// MaxAttachments is how many files one document may carry.
const MaxAttachments = 20

// SniffBytes is how much of a file's start Check looks at.
const SniffBytes = 64 << 10

// Policy is what a DocType allows to be attached to it.
type Policy struct {
	MaxBytes                     int64
	MaxCount                     int
	Exts                         []string // lower-case, with the dot
	ReadAnySignedIn, IgnoreState bool
	TypeMessage, SizeMessage     string // replace the default messages when set
}

// DefaultPolicy is the allowlist for document attachments.
var DefaultPolicy = Policy{
	MaxBytes: MaxFileBytes,
	MaxCount: MaxAttachments,
	Exts:     []string{".pdf", ".jpg", ".jpeg", ".png", ".webp", ".heic", ".heif", ".xlsx", ".docx", ".csv"},
}
