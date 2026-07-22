package preview

import (
	"bytes"
	"embed"
	"html/template"
	"strings"
)

// Render returns a self-contained HTML document for report.
func Render(report Report) ([]byte, error) {
	var output bytes.Buffer
	if err := pageTemplate.Execute(&output, report); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

type diffLine struct {
	Class string
	Text  string
}

func diffLines(diff string) []diffLine {
	if diff == "" {
		return nil
	}

	lines := strings.Split(strings.TrimSuffix(diff, "\n"), "\n")
	result := make([]diffLine, 0, len(lines))
	for _, line := range lines {
		result = append(result, diffLine{Class: diffLineClass(line), Text: line})
	}
	return result
}

func diffLineClass(line string) string {
	switch {
	case strings.HasPrefix(line, "diff --git "):
		return "diff-file"
	case strings.HasPrefix(line, "@@"):
		return "diff-hunk"
	case strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "--- "):
		return "diff-path"
	case strings.HasPrefix(line, "+"):
		return "diff-addition"
	case strings.HasPrefix(line, "-"):
		return "diff-deletion"
	case strings.HasPrefix(line, "index "), strings.HasPrefix(line, "new file mode "), strings.HasPrefix(line, "deleted file mode "):
		return "diff-meta"
	default:
		return "diff-context"
	}
}

//go:embed templates/*
var templateFiles embed.FS

var pageTemplateSource = embeddedTemplate("templates/preview.html.tmpl")

var stylesheet = embeddedTemplate("templates/preview.css")

var pageTemplate = template.Must(template.New("preview").Funcs(template.FuncMap{
	"diffLines": diffLines,
}).Parse(`{{define "stylesheet"}}` + stylesheet + `{{end}}` + pageTemplateSource))

func embeddedTemplate(path string) string {
	contents, err := templateFiles.ReadFile(path)
	if err != nil {
		panic(err)
	}

	return string(contents)
}
