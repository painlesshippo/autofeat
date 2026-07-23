package review

import (
	"bytes"
	"embed"
	"html/template"
	"strconv"
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

type diffFile struct {
	Name     string
	Metadata []diffLine
	Lines    []diffLine
}

func diffFiles(diff string) []diffFile {
	if diff == "" {
		return nil
	}

	lines := strings.Split(strings.TrimSuffix(diff, "\n"), "\n")
	result := make([]diffFile, 0)
	for _, line := range lines {
		if strings.HasPrefix(line, "diff --git ") {
			result = append(result, diffFile{Name: diffHeaderPath(line)})
		}
		if len(result) == 0 {
			result = append(result, diffFile{Name: "Changes"})
		}

		file := &result[len(result)-1]
		if strings.HasPrefix(line, "+++ ") {
			if path := diffPath(strings.TrimPrefix(line, "+++ ")); path != "" {
				file.Name = path
			}
		} else if file.Name == "Changed file" && strings.HasPrefix(line, "--- ") {
			if path := diffPath(strings.TrimPrefix(line, "--- ")); path != "" {
				file.Name = path
			}
		}
		diffLine := diffLine{Class: diffLineClass(line), Text: line}
		if len(file.Lines) == 0 && diffLine.Class != "diff-hunk" {
			file.Metadata = append(file.Metadata, diffLine)
			continue
		}
		file.Lines = append(file.Lines, diffLine)
	}
	return result
}

func diffHeaderPath(line string) string {
	header := strings.TrimPrefix(line, "diff --git ")
	if index := strings.LastIndex(header, `"b/`); index >= 0 {
		if path := diffPath(header[index:]); path != "" {
			return path
		}
	}
	if index := strings.LastIndex(header, " b/"); index >= 0 {
		return diffPath(header[index+1:])
	}
	return "Changed file"
}

func diffPath(path string) string {
	if path == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(path, `"`) {
		if unquoted, err := strconv.Unquote(path); err == nil {
			path = unquoted
		}
	}
	return strings.TrimPrefix(strings.TrimPrefix(path, "a/"), "b/")
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

var pageTemplateSource = embeddedTemplate("templates/review.html.tmpl")

var stylesheet = embeddedTemplate("templates/review.css")

var pageTemplate = template.Must(template.New("review").Funcs(template.FuncMap{
	"diffFiles": diffFiles,
}).Parse(`{{define "stylesheet"}}` + stylesheet + `{{end}}` + pageTemplateSource))

func embeddedTemplate(path string) string {
	contents, err := templateFiles.ReadFile(path)
	if err != nil {
		panic(err)
	}

	return string(contents)
}
