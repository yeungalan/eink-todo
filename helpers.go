package main

import (
	"embed"
	"strings"
)

//go:embed data/*.json
var seedFS embed.FS

func readSeed(name string) ([]byte, error) {
	return seedFS.ReadFile("data/" + name)
}

const bom = "\ufeff"

// firstLine returns the first non-empty line with any UTF-8 BOM stripped.
func firstLine(b []byte) string {
	for _, l := range splitLines(b) {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

func splitLines(b []byte) []string {
	s := strings.TrimPrefix(string(b), bom)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}

// parseCSVLine parses one CSV row supporting double-quoted fields.
func parseCSVLine(line string) []string {
	line = strings.TrimPrefix(line, bom)
	var fields []string
	var cur strings.Builder
	inQuotes := false
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '"':
			if inQuotes && i+1 < len(line) && line[i+1] == '"' {
				cur.WriteByte('"')
				i++
			} else {
				inQuotes = !inQuotes
			}
		case ch == ',' && !inQuotes:
			fields = append(fields, cur.String())
			cur.Reset()
		default:
			cur.WriteByte(ch)
		}
	}
	fields = append(fields, cur.String())
	return fields
}
