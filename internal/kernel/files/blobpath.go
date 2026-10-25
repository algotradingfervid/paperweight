package files

import (
	"errors"
	"path/filepath"
	"regexp"
)

// ErrBadHash is returned by BlobPath for anything but a lower-case 64-hex SHA-256.
var ErrBadHash = errors.New("files: bad blob hash")

var blobHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BlobPath returns where the blob with this hash lives: <dataDir>/files/<2 hex>/<64 hex> (FILE-014).
func BlobPath(dataDir, hash string) (string, error) {
	if !blobHash.MatchString(hash) {
		return "", ErrBadHash
	}
	return filepath.Join(dataDir, "files", hash[:2], hash), nil
}
