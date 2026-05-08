package content

import (
	"fmt"
	"hash/fnv"

	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/template/sbtemplate"
)

// templateCache is a process-wide parse cache shared by all Render calls.
var templateCache = sbtemplate.NewCache()

// cachedParse returns a parsed template, consulting templateCache first.
// The cache key is (template.ID, updated_at.Unix(), body-field, fnv64(src))
// so editing a template body produces a new cache entry automatically, and
// tests that reuse ID 1 across fresh databases still get distinct entries.
func cachedParse(t *domain.Template, field, src string) (*sbtemplate.Template, error) {
	h := fnv.New64a()
	h.Write([]byte(src))
	key := fmt.Sprintf("%d:%d:%s:%x", t.ID, t.UpdatedAt.Unix(), field, h.Sum64())
	return templateCache.Get(key, src, sbtemplate.DefaultCallback)
}
