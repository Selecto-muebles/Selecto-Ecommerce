package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

type legalEditorSection struct {
	Heading  string `json:"heading"`
	Body     string `json:"body"`
	Editable bool   `json:"editable"`
}

type legalEditorResponse struct {
	BaseVersion string               `json:"base_version"`
	BaseSHA256  string               `json:"base_sha256"`
	Revision    int64                `json:"revision"`
	Dirty       bool                 `json:"dirty"`
	Sections    []legalEditorSection `json:"sections"`
}

var legalHeadingPattern = regexp.MustCompile(`(?m)^#{1,6} [^\r\n]+`)
var legalVersionPattern = regexp.MustCompile(`(?m)^\*\*Versión:\*\* [^\r\n]+`)

func splitLegalSections(body string) []legalEditorSection {
	indexes := legalHeadingPattern.FindAllStringIndex(body, -1)
	if len(indexes) == 0 {
		return []legalEditorSection{{Body: body}}
	}
	sections := make([]legalEditorSection, 0, len(indexes)+1)
	if indexes[0][0] > 0 {
		sections = append(sections, legalEditorSection{Body: body[:indexes[0][0]]})
	}
	for index, span := range indexes {
		end := len(body)
		if index+1 < len(indexes) {
			end = indexes[index+1][0]
		}
		heading := body[span[0]:span[1]]
		sections = append(sections, legalEditorSection{
			Heading: heading, Body: body[span[1]:end],
			Editable: strings.HasPrefix(heading, "### Artículo "),
		})
	}
	return sections
}

func composeLegalSections(sections []legalEditorSection) string {
	var body strings.Builder
	for _, section := range sections {
		body.WriteString(section.Heading)
		body.WriteString(section.Body)
	}
	return body.String()
}

func legalDigest(body string) string {
	hash := sha256.Sum256([]byte(body))
	return hex.EncodeToString(hash[:])
}
