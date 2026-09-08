package tl

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
)

func TestInitConnectionDecodesIndependentClientOptions(t *testing.T) {
	query := &tg.HelpGetNearestDCRequest{}
	request := &tg.InitConnectionRequest{APIID: 77777, DeviceModel: "Android", SystemVersion: "16", AppVersion: "1", SystemLangCode: "en", LangPack: "android", LangCode: "en", Query: query}
	request.SetProxy(tg.InputClientProxy{Address: "127.0.0.1", Port: 443})
	request.SetParams(&tg.JSONObject{Value: []tg.JSONObjectValue{
		{Key: "tz_offset", Value: &tg.JSONNumber{Value: 12600}},
		{Key: "package_id", Value: &tg.JSONString{Value: "org.alooo.test"}},
		{Key: "nested", Value: &tg.JSONArray{Value: []tg.JSONValueClass{&tg.JSONBool{Value: true}, &tg.JSONNull{}}}},
	}})
	b := &bin.Buffer{}
	if err := request.Encode(b); err != nil {
		t.Fatal(err)
	}
	var decoded InitConnection
	if err := decoded.DeserializeTL(bytes.NewReader(b.Buf)); err != nil {
		t.Fatal(err)
	}
	if decoded.Proxy == nil || decoded.Proxy.Port != 443 || decoded.Params == nil || len(decoded.Params.Object) != 3 {
		t.Fatalf("options not decoded: %#v", decoded)
	}
	if len(decoded.QueryRaw) != 4 || binary.LittleEndian.Uint32(decoded.QueryRaw) != query.TypeID() {
		t.Fatalf("nested query shifted: %x", decoded.QueryRaw)
	}
	var encoded bytes.Buffer
	if err := decoded.SerializeTL(&encoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded.Bytes(), b.Buf) {
		t.Fatal("wire options differ from independent client")
	}
	for i := 4; i < len(b.Buf)-4; i++ {
		if err := new(InitConnection).DeserializeTL(bytes.NewReader(b.Buf[:i])); err == nil {
			t.Fatalf("truncated options accepted at %d", i)
		}
	}
}

func TestInitConnectionOptionsBoundDepthAndCounts(t *testing.T) {
	for _, count := range []uint32{1025, 0xffffffff, 0x7fffffff} {
		body := make([]byte, 12)
		binary.LittleEndian.PutUint32(body, JSONArrayID)
		binary.LittleEndian.PutUint32(body[4:], 0x1cb5c415)
		binary.LittleEndian.PutUint32(body[8:], count)
		nodes := 0
		if err := new(JSONValue).read(bytes.NewReader(body), 0, &nodes); err == nil {
			t.Fatalf("accepted count %d", count)
		}
	}
	var deep bytes.Buffer
	for i := 0; i < 18; i++ {
		for _, v := range []uint32{JSONArrayID, 0x1cb5c415, 1} {
			_ = binary.Write(&deep, binary.LittleEndian, v)
		}
	}
	_ = binary.Write(&deep, binary.LittleEndian, JSONNullID)
	nodes := 0
	if err := new(JSONValue).read(bytes.NewReader(deep.Bytes()), 0, &nodes); err == nil {
		t.Fatal("unbounded JSON nesting accepted")
	}
}
