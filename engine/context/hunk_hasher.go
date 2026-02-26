package context

import (
	"crypto/sha256"
	"fmt"

	"github.com/crvgilbertson/intentra/engine/models"
)

// HashHunk produces a stable, deterministic ID for a hunk:
// sha256(filePath + header + patch).
func HashHunk(h models.Hunk) string {
	data := h.FilePath + h.Header + h.Patch
	sum := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", sum)
}
