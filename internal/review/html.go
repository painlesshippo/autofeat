package review

import (
	"bytes"
	"embed"
	"html/template"
	"strconv"
	"strings"
	"time"
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
	OldName  string
	Status   string
	Metadata string
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
			result = append(result, diffFile{Name: diffHeaderPath(line), Status: "Modified"})
		}
		if len(result) == 0 {
			result = append(result, diffFile{Name: "Changes", Status: "Modified"})
		}

		file := &result[len(result)-1]
		switch {
		case strings.HasPrefix(line, "new file mode "):
			file.Status = "Added"
		case strings.HasPrefix(line, "deleted file mode "):
			file.Status = "Deleted"
		case strings.HasPrefix(line, "rename from "):
			file.Status = "Renamed"
			file.OldName = diffMetadataPath(strings.TrimPrefix(line, "rename from "))
		case strings.HasPrefix(line, "rename to "):
			file.Name = diffMetadataPath(strings.TrimPrefix(line, "rename to "))
		case strings.HasPrefix(line, "copy from "):
			file.Status = "Copied"
			file.OldName = diffMetadataPath(strings.TrimPrefix(line, "copy from "))
		case strings.HasPrefix(line, "copy to "):
			file.Name = diffMetadataPath(strings.TrimPrefix(line, "copy to "))
		case strings.HasPrefix(line, "+++ "):
			if path := diffPath(strings.TrimPrefix(line, "+++ ")); path != "" {
				file.Name = path
			}
		case file.Name == "Changed file" && strings.HasPrefix(line, "--- "):
			if path := diffPath(strings.TrimPrefix(line, "--- ")); path != "" {
				file.Name = path
			}
		}
		diffLine := diffLine{Class: diffLineClass(line), Text: line}
		if len(file.Lines) == 0 && diffLine.Class != "diff-hunk" {
			if file.Metadata != "" {
				file.Metadata += "\n"
			}
			file.Metadata += line
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

func diffMetadataPath(path string) string {
	if strings.HasPrefix(path, `"`) {
		if unquoted, err := strconv.Unquote(path); err == nil {
			return unquoted
		}
	}
	return path
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
	"diffFiles":        diffFiles,
	"localTimestamp":   func(value time.Time) string { return value.Local().Format("2006-01-02 15:04:05 MST") },
	"utcTimestamp":     func(value time.Time) string { return value.UTC().Format("2006-01-02 15:04:05 UTC") },
	"timestampMachine": func(value time.Time) string { return value.UTC().Format(time.RFC3339) },
}).Parse(`{{define "stylesheet"}}` + stylesheet + `{{end}}` + pageTemplateSource))

func embeddedTemplate(path string) string {
	contents, err := templateFiles.ReadFile(path)
	if err != nil {
		panic(err)
	}

	return string(contents)
}
