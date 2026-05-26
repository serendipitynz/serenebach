package sitemap

import (
	"fmt"

	"github.com/serendipitynz/serenebach/internal/domain"
)

// RobotsTxt returns the plain-text body for /robots.txt. The Sitemap
// directive is injected dynamically from the weblog's base_url.
func RobotsTxt(weblog *domain.Weblog) string {
	if weblog == nil || weblog.BaseURL == "" {
		return ""
	}
	base := weblog.BaseURL
	if base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return fmt.Sprintf("User-agent: *\nAllow: /\n\nSitemap: %s/sitemap.xml\n", base)
}
