package expression

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Parser parses factor expression DSL strings into AST
type Parser struct {
	tokens []token
	pos    int
}

// token represents a lexical token
type token struct {
	typ tokenType
	val string
}

type tokenType int

const (
	tokenEOF tokenType = iota
	tokenNumber
	tokenIdentifier
	tokenLParen
	tokenRParen
	tokenComma
	tokenPlus
	tokenMinus
	tokenMul
	tokenDiv
	tokenPow
	tokenGT
	tokenLT
	tokenEQ
	// Add more operators as needed
)

// NewParser creates a new expression parser
func NewParser() *Parser {
	return &Parser{}
}

// Parse parses a factor expression string into an AST
func (p *Parser) Parse(formula string) (*Expression, error) {
	if strings.TrimSpace(formula) == "" {
		return nil, fmt.Errorf("empty formula")
	}

	tokens, err := tokenize(formula)
	if err != nil {
		return nil, fmt.Errorf("tokenization failed: %w", err)
	}

	p.tokens = tokens
	p.pos = 0

	ast, err := p.parseExpression()
	if err != nil {
		return nil, fmt.Errorf("parse failed: %w", err)
	}

	if !p.isAtEnd() {
		return nil, fmt.Errorf("unexpected token: %s", p.current().val)
	}

	expr := &Expression{
		Formula: formula,
		AST:     ast,
		Inputs:  ExtractInputs(ast),
	}

	return expr, nil
}

// tokenize converts a formula string into tokens
func tokenize(formula string) ([]token, error) {
	var tokens []token
	runes := []rune(formula)
	i := 0

	for i < len(runes) {
		r := runes[i]

		// Skip whitespace
		if unicode.IsSpace(r) {
			i++
			continue
		}

		// Number
		if unicode.IsDigit(r) || r == '.' {
			start := i
			hasDot := r == '.'
			i++
			for i < len(runes) && (unicode.IsDigit(runes[i]) || runes[i] == '.') {
				if runes[i] == '.' {
					if hasDot {
						return nil, fmt.Errorf("invalid number at position %d", i)
					}
					hasDot = true
				}
				i++
			}
			tokens = append(tokens, token{typ: tokenNumber, val: string(runes[start:i])})
			continue
		}

		// Identifier (function name or data field)
		if unicode.IsLetter(r) || r == '_' {
			start := i
			i++
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			tokens = append(tokens, token{typ: tokenIdentifier, val: string(runes[start:i])})
			continue
		}

		// Operators and punctuation
		switch r {
		case '(':
			tokens = append(tokens, token{typ: tokenLParen, val: "("})
		case ')':
			tokens = append(tokens, token{typ: tokenRParen, val: ")"})
		case ',':
			tokens = append(tokens, token{typ: tokenComma, val: ","})
		case '+':
			tokens = append(tokens, token{typ: tokenPlus, val: "+"})
		case '-':
			tokens = append(tokens, token{typ: tokenMinus, val: "-"})
		case '*':
			tokens = append(tokens, token{typ: tokenMul, val: "*"})
		case '/':
			tokens = append(tokens, token{typ: tokenDiv, val: "/"})
		case '^':
			tokens = append(tokens, token{typ: tokenPow, val: "^"})
		case '>':
			tokens = append(tokens, token{typ: tokenGT, val: ">"})
		case '<':
			tokens = append(tokens, token{typ: tokenLT, val: "<"})
		case '=':
			if i+1 < len(runes) && runes[i+1] == '=' {
				tokens = append(tokens, token{typ: tokenEQ, val: "=="})
				i++
			} else {
				return nil, fmt.Errorf("unexpected character '=' at position %d", i)
			}
		default:
			return nil, fmt.Errorf("unexpected character '%c' at position %d", r, i)
		}
		i++
	}

	tokens = append(tokens, token{typ: tokenEOF, val: ""})
	return tokens, nil
}

// parseExpression parses the top-level expression (handles comparisons)
func (p *Parser) parseExpression() (Node, error) {
	return p.parseComparison()
}

