package renderer

import (
	"bytes"
	"fmt"
	"html/template"
	"regexp"
	"strings"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
)

// Heading holds a parsed heading for TOC generation.
type Heading struct {
	Level int
	ID    string
	Text  string
}

// Result is the output of rendering a markdown document.
type Result struct {
	HTML     template.HTML
	Headings []Heading
}

// sanitiser is built once; UGCPolicy is a well-tested allowlist that permits
// safe formatting tags while stripping scripts, event handlers, and dangerous
// URI schemes. It is extended to allow the class attributes that Chroma emits
// for syntax-highlighted code blocks (class="chroma", class="k", etc.).
var sanitiser = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// Allow class attributes on elements that Chroma uses for highlighting.
	p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).OnElements(
		"code", "pre", "span", "div",
	)
	// Allow id attributes on headings so TOC anchor links work.
	p.AllowAttrs("id").Matching(bluemonday.SpaceSeparatedTokens).OnElements(
		"h1", "h2", "h3", "h4", "h5", "h6",
	)
	// Allow the checked attribute on inputs for GFM task list checkboxes.
	p.AllowAttrs("checked", "disabled", "type").OnElements("input")
	return p
}()

// md is built once and reused — goldmark options are stateless.
var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		highlighting.NewHighlighting(
			highlighting.WithStyle("github-dark"),
			highlighting.WithFormatOptions(
				chromahtml.WithClasses(true),
				chromahtml.WithLineNumbers(false),
			),
		),
	),
	goldmark.WithParserOptions(
		parser.WithAutoHeadingID(),
	),
)

// Render converts markdown source to HTML and extracts headings for TOC.
// Class-based Chroma output is used; colours come from ThemeCSS().
// The rendered HTML is passed through bluemonday's UGC policy for defence-in-depth
// sanitisation before being cast to template.HTML.
func Render(source string) (*Result, error) {
	var buf bytes.Buffer
	if err := md.Convert([]byte(source), &buf); err != nil {
		return nil, fmt.Errorf("renderer: convert markdown: %w", err)
	}

	safe := sanitiser.SanitizeBytes(buf.Bytes())
	html := string(safe)
	return &Result{
		HTML:     template.HTML(html),
		Headings: extractHeadings(html),
	}, nil
}

// ThemeCSS returns a CSS string with Chroma token colours scoped to each
// theme group via [data-theme] attribute selectors. Embed this in a <style>
// tag in the document <head>.
func ThemeCSS() template.CSS {
	light := scopedCSS("github",
		`:is([data-theme="light"],[data-theme="catppuccin-latte"])`)
	dark := scopedCSS("github-dark",
		`:is([data-theme="dark"],[data-theme="emerald"],[data-theme="catppuccin-mocha"],[data-theme="catppuccin-frappe"],[data-theme="arctic"])`)
	return template.CSS(light + "\n" + dark)
}

// scopedCSS generates Chroma CSS for the named style and prefixes every
// selector with scopeSelector so the colours only apply under matching themes.
// The PreWrapper background-color is stripped so our CSS variable controls it.
func scopedCSS(styleName, scopeSelector string) string {
	style := chromastyles.Get(styleName)
	if style == nil {
		style = chromastyles.Fallback
	}
	formatter := chromahtml.New(chromahtml.WithClasses(true))
	var buf bytes.Buffer
	_ = formatter.WriteCSS(&buf, style)
	css := strings.ReplaceAll(buf.String(), ".chroma", scopeSelector+" .chroma")

	var out strings.Builder
	for _, line := range strings.Split(css, "\n") {
		// The unscoped .bg rule is unused — our HTML never emits class="bg".
		if strings.Contains(line, "/* Background */") {
			continue
		}
		// Strip background-color from the PreWrapper rule so the CSS variable
		// in .markdown-body pre.chroma wins without a specificity fight.
		if strings.Contains(line, "/* PreWrapper */") {
			line = stripProperty(line, "background-color")
		}
		out.WriteString(line)
		out.WriteByte('\n')
	}
	return out.String()
}

// stripProperty removes all occurrences of "prop: value;" from a CSS rule string.
func stripProperty(rule, prop string) string {
	for {
		i := strings.Index(rule, prop+":")
		if i == -1 {
			break
		}
		j := strings.Index(rule[i:], ";")
		if j == -1 {
			break
		}
		end := i + j + 1
		if end < len(rule) && rule[end] == ' ' {
			end++
		}
		rule = rule[:i] + rule[end:]
	}
	return rule
}

// headingRE matches h1–h3 tags that goldmark emits with WithAutoHeadingID:
// <h1 id="slug">content</h1>
// (?s) makes . match newlines so multi-line heading content is captured.
var headingRE = regexp.MustCompile(`(?s)<h([1-3]) id="([^"]+)">(.*?)</h[1-3]>`)

// extractHeadings parses <h1>–<h3> tags with id attributes from rendered HTML.
func extractHeadings(html string) []Heading {
	var headings []Heading
	for _, m := range headingRE.FindAllStringSubmatch(html, -1) {
		level := int(m[1][0] - '0')
		id := m[2]
		text := stripTags(m[3])
		if text != "" {
			headings = append(headings, Heading{Level: level, ID: id, Text: text})
		}
	}
	return headings
}

// stripTags removes HTML tags from a string.
func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, ch := range s {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(ch)
		}
	}
	return strings.TrimSpace(b.String())
}
