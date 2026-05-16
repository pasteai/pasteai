package renderer_test

import (
	"strings"
	"testing"

	"github.com/pasteai/pasteai/internal/renderer"
)

func TestRenderBasicMarkdown(t *testing.T) {
	result, err := renderer.Render("**bold** and *italic*")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	html := string(result.HTML)
	if !strings.Contains(html, "<strong>bold</strong>") {
		t.Errorf("expected <strong>bold</strong> in output, got: %s", html)
	}
	if !strings.Contains(html, "<em>italic</em>") {
		t.Errorf("expected <em>italic</em> in output, got: %s", html)
	}
}

func TestRenderHeadingsExtracted(t *testing.T) {
	md := "# Top Heading\n\n## Sub Heading\n\n### Deep Heading\n\n#### Ignored"
	result, err := renderer.Render(md)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Headings) != 3 {
		t.Fatalf("expected 3 headings (h1-h3), got %d: %+v", len(result.Headings), result.Headings)
	}
	if result.Headings[0].Level != 1 || result.Headings[0].Text != "Top Heading" {
		t.Errorf("heading[0] = %+v", result.Headings[0])
	}
	if result.Headings[1].Level != 2 || result.Headings[1].Text != "Sub Heading" {
		t.Errorf("heading[1] = %+v", result.Headings[1])
	}
	if result.Headings[2].Level != 3 || result.Headings[2].Text != "Deep Heading" {
		t.Errorf("heading[2] = %+v", result.Headings[2])
	}
}

func TestRenderHeadingIDs(t *testing.T) {
	result, err := renderer.Render("## Hello World")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Headings) == 0 {
		t.Fatal("expected at least one heading")
	}
	if result.Headings[0].ID == "" {
		t.Error("expected non-empty heading ID")
	}
	if !strings.Contains(string(result.HTML), `id="hello-world"`) {
		t.Errorf("expected id=hello-world in HTML: %s", result.HTML)
	}
}

func TestRenderNoHeadingsWhenNone(t *testing.T) {
	result, err := renderer.Render("Just a paragraph with no headings.")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Headings) != 0 {
		t.Errorf("expected no headings, got %d", len(result.Headings))
	}
}

func TestRenderGFMTable(t *testing.T) {
	md := "| A | B |\n|---|---|\n| 1 | 2 |"
	result, err := renderer.Render(md)
	if err != nil {
		t.Fatal(err)
	}
	html := string(result.HTML)
	if !strings.Contains(html, "<table>") {
		t.Errorf("expected <table> in output: %s", html)
	}
	if !strings.Contains(html, "<th>") {
		t.Errorf("expected <th> in output: %s", html)
	}
}

func TestRenderGFMTaskList(t *testing.T) {
	md := "- [x] Done\n- [ ] Not done"
	result, err := renderer.Render(md)
	if err != nil {
		t.Fatal(err)
	}
	html := string(result.HTML)
	if !strings.Contains(html, `type="checkbox"`) {
		t.Errorf("expected checkbox input in output: %s", html)
	}
}

func TestRenderCodeBlock(t *testing.T) {
	md := "```go\nfmt.Println(\"hello\")\n```"
	result, err := renderer.Render(md)
	if err != nil {
		t.Fatal(err)
	}
	html := string(result.HTML)
	if !strings.Contains(html, "<pre") {
		t.Errorf("expected <pre> in output: %s", html)
	}
	// Chroma adds CSS class spans (class-based output, no inline styles)
	if !strings.Contains(html, `class="chroma"`) {
		t.Errorf("expected Chroma class in output: %s", html)
	}
}

func TestRenderDarkStyle(t *testing.T) {
	// ThemeCSS must produce distinct CSS for light vs dark themes.
	css := string(renderer.ThemeCSS())
	if !strings.Contains(css, `data-theme="light"`) {
		t.Error("ThemeCSS missing light theme selector")
	}
	if !strings.Contains(css, `data-theme="dark"`) {
		t.Error("ThemeCSS missing dark theme selector")
	}
	lightIdx := strings.Index(css, `data-theme="light"`)
	darkIdx := strings.Index(css, `data-theme="dark"`)
	if lightIdx == -1 || darkIdx == -1 || lightIdx == darkIdx {
		t.Error("ThemeCSS must contain separate light and dark sections")
	}
}

func TestThemeCSSBackgroundStripped(t *testing.T) {
	css := string(renderer.ThemeCSS())

	// The unscoped .bg rule must be absent — it would override arbitrary page
	// elements and is never used (our HTML emits no class="bg").
	if strings.Contains(css, "/* Background */") {
		t.Error("ThemeCSS must not contain the unscoped .bg Background rule")
	}

	// Extract the PreWrapper lines and assert they carry no background-color.
	// If they did, the Chroma-injected background would fight our CSS variable
	// (--color-surface-card-strong) and win on some browsers due to source order.
	for _, line := range strings.Split(css, "\n") {
		if !strings.Contains(line, "/* PreWrapper */") {
			continue
		}
		if strings.Contains(line, "background-color") {
			t.Errorf("PreWrapper rule must not set background-color (blocks CSS variable): %s", line)
		}
	}

	// Sanity: token colours are still present for both theme groups.
	if !strings.Contains(css, `data-theme="light"`) {
		t.Error("ThemeCSS missing light scope")
	}
	if !strings.Contains(css, `data-theme="dark"`) {
		t.Error("ThemeCSS missing dark scope")
	}
}

func TestRenderHTMLStripped(t *testing.T) {
	// Raw HTML in markdown is stripped (WithUnsafe removed to prevent XSS).
	md := "before\n\n<div class=\"custom\">raw html</div>\n\nafter"
	result, err := renderer.Render(md)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(result.HTML), `class="custom"`) {
		t.Errorf("raw HTML should be stripped, not passed through: %s", result.HTML)
	}
	if !strings.Contains(string(result.HTML), "before") || !strings.Contains(string(result.HTML), "after") {
		t.Errorf("surrounding markdown should still render: %s", result.HTML)
	}
}

func TestRenderStrikethrough(t *testing.T) {
	result, err := renderer.Render("~~deleted~~")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.HTML), "<del>") {
		t.Errorf("expected <del> for strikethrough: %s", result.HTML)
	}
}

func TestRenderReturnsTrustedHTML(t *testing.T) {
	result, err := renderer.Render("hello")
	if err != nil {
		t.Fatal(err)
	}
	// template.HTML type ensures Go's template engine won't double-escape it
	_ = result.HTML
}
