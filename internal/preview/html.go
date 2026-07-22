package preview

import (
	"bytes"
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

var pageTemplate = template.Must(template.New("preview").Funcs(template.FuncMap{
	"diffLines": diffLines,
}).Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>autofeat preview</title>
  <style>
    :root { color-scheme: light dark; font-family: system-ui, sans-serif; }
    body { margin: 0 auto; max-width: 110rem; padding: 2rem; }
    h1 { margin-bottom: 0.25rem; }
    h2 { border-bottom: 1px solid #8886; margin-top: 2.5rem; padding-bottom: 0.35rem; }
    h3 { margin-bottom: 0.5rem; }
    .metadata, .empty { color: #666; }
    .repository { border: 1px solid #8886; border-radius: 0.5rem; margin: 1rem 0; overflow: hidden; }
    .repository h3 { background: #8882; margin: 0; padding: 0.75rem 1rem; }
    .repository p { padding: 0 1rem; }
    .error { color: #b42318; white-space: pre-wrap; }
    pre { margin: 0; overflow-x: auto; padding: 1rem; tab-size: 4; }
    .diff-file { color: #8250df; font-weight: 700; }
    .diff-hunk { color: #0969da; }
    .diff-path, .diff-meta { color: #666; }
    .diff-addition { background: #2da44e33; color: #1a7f37; }
    .diff-deletion { background: #cf222e33; color: #cf222e; }
    .diff-context { color: inherit; }
  </style>
</head>
<body>
  <h1>autofeat preview</h1>
  <p class="metadata">Generated {{.GeneratedAt.Format "2006-01-02 15:04:05 MST"}} · Base branch: {{.BaseRef}}</p>
  {{if .Sessions}}
    {{range .Sessions}}
      <section>
        <h2>{{.FeatureName}}</h2>
        {{if .Repositories}}
          {{range .Repositories}}
            <article class="repository">
              <h3>{{.Name}}</h3>
              {{if .Error}}
                <p class="error">{{.Error}}</p>
              {{else if .Diff}}
                <pre>{{range diffLines .Diff}}<span class="{{.Class}}">{{.Text}}</span>
{{end}}</pre>
              {{else}}
                <p class="empty">No changes relative to {{$.BaseRef}}.</p>
              {{end}}
            </article>
          {{end}}
        {{else}}
          <p class="empty">This feature session has no repositories.</p>
        {{end}}
      </section>
    {{end}}
  {{else}}
    <p class="empty">No active feature sessions were found.</p>
  {{end}}
</body>
</html>
`))
