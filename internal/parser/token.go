// Package codegen provides TL schema parsing and code generation.
package parser

import "fmt"

// TokenType represents the type of a token in TL schema.
type TokenType int

// Token types for TL schema parsing.
const (
	TokenEOF         TokenType = iota
	TokenIdent                 // user, messages.sendMessage
	TokenNumber                // 123, 0x520c3870
	TokenColon                 // :
	TokenSemi                  // ;
	TokenEquals                // =
	TokenLParen                // (
	TokenRParen                // )
	TokenLBrace                // {
	TokenRBrace                // }
	TokenLBracket              // [
	TokenRBracket              // ]
	TokenLess                  // <
	TokenGreater               // >
	TokenQuestion              // ?
	TokenHash                  // #
	TokenBang                  // !
	TokenPercent               // %
	TokenDot                   // .
	TokenComma                 // ,
	TokenUnderscore            // _
	TokenNewLine               // \n
	TokenComment               // // comment
	TokenTypes                 // ---types---
	TokenFunctions             // ---functions---
	TokenHashBracket           // # [ for vector count syntax
)

// String returns a string representation of the token type.
func (t TokenType) String() string {
	switch t {
	case TokenEOF:
		return "EOF"
	case TokenIdent:
		return "IDENT"
	case TokenNumber:
		return "NUMBER"
	case TokenColon:
		return "COLON"
	case TokenSemi:
		return "SEMI"
	case TokenEquals:
		return "EQUALS"
	case TokenLParen:
		return "LPAREN"
	case TokenRParen:
		return "RPAREN"
	case TokenLBrace:
		return "LBRACE"
	case TokenRBrace:
		return "RBRACE"
	case TokenLBracket:
		return "LBRACKET"
	case TokenRBracket:
		return "RBRACKET"
	case TokenLess:
		return "LESS"
	case TokenGreater:
		return "GREATER"
	case TokenQuestion:
		return "QUESTION"
	case TokenHash:
		return "HASH"
	case TokenBang:
		return "BANG"
	case TokenPercent:
		return "PERCENT"
	case TokenDot:
		return "DOT"
	case TokenComma:
		return "COMMA"
	case TokenUnderscore:
		return "UNDERSCORE"
	case TokenNewLine:
		return "NEWLINE"
	case TokenComment:
		return "COMMENT"
	case TokenTypes:
		return "TYPES"
	case TokenFunctions:
		return "FUNCTIONS"
	case TokenHashBracket:
		return "HASH_BRACKET"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", int(t))
	}
}

// Token represents a lexical token in TL schema.
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

// String returns a string representation of the token.
func (t Token) String() string {
	if t.Literal == "" {
		return fmt.Sprintf("%s at %d:%d", t.Type.String(), t.Line, t.Column)
	}
	return fmt.Sprintf("%s(%q) at %d:%d", t.Type.String(), t.Literal, t.Line, t.Column)
}
