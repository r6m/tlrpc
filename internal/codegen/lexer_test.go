package codegen

import (
	"strings"
	"testing"
)

func TestLexer_NextToken(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []Token
	}{
		{
			name:  "empty input",
			input: "",
			expected: []Token{
				{Type: TokenEOF},
			},
		},
		{
			name:  "whitespace only",
			input: "   \t   ",
			expected: []Token{
				{Type: TokenEOF},
			},
		},
		{
			name:  "single tokens",
			input: ":;=(){}[]<>?#%!. _",
			expected: []Token{
				{Type: TokenColon, Literal: ":"},
				{Type: TokenSemi, Literal: ";"},
				{Type: TokenEquals, Literal: "="},
				{Type: TokenLParen, Literal: "("},
				{Type: TokenRParen, Literal: ")"},
				{Type: TokenLBrace, Literal: "{"},
				{Type: TokenRBrace, Literal: "}"},
				{Type: TokenLBracket, Literal: "["},
				{Type: TokenRBracket, Literal: "]"},
				{Type: TokenLess, Literal: "<"},
				{Type: TokenGreater, Literal: ">"},
				{Type: TokenQuestion, Literal: "?"},
				{Type: TokenHash, Literal: "#"},
				{Type: TokenPercent, Literal: "%"},
				{Type: TokenBang, Literal: "!"},
				{Type: TokenDot, Literal: "."},
				{Type: TokenIdent, Literal: "_"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "identifiers",
			input: "user messages_sendMessage _private user_empty",
			expected: []Token{
				{Type: TokenIdent, Literal: "user"},
				{Type: TokenIdent, Literal: "messages_sendMessage"},
				{Type: TokenIdent, Literal: "_private"},
				{Type: TokenIdent, Literal: "user_empty"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "numbers",
			input: "123 0x520c3870 42",
			expected: []Token{
				{Type: TokenNumber, Literal: "123"},
				{Type: TokenNumber, Literal: "0x520c3870"},
				{Type: TokenNumber, Literal: "42"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "section markers",
			input: "---types---\n---functions---",
			expected: []Token{
				{Type: TokenTypes, Literal: "---types---"},
				{Type: TokenNewLine, Literal: "\n"},
				{Type: TokenFunctions, Literal: "---functions---"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "comments",
			input: "// this is a comment\nuser#123",
			expected: []Token{
				{Type: TokenComment, Literal: "// this is a comment"},
				{Type: TokenNewLine, Literal: "\n"},
				{Type: TokenIdent, Literal: "user"},
				{Type: TokenHash, Literal: "#"},
				{Type: TokenNumber, Literal: "123"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "constructor example",
			input: "user#8f97c628 flags:# id:long first_name:string last_name:string = User;",
			expected: []Token{
				{Type: TokenIdent, Literal: "user"},
				{Type: TokenHash, Literal: "#"},
				{Type: TokenNumber, Literal: "8f97c628"},
				{Type: TokenIdent, Literal: "flags"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenHash, Literal: "#"},
				{Type: TokenIdent, Literal: "id"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "long"},
				{Type: TokenIdent, Literal: "first_name"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "string"},
				{Type: TokenIdent, Literal: "last_name"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "string"},
				{Type: TokenEquals, Literal: "="},
				{Type: TokenIdent, Literal: "User"},
				{Type: TokenSemi, Literal: ";"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "function example",
			input: "auth.sendCode#a677244f phone_number:string api_id:int api_hash:string = auth.SentCode;",
			expected: []Token{
				{Type: TokenIdent, Literal: "auth"},
				{Type: TokenDot, Literal: "."},
				{Type: TokenIdent, Literal: "sendCode"},
				{Type: TokenHash, Literal: "#"},
				{Type: TokenIdent, Literal: "a677244f"},
				{Type: TokenIdent, Literal: "phone_number"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "string"},
				{Type: TokenIdent, Literal: "api_id"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "int"},
				{Type: TokenIdent, Literal: "api_hash"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "string"},
				{Type: TokenEquals, Literal: "="},
				{Type: TokenIdent, Literal: "auth"},
				{Type: TokenDot, Literal: "."},
				{Type: TokenIdent, Literal: "SentCode"},
				{Type: TokenSemi, Literal: ";"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "bare type",
			input: "%userEmpty#d3bc4b7c = User;",
			expected: []Token{
				{Type: TokenPercent, Literal: "%"},
				{Type: TokenIdent, Literal: "userEmpty"},
				{Type: TokenHash, Literal: "#"},
				{Type: TokenIdent, Literal: "d3bc4b7c"},
				{Type: TokenEquals, Literal: "="},
				{Type: TokenIdent, Literal: "User"},
				{Type: TokenSemi, Literal: ";"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "conditional parameters",
			input: "flags:# id:long first_name:flags.0?string last_name:flags.1?string",
			expected: []Token{
				{Type: TokenIdent, Literal: "flags"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenHash, Literal: "#"},
				{Type: TokenIdent, Literal: "id"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "long"},
				{Type: TokenIdent, Literal: "first_name"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "flags"},
				{Type: TokenDot, Literal: "."},
				{Type: TokenNumber, Literal: "0"},
				{Type: TokenQuestion, Literal: "?"},
				{Type: TokenIdent, Literal: "string"},
				{Type: TokenIdent, Literal: "last_name"},
				{Type: TokenColon, Literal: ":"},
				{Type: TokenIdent, Literal: "flags"},
				{Type: TokenDot, Literal: "."},
				{Type: TokenNumber, Literal: "1"},
				{Type: TokenQuestion, Literal: "?"},
				{Type: TokenIdent, Literal: "string"},
				{Type: TokenEOF},
			},
		},
		{
			name:  "generic types",
			input: "vector<int> vector<vector<string>>",
			expected: []Token{
				{Type: TokenIdent, Literal: "vector"},
				{Type: TokenLess, Literal: "<"},
				{Type: TokenIdent, Literal: "int"},
				{Type: TokenGreater, Literal: ">"},
				{Type: TokenIdent, Literal: "vector"},
				{Type: TokenLess, Literal: "<"},
				{Type: TokenIdent, Literal: "vector"},
				{Type: TokenLess, Literal: "<"},
				{Type: TokenIdent, Literal: "string"},
				{Type: TokenGreater, Literal: ">"},
				{Type: TokenGreater, Literal: ">"},
				{Type: TokenEOF},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input)
			var got []Token

			for {
				token := lexer.NextToken()
				got = append(got, token)
				if token.Type == TokenEOF {
					break
				}
			}

			// Compare token types and literals (ignore position for simplicity)
			if len(got) != len(tt.expected) {
				t.Errorf("expected %d tokens, got %d", len(tt.expected), len(got))
				for i, token := range got {
					if i < len(tt.expected) {
						t.Errorf("token %d: got %v, expected %v", i, token.Type, tt.expected[i].Type)
					} else {
						t.Errorf("extra token %d: %v", i, token.Type)
					}
				}
				return
			}

			for i, token := range got {
				expected := tt.expected[i]
				if token.Type != expected.Type {
					t.Errorf("token %d: expected type %v, got %v", i, expected.Type, token.Type)
				}
				if expected.Literal != "" && token.Literal != expected.Literal {
					t.Errorf("token %d: expected literal %q, got %q", i, expected.Literal, token.Literal)
				}
			}
		})
	}
}

func TestLexer_PeekToken(t *testing.T) {
	lexer := NewLexer("user#123")

	// Peek should not advance
	peeked := lexer.PeekToken()
	if peeked.Type != TokenIdent || peeked.Literal != "user" {
		t.Errorf("PeekToken: expected IDENT(user), got %v", peeked)
	}

	// Next should return the same token
	next := lexer.NextToken()
	if next.Type != TokenIdent || next.Literal != "user" {
		t.Errorf("NextToken after peek: expected IDENT(user), got %v", next)
	}

	// Next should advance past peeked token
	next = lexer.NextToken()
	if next.Type != TokenHash || next.Literal != "#" {
		t.Errorf("NextToken after peek: expected HASH(#), got %v", next)
	}
}

func TestLexer_Position(t *testing.T) {
	lexer := NewLexer("user\n  #123")

	// Initial position
	line, col := lexer.Position()
	if line != 1 || col != 1 {
		t.Errorf("initial position: expected 1:1, got %d:%d", line, col)
	}

	// After reading "user"
	lexer.NextToken()
	line, col = lexer.Position()
	if line != 1 || col != 5 {
		t.Errorf("after 'user': expected 1:5, got %d:%d", line, col)
	}

	// After reading newline
	lexer.NextToken()
	line, col = lexer.Position()
	if line != 2 || col != 1 {
		t.Errorf("after newline: expected 2:1, got %d:%d", line, col)
	}

	// After reading spaces
	lexer.NextToken() // skip spaces
	line, col = lexer.Position()
	if line != 2 || col != 4 {
		t.Errorf("after spaces: expected 2:4, got %d:%d", line, col)
	}
}

func TestLexer_TokenPosition(t *testing.T) {
	lexer := NewLexer("user\n  #123")

	token := lexer.NextToken()
	if token.Line != 1 || token.Column != 1 {
		t.Errorf("'user' token position: expected 1:1, got %d:%d", token.Line, token.Column)
	}

	token = lexer.NextToken() // newline
	if token.Line != 1 || token.Column != 5 {
		t.Errorf("'\\n' token position: expected 1:5, got %d:%d", token.Line, token.Column)
	}

	// Skip spaces automatically
	token = lexer.NextToken() // #
	if token.Line != 2 || token.Column != 3 {
		t.Errorf("'#' token position: expected 2:3, got %d:%d", token.Line, token.Column)
	}
}

func TestLexer_GenericTokens(t *testing.T) {
	input := `vector#1cb5c415 {t:Type} # [ t ] = Vector t;`
	lexer := NewLexer(input)

	tests := []struct {
		expectedType    TokenType
		expectedLiteral string
	}{
		{TokenIdent, "vector"},
		{TokenHash, "#"},
		{TokenNumber, "1cb5c415"},
		{TokenLBrace, "{"},
		{TokenIdent, "t"},
		{TokenColon, ":"},
		{TokenIdent, "Type"},
		{TokenRBrace, "}"},
		{TokenHashBracket, "# ["},
		{TokenIdent, "t"},
		{TokenRBracket, "]"},
		{TokenEquals, "="},
		{TokenIdent, "Vector"},
		{TokenIdent, "t"},
		{TokenSemi, ";"},
		{TokenEOF, ""},
	}

	for i, tt := range tests {
		tok := lexer.NextToken()
		if tok.Type != tt.expectedType {
			t.Errorf("test %d: expected type %s, got %s", i, tt.expectedType, tok.Type)
		}
		if tok.Literal != tt.expectedLiteral {
			t.Errorf("test %d: expected literal %q, got %q", i, tt.expectedLiteral, tok.Literal)
		}
	}
}

func BenchmarkLexer_NextToken(b *testing.B) {
	input := strings.Repeat("user#8f97c628 flags:# id:long first_name:string last_name:string = User;\n", 1000)
	lexer := NewLexer(input)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		lexer.pos = 0
		lexer.line = 1
		lexer.col = 1

		for {
			token := lexer.NextToken()
			if token.Type == TokenEOF {
				break
			}
		}
	}
}
