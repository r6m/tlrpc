package runtime

import (
	"bytes"
	"compress/gzip"
	"errors"
	"reflect"
	"testing"

	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
)

func TestNormalizeRequestUnwrapsLayerAndConnectionMetadata(t *testing.T) {
	query := constructorBody(0x11223344)
	init := encodeControlBody(t, &mtprototl.InitConnection{
		APIID: 7, DeviceModel: "device", SystemVersion: "system",
		AppVersion: "app", SystemLangCode: "en", LangCode: "en", QueryRaw: query,
	})
	body := encodeControlBody(t, &mtprototl.InvokeWithLayer{Layer: 300, QueryRaw: init})
	request := controlRequest(101, body)
	normalized, mutations, err := NormalizeRequest(request, WrapperConfig{SchemaLayer: 228})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.Message.ConstructorID != 0x11223344 || !bytes.Equal(normalized.Message.Body, query) {
		t.Fatalf("normalized request = %+v", normalized.Message)
	}
	want := []SessionMutation{
		SetLayer{Layer: 228},
		SetClientMetadata{APIID: 7, DeviceModel: "device", SystemVersion: "system", AppVersion: "app", SystemLangCode: "en", LangCode: "en"},
	}
	if !reflect.DeepEqual(mutations, want) {
		t.Fatalf("wrapper mutations = %#v, want %#v", mutations, want)
	}
}

func TestNormalizeRequestBoundsGzipAndWrapperDepth(t *testing.T) {
	query := constructorBody(0x55667788)
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(query); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	body := encodeControlBody(t, &mtprototl.GzipPacked{PackedData: compressed.Bytes()})
	normalized, _, err := NormalizeRequest(controlRequest(105, body), WrapperConfig{MaxDecodedPayload: len(query)})
	if err != nil || !bytes.Equal(normalized.Message.Body, query) {
		t.Fatalf("normalize gzip = %+v, %v", normalized.Message, err)
	}
	if _, _, err := NormalizeRequest(controlRequest(105, body), WrapperConfig{MaxDecodedPayload: len(query) - 1}); !errors.Is(err, mtproto.ErrDecodedPayloadTooLarge) {
		t.Fatalf("oversized gzip error = %v", err)
	}

	wrapped := encodeControlBody(t, &mtprototl.InvokeAfterMsg{MsgID: 1, QueryRaw: query})
	if normalized, _, err := NormalizeRequest(controlRequest(109, wrapped), WrapperConfig{MaxDepth: 1}); err != nil || !bytes.Equal(normalized.Message.Body, query) {
		t.Fatalf("exact wrapper depth rejected: %v", err)
	}
	doubleWrapped := encodeControlBody(t, &mtprototl.InvokeWithoutUpdates{QueryRaw: wrapped})
	if _, _, err := NormalizeRequest(controlRequest(109, doubleWrapped), WrapperConfig{MaxDepth: 1}); !errors.Is(err, ErrWrapperDepth) {
		t.Fatalf("wrapper depth error = %v", err)
	}
}

func TestNormalizeRequestSuppressesOnlyPushIntents(t *testing.T) {
	query := constructorBody(0x99aabbcc)
	body := encodeControlBody(t, &mtprototl.InvokeWithoutUpdates{QueryRaw: query})
	normalized, _, err := NormalizeRequest(controlRequest(113, body), WrapperConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !normalized.Message.SuppressPush {
		t.Fatal("invokeWithoutUpdates did not mark push suppression")
	}
	intents := suppressPushIntents([]Intent{
		Push{Body: constructorBody(1)},
		RPCResult{RequestMessageID: 1, Body: constructorBody(2)},
		Batch{Items: []Intent{Push{Body: constructorBody(3)}, Acknowledge{MessageIDs: []int64{4}}}},
	})
	if len(intents) != 2 {
		t.Fatalf("filtered intents = %#v", intents)
	}
	if _, ok := intents[0].(RPCResult); !ok {
		t.Fatalf("first retained intent = %T", intents[0])
	}
	batch := intents[1].(Batch)
	if len(batch.Items) != 1 {
		t.Fatalf("filtered batch = %#v", batch)
	}
}

func TestNormalizeRequestPreservesInvokeAfterDependencies(t *testing.T) {
	query := constructorBody(0x22334455)
	layer := encodeControlBody(t, &mtprototl.InvokeWithLayer{Layer: 228, QueryRaw: query})
	body := encodeControlBody(t, &mtprototl.InvokeAfterMsgs{MsgIDs: []int64{81, 85, 81}, QueryRaw: layer})
	normalized, _, err := NormalizeRequest(controlRequest(101, body), WrapperConfig{SchemaLayer: 228})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(normalized.Message.Dependencies, []int64{81, 85}) {
		t.Fatalf("dependencies = %v", normalized.Message.Dependencies)
	}
	if normalized.Message.ConstructorID != 0x22334455 {
		t.Fatalf("constructor = 0x%08x", normalized.Message.ConstructorID)
	}
}

func TestNormalizeRequestRejectsUnsafeInvokeAfterDependencies(t *testing.T) {
	query := constructorBody(0x22334455)
	for name, body := range map[string][]byte{
		"self":   encodeControlBody(t, &mtprototl.InvokeAfterMsg{MsgID: 101, QueryRaw: query}),
		"future": encodeControlBody(t, &mtprototl.InvokeAfterMsg{MsgID: 105, QueryRaw: query}),
		"empty":  encodeControlBody(t, &mtprototl.InvokeAfterMsgs{QueryRaw: query}),
		"nested": encodeControlBody(t, &mtprototl.InvokeWithLayer{Layer: 228, QueryRaw: encodeControlBody(t, &mtprototl.InvokeAfterMsg{MsgID: 81, QueryRaw: query})}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := NormalizeRequest(controlRequest(101, body), WrapperConfig{SchemaLayer: 228}); !errors.Is(err, ErrInvalidInvokeAfter) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
