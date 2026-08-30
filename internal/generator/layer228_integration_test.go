package generator

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
	"github.com/stretchr/testify/require"
)

const (
	telegramLayer228Fixture = "telegram_layer_228.tl"
	telegramLayer228        = 228

	// These counts are independently derived below from the exact TL declarations
	// in the resolved fixture, rather than inferred from generated Go output.
	telegramLayer228Constructors = 1655
	telegramLayer228Functions    = 813

	telegramLayer228FixtureSHA256 = "921a58e71f2baebb840609366c506fb92ffac8db3c5bb832423aacd7cd6817cf"
)

func TestIntegration_TelegramLayer228(t *testing.T) {
	fixturePath := filepath.Join("..", "..", "testdata", "schemas", telegramLayer228Fixture)
	fixture, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	// The fixture is a byte-for-byte copy of tgserver/schema/tl/telegram_api.tl.
	// It is intentionally not composed with mtproto.tl: TLRPC owns MTProto wire
	// constructors internally, while generated service packages represent the
	// user-supplied API schema. Including the companion would duplicate framework
	// mechanics in the generated service contract and is not required by this
	// generator. This digest pins the exact API source content and order.
	require.Equal(t, telegramLayer228FixtureSHA256, sha256Hex(fixture))

	sourceConstructors, sourceFunctions := countTLDeclarations(t, fixture)
	require.Equal(t, telegramLayer228Constructors, sourceConstructors)
	require.Equal(t, telegramLayer228Functions, sourceFunctions)

	schema, err := parser.NewParser(string(fixture)).ParseWithLayer(telegramLayer228)
	require.NoError(t, err)
	require.NoError(t, parser.NewValidator(schema).Validate())
	require.Equal(t, telegramLayer228, schema.Layer)
	require.Len(t, schema.Constructors, sourceConstructors)
	require.Len(t, schema.Functions, sourceFunctions)

	moduleDir := t.TempDir()
	firstDir := filepath.Join(moduleDir, "telegram")
	secondDir := filepath.Join(t.TempDir(), "telegram")
	require.NoError(t, generateLayer228Package(firstDir, schema, sha256Hex(fixture)))
	require.NoError(t, generateLayer228Package(secondDir, schema, sha256Hex(fixture)))
	requireGeneratedDirectoriesEqual(t, firstDir, secondDir)

	wantHeader := "// Source: " + telegramLayer228Fixture + "\n" +
		"// Schema SHA-256: " + telegramLayer228FixtureSHA256 + "\n" +
		"// Layer: 228\n"
	entries, err := os.ReadDir(firstDir)
	require.NoError(t, err)
	require.NotEmpty(t, entries)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" {
			continue
		}
		generated, err := os.ReadFile(filepath.Join(firstDir, entry.Name()))
		require.NoError(t, err)
		require.Contains(t, string(generated), wantHeader, "%s has incorrect provenance", entry.Name())
	}
	constants, err := os.ReadFile(filepath.Join(firstDir, "constants.go"))
	require.NoError(t, err)
	require.Contains(t, string(constants), "const SchemaLayer = 228")

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	goMod := fmt.Sprintf(`module example.com/tlrpc-telegram-layer-228

go 1.25

require github.com/r6m/tlrpc v0.0.0

replace github.com/r6m/tlrpc => %s
`, filepath.ToSlash(repoRoot))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o600))

	cmd := exec.Command("go", "build", "./telegram")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod -p=1")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "compile generated layer-228 package:\n%s", output)
}

func sha256Hex(data []byte) string {
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func countTLDeclarations(t *testing.T, source []byte) (constructors, functions int) {
	t.Helper()

	section := "types"
	scanner := bufio.NewScanner(bytes.NewReader(source))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "", "---types---":
			if line != "" {
				section = "types"
			}
			continue
		case "---functions---":
			section = "functions"
			continue
		}
		if strings.HasPrefix(line, "//") {
			continue
		}
		require.True(t, strings.HasSuffix(line, ";"), "TL declaration must remain on one source line: %q", line)
		require.Equal(t, 1, strings.Count(line, ";"), "expected one TL declaration per source line: %q", line)
		if section == "functions" {
			functions++
		} else {
			constructors++
		}
	}
	require.NoError(t, scanner.Err())
	return constructors, functions
}

func generateLayer228Package(outDir string, schema *parser.Schema, digest string) error {
	writer := NewFileWriterWithProvenance(outDir, "telegram", telegramLayer228Fixture, schema.Layer, digest)
	namer := naming.NewNamer()
	typesOut := writer.NewFile("types.go")
	interfacesOut := writer.NewFile("interfaces.go")

	for i := range schema.Types {
		if err := NewTypeGenerator(namer, typesOut, schema).GenerateType(&schema.Types[i]); err != nil {
			return err
		}
		if err := NewTypeGenerator(namer, interfacesOut, schema).GenerateInterface(&schema.Types[i]); err != nil {
			return err
		}
	}
	if err := NewServiceGenerator(namer, schema, writer.NewFile("services.go")).GenerateService(schema.Functions); err != nil {
		return err
	}
	if err := NewServiceGenerator(namer, schema, writer.NewFile("register.go")).GenerateRegistration(schema.Functions); err != nil {
		return err
	}
	if err := NewServiceGenerator(namer, schema, writer.NewFile("requests.go")).GenerateRequests(schema.Functions); err != nil {
		return err
	}
	if err := NewCodecGenerator(namer, writer.NewFile("codec.go")).Generate(schema); err != nil {
		return err
	}
	constants := writer.NewFile("constants.go")
	if err := GenerateSchemaMetadata(constants, schema.Layer); err != nil {
		return err
	}
	if err := GenerateConstructorConstants(namer, constants, schema.Constructors); err != nil {
		return err
	}
	if err := GenerateBaseAliases(writer.NewFile("base_aliases.go")); err != nil {
		return err
	}
	return writer.WriteAll()
}

func requireGeneratedDirectoriesEqual(t *testing.T, firstDir, secondDir string) {
	t.Helper()

	firstEntries, err := os.ReadDir(firstDir)
	require.NoError(t, err)
	secondEntries, err := os.ReadDir(secondDir)
	require.NoError(t, err)
	firstNames := make([]string, 0, len(firstEntries))
	secondNames := make([]string, 0, len(secondEntries))
	for _, entry := range firstEntries {
		firstNames = append(firstNames, entry.Name())
	}
	for _, entry := range secondEntries {
		secondNames = append(secondNames, entry.Name())
	}
	sort.Strings(firstNames)
	sort.Strings(secondNames)
	require.Equal(t, firstNames, secondNames)
	for _, name := range firstNames {
		first, err := os.ReadFile(filepath.Join(firstDir, name))
		require.NoError(t, err)
		second, err := os.ReadFile(filepath.Join(secondDir, name))
		require.NoError(t, err)
		require.Equal(t, first, second, "%s changed across deterministic regeneration", name)
	}
}
