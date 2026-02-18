package parser

import (
	"fmt"
	"strings"
	"unicode"
)

// Lexer tokenizes TL schema input.
type Lexer struct {
	input string
	pos   int // current position in input
	line  int // current line number
	col   int // current column number
}

// NewLexer creates a new lexer for the given input.
func NewLexer(input string) *Lexer {
	return &Lexer{
		input: input,
		pos:   0,
		line:  1,
		col:   1,
	}
}

// NextToken returns the next token from the input.
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Line: l.line, Column: l.col}
	}

	ch := l.input[l.pos]

	// Handle section markers
	if l.isSectionMarker() {
		return l.readSectionMarker()
	}

	// Handle comments
	if ch == '/' && l.peekChar() == '/' {
		return l.readComment()
	}

	// Handle single-character tokens first
	switch ch {
	case ':':
		return l.newToken(TokenColon, string(ch))
	case ';':
		return l.newToken(TokenSemi, string(ch))
	case '=':
		return l.newToken(TokenEquals, string(ch))
	case '(':
		return l.newToken(TokenLParen, string(ch))
	case ')':
		return l.newToken(TokenRParen, string(ch))
	case '{':
		return l.newToken(TokenLBrace, string(ch))
	case '}':
		return l.newToken(TokenRBrace, string(ch))
	case '[':
		return l.newToken(TokenLBracket, string(ch))
	case ']':
		return l.newToken(TokenRBracket, string(ch))
	case '<':
		return l.newToken(TokenLess, string(ch))
	case '>':
		return l.newToken(TokenGreater, string(ch))
	case '?':
		return l.newToken(TokenQuestion, string(ch))
	case ',':
		return l.newToken(TokenComma, string(ch))
	case '#':
		// Handle special case: # [ for vector count syntax
		if l.peekChar() == ' ' && l.peekCharN(2) == '[' {
			// This is vector count syntax: # [ t ]
			tok := Token{
				Type:    TokenHashBracket,
				Literal: "# [",
				Line:    l.line,
				Column:  l.col,
			}
			l.advance(3) // skip # space [
			return tok
		}
		return l.newToken(TokenHash, string(ch))
	case '!':
		return l.newToken(TokenBang, string(ch))
	case '%':
		return l.newToken(TokenPercent, string(ch))
	case '.':
		return l.newToken(TokenDot, string(ch))
	case '\n':
		return l.newToken(TokenNewLine, string(ch))
	}

	// Handle identifiers and numbers
	if unicode.IsLetter(rune(ch)) || ch == '_' || unicode.IsDigit(rune(ch)) {
		return l.readIdent()
	}

	// Unknown character
	return l.newToken(TokenEOF, string(ch))
}

// PeekToken returns the next token without advancing the position.
func (l *Lexer) PeekToken() Token {
	savedPos := l.pos
	savedLine := l.line
	savedCol := l.col

	token := l.NextToken()

	l.pos = savedPos
	l.line = savedLine
	l.col = savedCol

	return token
}

// newToken creates a new token and advances the position.
func (l *Lexer) newToken(tokenType TokenType, literal string) Token {
	token := Token{
		Type:    tokenType,
		Literal: literal,
		Line:    l.line,
		Column:  l.col,
	}

	l.advance(len(literal))
	return token
}

// advance moves the position forward by n characters.
func (l *Lexer) advance(n int) {
	for i := 0; i < n; i++ {
		if l.pos >= len(l.input) {
			break
		}
		if l.input[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}
}

// peekChar returns the next character without advancing.
func (l *Lexer) peekChar() byte {
	if l.pos+1 >= len(l.input) {
		return 0
	}
	return l.input[l.pos+1]
}

// peekCharN returns the nth character ahead without advancing.
func (l *Lexer) peekCharN(n int) byte {
	if l.pos+n >= len(l.input) {
		return 0
	}
	return l.input[l.pos+n]
}

// skipWhitespace skips whitespace characters (but not newlines).
func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if ch == ' ' || ch == '\t' || ch == '\r' {
			l.advance(1)
		} else {
			break
		}
	}
}

// isSectionMarker checks if we're at a section marker (---types--- or ---functions---).
func (l *Lexer) isSectionMarker() bool {
	if l.pos+3 >= len(l.input) {
		return false
	}
	return strings.HasPrefix(l.input[l.pos:], "---")
}

// readSectionMarker reads a section marker token.
func (l *Lexer) readSectionMarker() Token {
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '\n' {
		l.pos++
	}

	literal := l.input[start:l.pos]
	col := l.col
	l.col += len(literal)

	switch literal {
	case "---types---":
		return Token{Type: TokenTypes, Literal: literal, Line: l.line, Column: col}
	case "---functions---":
		return Token{Type: TokenFunctions, Literal: literal, Line: l.line, Column: col}
	}

	// Invalid section marker, treat as identifier
	l.pos = start
	return l.readIdent()
}

// readComment reads a comment token.
func (l *Lexer) readComment() Token {
	start := l.pos
	for l.pos < len(l.input) && l.input[l.pos] != '\n' {
		l.pos++
	}

	literal := l.input[start:l.pos]
	col := l.col
	l.col += len(literal)

	return Token{Type: TokenComment, Literal: literal, Line: l.line, Column: col}
}

// readIdent reads an identifier token.
func (l *Lexer) readIdent() Token {
	start := l.pos

	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if !unicode.IsLetter(rune(ch)) && !unicode.IsDigit(rune(ch)) && ch != '_' {
			break
		}
		l.pos++
	}

	literal := l.input[start:l.pos]
	col := l.col
	l.col += len(literal)

	// Check if this looks like a hex number - if so, return as number token
	if isAllHexDigits(literal) && unicode.IsDigit(rune(literal[0])) {
		return Token{Type: TokenNumber, Literal: literal, Line: l.line, Column: col}
	}

	return Token{Type: TokenIdent, Literal: literal, Line: l.line, Column: col}
}

// isHexDigit checks if a character is a valid hex digit.
func isHexDigit(ch byte) bool {
	return unicode.IsDigit(rune(ch)) ||
		(ch >= 'a' && ch <= 'f') ||
		(ch >= 'A' && ch <= 'F')
}

// isAllHexDigits checks if a string consists entirely of hex digits, optionally prefixed with 0x.
func isAllHexDigits(s string) bool {
	if len(s) == 0 {
		return false
	}

	start := 0
	if len(s) >= 2 && s[0] == '0' && s[1] == 'x' {
		start = 2
	}

	for i := start; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	return true
}

func isAllDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !unicode.IsDigit(rune(s[i])) {
			return false
		}
	}
	return true
}

// Position returns the current position information.
func (l *Lexer) Position() (line, col int) {
	return l.line, l.col
}

// Error creates an error message with position information.
func (l *Lexer) Error(msg string) error {
	return fmt.Errorf("lexer error at %d:%d: %s", l.line, l.col, msg)
}
