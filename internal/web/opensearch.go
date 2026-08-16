package web

import (
	"encoding/xml"
	"log"
	"net/http"
)

// The descriptor is what lets both browsers discover the engine on their own.
// Neither will make it the default — that stays a manual step — but without it
// there is nothing to add in the first place.
type openSearchDoc struct {
	XMLName       xml.Name      `xml:"OpenSearchDescription"`
	Xmlns         string        `xml:"xmlns,attr"`
	ShortName     string        `xml:"ShortName"`
	Description   string        `xml:"Description"`
	InputEncoding string        `xml:"InputEncoding"`
	URL           openSearchURL `xml:"Url"`
}

type openSearchURL struct {
	Type     string `xml:"type,attr"`
	Method   string `xml:"method,attr"`
	Template string `xml:"template,attr"`
}

func (s *server) handleOpenSearch(w http.ResponseWriter, r *http.Request) {
	// Marshalling rather than a text template: XML attribute escaping (notably
	// & in the search template) has to be exact or Firefox refuses the plugin.
	doc := openSearchDoc{
		Xmlns:         "http://a9.com/-/spec/opensearch/1.1/",
		ShortName:     shortName,
		Description:   "Personal address-bar shortcuts, resolved locally",
		InputEncoding: "UTF-8",
		URL: openSearchURL{
			Type:     "text/html",
			Method:   "get",
			Template: baseURL(r) + "/?q={searchTerms}",
		},
	}
	out, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/opensearchdescription+xml")
	if _, err := w.Write(append([]byte(xml.Header), out...)); err != nil {
		log.Printf("opensearch: %v", err)
	}
}
