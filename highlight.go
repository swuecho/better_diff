package main

import (
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

// SyntaxHighlighter handles syntax highlighting for diff content
type SyntaxHighlighter struct {
	style *chroma.Style
}

// NewSyntaxHighlighter creates a new syntax highlighter
func NewSyntaxHighlighter() *SyntaxHighlighter {
	// Use a terminal-friendly style that works well with our color scheme
	style := styles.Get("monokai")
	if style == nil {
		style = styles.Fallback
	}
	return &SyntaxHighlighter{style: style}
}

// Highlight highlights a line of code based on file extension
func (h *SyntaxHighlighter) Highlight(line, filePath string) string {
	lexer := h.getLexer(filePath)
	if lexer == nil {
		return line
	}

	// Tokenize the line
	iterator, err := lexer.Tokenise(nil, line)
	if err != nil {
		return line
	}

	// Convert tokens to styled string
	var result strings.Builder
	for _, token := range iterator.Tokens() {
		styled := h.styleToken(token)
		result.WriteString(styled)
	}

	return result.String()
}

// getLexer returns the appropriate lexer for a file path
func (h *SyntaxHighlighter) getLexer(filePath string) chroma.Lexer {
	if filePath == "" {
		return nil
	}

	// Get file extension
	ext := strings.ToLower(filepath.Ext(filePath))

	// Try to get lexer by extension first
	lexer := lexers.Get(ext)
	if lexer != nil {
		return lexer
	}

	// Try to get lexer by filename
	lexer = lexers.Get(filepath.Base(filePath))
	if lexer != nil {
		return lexer
	}

	// Fallback to matching by content type
	switch ext {
	case ".go":
		return lexers.Get("go")
	case ".js", ".jsx", ".mjs":
		return lexers.Get("javascript")
	case ".ts", ".tsx":
		return lexers.Get("typescript")
	case ".py":
		return lexers.Get("python")
	case ".rs":
		return lexers.Get("rust")
	case ".rb":
		return lexers.Get("ruby")
	case ".java":
		return lexers.Get("java")
	case ".c", ".h":
		return lexers.Get("c")
	case ".cpp", ".cc", ".cxx", ".hpp", ".hxx":
		return lexers.Get("cpp")
	case ".cs":
		return lexers.Get("csharp")
	case ".swift":
		return lexers.Get("swift")
	case ".kt", ".kts":
		return lexers.Get("kotlin")
	case ".scala":
		return lexers.Get("scala")
	case ".sh", ".bash", ".zsh":
		return lexers.Get("bash")
	case ".fish":
		return lexers.Get("fish")
	case ".ps1":
		return lexers.Get("powershell")
	case ".json":
		return lexers.Get("json")
	case ".yaml", ".yml":
		return lexers.Get("yaml")
	case ".toml":
		return lexers.Get("toml")
	case ".xml", ".svg", ".xhtml":
		return lexers.Get("xml")
	case ".html", ".htm":
		return lexers.Get("html")
	case ".css":
		return lexers.Get("css")
	case ".scss", ".sass":
		return lexers.Get("scss")
	case ".less":
		return lexers.Get("less")
	case ".sql":
		return lexers.Get("sql")
	case ".md", ".markdown":
		return lexers.Get("markdown")
	case ".dockerfile":
		return lexers.Get("docker")
	case ".makefile", ".mk":
		return lexers.Get("make")
	case ".vue":
		return lexers.Get("vue")
	case ".svelte":
		return lexers.Get("svelte")
	default:
		// Try to guess from the full path
		lexer = lexers.Match(filePath)
		if lexer != nil {
			return lexer
		}
	}

	return nil
}

// styleToken applies lipgloss styling to a chroma token
func (h *SyntaxHighlighter) styleToken(token chroma.Token) string {
	content := token.Value
	entry := h.style.Get(token.Type)

	// Check if entry is empty (no styling)
	if entry == (chroma.StyleEntry{}) {
		return content
	}

	style := lipgloss.NewStyle()

	// Apply color
	if entry.Colour.IsSet() {
		color := entry.Colour.String()
		if strings.HasPrefix(color, "#") {
			style = style.Foreground(lipgloss.Color(color))
		}
	}

	// Apply bold
	if entry.Bold == chroma.Yes {
		style = style.Bold(true)
	}

	// Apply italic
	if entry.Italic == chroma.Yes {
		style = style.Italic(true)
	}

	// Apply underline
	if entry.Underline == chroma.Yes {
		style = style.Underline(true)
	}

	return style.Render(content)
}
