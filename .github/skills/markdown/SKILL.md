---
name: markdown
description: "Write Markdown that passes this repository's rumdl formatting and lint checks on the first attempt. Use when creating or editing any .md file in this repository, such as README.md, docs/, or release notes, and before committing Markdown changes."
---

# Markdown for this repository

Markdown is formatted and linted by [rumdl](https://github.com/rvben/rumdl) using
[`.rumdl.toml`](../../../.rumdl.toml). The pre-commit hook runs `rumdl fmt --check` on every staged
Markdown file and **fails if rumdl would change anything**, so the file must already be in rumdl's
canonical form. Write it that way from the start instead of relying on a fix-up pass.

Files under `.github/**` are excluded by the config, but follow the same rules there for consistency.

## Workflow

1. Write the file following the rules below.
2. Format and lint it from the repository root:

   ```bash
   mise exec -- rumdl fmt path/to/file.md    # applies every auto-fix
   mise exec -- rumdl check path/to/file.md  # must report "No issues found"
   ```

   `mise run format` formats every tracked file, including Go, shell, and TOML.
3. Review the diff produced by `rumdl fmt`. Auto-fixes can be wrong for the intent, for example
   demoting a second `#` heading or rewrapping a line at an awkward point. Adjust the source so the
   result reads well.
4. Fix everything `rumdl check` still reports. Those rules cannot be auto-fixed, and the pre-commit
   hook does not catch them.

## Rules

### Headings

* Use ATX headings (`# Title`). Never use setext underlines (`===`, `---`).
* Exactly one `#` heading per file, and it must be the first line.
* Increase levels one at a time (`##` then `###`, never `##` then `####`).
* Put one blank line above every heading. A blank line below is optional.
* No trailing punctuation such as `:` or `.` in headings.
* Do not repeat heading text among siblings. Identical subheadings under different parents are fine.
* Never use a bold or italic line as a pseudo-heading. rumdl converts `*Label*` on its own line into
  a heading.

### Line length and wrapping

* Keep every line at most **120 characters**, including headings, list items, and blockquotes.
* rumdl splits lines that are too long but never joins short lines, so hard-wrapping prose below 120
  is fine. Prefer wrapping near 100 characters so later edits do not push lines over the limit.
* Tables and code blocks are exempt from the length limit.
* Put a long URL on its own line in angle brackets or link syntax; it cannot be wrapped.

### Lists

* Use `*` for every unordered list item. Never use `-` or `+`.
* Put exactly one space after the list marker.
* Indent nested unordered lists by **4 spaces**.
* Number ordered lists sequentially (`1.`, `2.`, `3.`).
* Indent paragraphs, code blocks, and nested content inside a list item to align with the item's
  text: 2 spaces for a `*` item and 3 spaces for a `1.` item. Inside nested lists, add the 4-space
  nesting.
* Lists may start directly after a paragraph or heading without a blank line.

### Emphasis

* Italic is `*text*`; bold is `**text**`. Never use `_text_` or `__text__`.

### Code

* Always use fenced code blocks with backticks (```` ``` ````), never tildes or 4-space indentation.
* Always set a language after the opening fence, such as `bash`, `go`, `powershell`, `toml`, or
  `yaml`. Use `text` for plain output.
* Put a blank line before and after every fenced block.
* Never start or end an inline code span with a space: write `` `foo` ``, not `` ` foo ` ``.

### Links and images

* Never leave a bare URL in prose. Write `<https://example.com>` or `[descriptive text](https://example.com)`.
* Link text must describe the target. Avoid `here`, `link`, `this`, `click here`, and similar.
* Relative links must point to files that exist.
* `#fragment` links must match a real heading slug: lowercase, punctuation removed, spaces turned
  into `-`.
* Every image needs alt text: `![Architecture overview](diagram.png)`.

### Proper names

* Outside code spans and code blocks, write `Markdown`, `GitHub`, and `rumdl` exactly like this.
  Wrap identifiers, commands, and paths in backticks.

### Whitespace and HTML

* No trailing whitespace, except two spaces for an intentional line break. Prefer a trailing `\`
  for line breaks.
* No tabs. Use spaces.
* At most one consecutive blank line.
* End the file with exactly one newline.
* Avoid inline HTML, including `<br>`, `<details>`, and `<div>`. Use Markdown constructs instead.
  HTML comments are allowed.

## Template

````markdown
# Document title

Short introduction wrapped below 120 characters. Reference `code` and link to
[the release guide](docs/releases.md).

## Section

* First point.
* Second point with a nested list:
    * Nested point.

1. Run the command:

   ```bash
   mise run test
   ```

2. Check the output.
````
