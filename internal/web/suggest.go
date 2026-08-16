package web

import (
	"cmp"
	"encoding/json"
	"log"
	"net/http"
)

const (
	// suggestionType is the MIME type both browsers match the descriptor's
	// suggestions Url on, and the one this endpoint answers with.
	suggestionType = "application/x-suggestions+json"

	// suggestLimit caps one response. Both browsers trim the list to fit their
	// own dropdown; the cap is what keeps a large rule set off the wire on every
	// keystroke.
	suggestLimit = 10
)

// handleSuggest implements the OpenSearch suggestions extension: the browser
// calls it on every keystroke and lists what comes back under the address bar.
//
// The response is a positional array — query, suggestions, descriptions, query
// URLs, metadata — and the browsers read different slices of it, so what goes
// into it depends on which one is asking. See docs/browsers.md.
func (s *server) handleSuggest(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	found := s.live.Load().Suggest(q, suggestLimit)

	texts := make([]string, len(found))
	descs := make([]string, len(found))
	for i, sg := range found {
		texts[i] = sg.Query
		descs[i] = cmp.Or(sg.Desc, sg.Query)
	}

	// Element 0 has to be what was typed, byte for byte: Chrome drops the whole
	// response when it differs, so q is echoed rather than the trimmed form the
	// rules were matched against. Element 3 is the spec's query URL list, which
	// neither browser reads; it is present so the metadata object lands at
	// element 4, where Chrome looks for it.
	out := []any{q, texts, descs, []string{}}

	// Chrome renders an entry as a place to go, rather than a query to run,
	// when its type is NAVIGATION — and then the entry itself has to be the URL
	// and the description supplies the title. Firefox has no equivalent: it
	// would send that URL back as a query, which no rule matches, so Firefox is
	// only ever offered the shortcut. Anything unrecognised gets the safe form.
	if detectBrowser(r.UserAgent()) == "chrome" {
		types := make([]string, len(found))
		for i, sg := range found {
			types[i] = "QUERY"
			if sg.Target != "" {
				texts[i], types[i] = sg.Target, "NAVIGATION"
			}
		}
		out = append(out, map[string]any{"google:suggesttype": types})
	}

	w.Header().Set("Content-Type", suggestionType)
	w.Header().Set("Cache-Control", noStore)
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("suggest: %v", err)
	}
}
