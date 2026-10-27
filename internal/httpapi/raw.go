package httpapi

import (
	"net/http"
	"path"
	"strings"

	"github.com/danielfrg/notebook/internal/vault"
	"github.com/danielgtaylor/huma/v2/queryparam"
)

// imageTypes are the content types that raw serves, by extension in lower case:
// the images of vault.IsAsset. The type comes from the extension and never from
// the file, so a file named .png that holds a page is sent as an image that the
// browser cannot show, and not as a page.
var imageTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".avif": "image/avif",
}

// raw serves an image of a vault, for the notes that show it. It is a plain
// handler because it sends bytes and not JSON. Only images are served, which
// rules out the notes and anything that a browser would run, such as .svg, and
// the headers keep even a file that is not an image from running.
func raw(reg *vault.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		v, err := find(reg, r.PathValue("vault"))
		if err != nil {
			writeProblem(w, problem(err))
			return
		}
		// The path is read as it is for every other endpoint.
		p := queryparam.Get(r.URL.RawQuery, "path")
		f, info, err := v.OpenAsset(p)
		if err != nil {
			writeProblem(w, problem(err))
			return
		}
		defer f.Close()
		h := w.Header()
		h.Set("Content-Type", imageTypes[strings.ToLower(path.Ext(p))])
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Content-Security-Policy", "sandbox")
		h.Set("Cache-Control", "no-cache") // an image is stored, and asked about each time
		http.ServeContent(w, r, "", info.ModTime(), f)
	}
}
