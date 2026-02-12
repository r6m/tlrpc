package codegen

import (
	"strings"
	"unicode"
)

// Namer maps TL identifiers to Go identifiers.
type Namer struct {
	reserved map[string]bool
}

// NewNamer creates a Namer with default reserved keywords.
func NewNamer() *Namer {
	reserved := make(map[string]bool, len(goKeywords))
	for k, v := range goKeywords {
		reserved[k] = v
	}
	return &Namer{reserved: reserved}
}

func (n *Namer) ConstructorName(name string) string {
	return n.qualify(name, true)
}

func (n *Namer) TypeName(name string) string {
	return n.qualify(name, true)
}

// MethodName returns the Go method name (no namespace).
func (n *Namer) MethodName(name string) string {
	parts := strings.Split(name, ".")
	return n.exported(parts[len(parts)-1])
}

// ServiceName returns the Go service interface name (e.g., auth -> AuthServer).
func (n *Namer) ServiceName(name string) string {
	if name == "" {
		name = "root"
	}
	return n.exported(name) + "Server"
}

// FieldName returns the Go struct field name.
func (n *Namer) FieldName(name string) string {
	return n.exported(name)
}

// PackageName returns a Go package name from a TL name.
func (n *Namer) PackageName(name string) string {
	if name == "" {
		return "gen"
	}
	parts := strings.Split(name, ".")
	pkg := strings.ToLower(parts[len(parts)-1])
	if n.isReserved(pkg) {
		return pkg + "_"
	}
	return pkg
}

func (n *Namer) qualify(name string, joinAll bool) string {
	parts := strings.Split(name, ".")
	if len(parts) == 1 {
		return n.exported(parts[0])
	}

	if parts[0] == "mtproto" {
		return "mtproto." + n.exported(parts[len(parts)-1])
	}

	if !joinAll {
		return n.exported(parts[len(parts)-1])
	}

	var b strings.Builder
	for _, part := range parts {
		b.WriteString(n.exported(part))
	}
	return b.String()
}

func (n *Namer) exported(name string) string {
	if name == "" {
		return ""
	}
	words := splitWords(name)
	var b strings.Builder
	for _, w := range words {
		b.WriteString(exportWord(w))
	}
	result := b.String()
	if n.isReserved(strings.ToLower(name)) {
		return result + "_"
	}
	return result
}

func (n *Namer) isReserved(name string) bool {
	return n.reserved[name]
}

var initialisms = map[string]string{
	"api":   "API",
	"ascii": "ASCII",
	"cpu":   "CPU",
	"css":   "CSS",
	"dns":   "DNS",
	"eof":   "EOF",
	"guid":  "GUID",
	"html":  "HTML",
	"http":  "HTTP",
	"https": "HTTPS",
	"id":    "ID",
	"ip":    "IP",
	"json":  "JSON",
	"lhs":   "LHS",
	"qps":   "QPS",
	"ram":   "RAM",
	"rhs":   "RHS",
	"rpc":   "RPC",
	"sla":   "SLA",
	"smtp":  "SMTP",
	"sql":   "SQL",
	"ssh":   "SSH",
	"tcp":   "TCP",
	"tls":   "TLS",
	"ttl":   "TTL",
	"udp":   "UDP",
	"ui":    "UI",
	"uid":   "UID",
	"uuid":  "UUID",
	"uri":   "URI",
	"url":   "URL",
	"utf8":  "UTF8",
	"vm":    "VM",
	"xml":   "XML",
}

func exportWord(word string) string {
	if word == "" {
		return ""
	}
	lower := strings.ToLower(word)
	if v, ok := initialisms[lower]; ok {
		return v
	}
	runes := []rune(lower)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func splitWords(name string) []string {
	parts := strings.Split(name, "_")
	var words []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		words = append(words, splitCamel(part)...)
	}
	if len(words) == 0 {
		return []string{name}
	}
	return words
}

func splitCamel(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	start := 0
	var words []string
	for i := 1; i < len(runes); i++ {
		prev := runes[i-1]
		curr := runes[i]
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}

		if isBoundary(prev, curr, next) {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	words = append(words, string(runes[start:]))
	return words
}

func isBoundary(prev, curr, next rune) bool {
	if unicode.IsDigit(curr) && !unicode.IsDigit(prev) {
		return true
	}
	if !unicode.IsDigit(curr) && unicode.IsDigit(prev) {
		return true
	}
	if unicode.IsUpper(curr) && unicode.IsLower(prev) {
		return true
	}
	if unicode.IsUpper(curr) && next != 0 && unicode.IsLower(next) && unicode.IsUpper(prev) {
		return true
	}
	return false
}
