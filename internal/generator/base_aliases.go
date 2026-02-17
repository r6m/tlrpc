package generator

import "io"

// GenerateBaseAliases emits aliases for built-in TL primitive/object types.
func GenerateBaseAliases(out io.Writer) error {
	content := `// Aliases for built-in TL primitives provided by github.com/r6m/tlrpc/types.
type BoolTrue = tltypes.BoolTrue
type BoolFalse = tltypes.BoolFalse
type True = tltypes.True
type Null = tltypes.Null
type Error = tltypes.Error
type String = tltypes.String
type Bytes = tltypes.Bytes
type Int128 = tltypes.Int128
type Int256 = tltypes.Int256
type Double = tltypes.Double
type Vector[T any] = tltypes.Vector[T]
`
	_, err := io.WriteString(out, content)
	return err
}