// parseAdditive parses addition and subtraction
func (p *Parser) parseAdditive() (Node, error) {
	left, err := p.parseMultiplicative()
	if err != nil {
		return nil, err
	}

	for p.match(tokenPlus, tokenMinus) {
		op := p.previous().val
		right, err := p.parseMultiplicative()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{Op: op, Left: left, Right: right}
	}

	return left, nil
}

// parseMultiplicative parses multiplication and division
func (p *Parser) parseMultiplicative() (Node, error) {
	left, err := p.parsePower()
	if err != nil {
		return nil, err
	}

	for p.match(tokenMul, tokenDiv) {
		op := p.previous().val
		right, err := p.parsePower()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{Op: op, Left: left, Right: right}
	}

	return left, nil
}

// parseComparison parses comparison operators
func (p *Parser) parseComparison() (Node, error) {
	left, err := p.parseAdditive()
	if err != nil {
		return nil, err
	}

	for p.match(tokenGT, tokenLT, tokenEQ) {
		op := p.previous().val
		right, err := p.parseAdditive()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{Op: op, Left: left, Right: right}
	}

	return left, nil
}

// parsePower parses exponentiation
func (p *Parser) parsePower() (Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for p.match(tokenPow) {
		op := p.previous().val
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = &BinaryOpNode{Op: op, Left: left, Right: right}
	}

	return left, nil
}

// parseUnary parses unary operators
func (p *Parser) parseUnary() (Node, error) {
	if p.match(tokenMinus) {
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &UnaryOpNode{Op: "neg", Expr: expr}, nil
	}

	return p.parsePrimary()
}

// parsePrimary parses primary expressions (literals, identifiers, function calls, parentheses)
func (p *Parser) parsePrimary() (Node, error) {
	if p.match(tokenNumber) {
		val, err := strconv.ParseFloat(p.previous().val, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %s", p.previous().val)
		}
		return &LiteralNode{Value: val}, nil
	}

	if p.match(tokenIdentifier) {
		name := p.previous().val

		// Check if it's a function call
		if p.check(tokenLParen) {
			return p.parseFunctionCall(name)
		}

		// It's a data field identifier
		return &IdentifierNode{Name: name}, nil
	}

	if p.match(tokenLParen) {
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if !p.match(tokenRParen) {
			return nil, fmt.Errorf("expected ')'")
		}
		return expr, nil
	}

	return nil, fmt.Errorf("unexpected token: %s", p.current().val)
}

// parseFunctionCall parses a function call (e.g., ts_mean(close, 20))
func (p *Parser) parseFunctionCall(name string) (Node, error) {
	if !p.match(tokenLParen) {
		return nil, fmt.Errorf("expected '(' after function name")
	}

	var args []Node
	if !p.check(tokenRParen) {
		for {
			arg, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			args = append(args, arg)

			if !p.match(tokenComma) {
				break
			}
		}
	}

	if !p.match(tokenRParen) {
		return nil, fmt.Errorf("expected ')'")
	}

	// Check if it's a cross-sectional operator
	if IsCrossSectionalOp(name) {
		if len(args) != 1 {
			return nil, fmt.Errorf("%s requires exactly 1 argument, got %d", name, len(args))
		}
		return &CrossSectionalNode{Op: name, Expr: args[0]}, nil
	}

	return &FunctionNode{Name: name, Args: args}, nil
}

// Helper methods for token manipulation

func (p *Parser) match(types ...tokenType) bool {
	for _, t := range types {
		if p.check(t) {
			p.advance()
			return true
		}
	}
	return false
}

func (p *Parser) check(t tokenType) bool {
	if p.isAtEnd() {
		return false
	}
	return p.current().typ == t
}

func (p *Parser) advance() token {
	if !p.isAtEnd() {
		p.pos++
	}
	return p.previous()
}

func (p *Parser) isAtEnd() bool {
	return p.current().typ == tokenEOF
}

func (p *Parser) current() token {
	return p.tokens[p.pos]
}

func (p *Parser) previous() token {
	return p.tokens[p.pos-1]
}
