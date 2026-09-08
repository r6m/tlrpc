package runtime

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

func TestConnectionNonSubscribingMethodUsesNormalizedConstructor(t *testing.T) {
	const (
		methodConstructor = uint32(0x18181818)
		responseBody      = uint32(0x19191919)
		incidentalPush    = uint32(0x20202020)
	)
	now := time.Unix(inboundNowSeconds, 0).UTC()
	requestID := inboundMessageID(4)
	application := &connectionApplicationStub{
		outcome: Outcome{
			Intents:   []Intent{RPCResult{RequestMessageID: requestID, Body: constructorBody(responseBody)}},
			Mutations: []SessionMutation{BindUser{UserID: 42}},
		},
		pushBody: constructorBody(incidentalPush),
	}
	presence := newSubscriptionPresenceStub()
	harness := newConnectionHarness(t, now, application, 10, nil)
	harness.connection.config.Presence = presence
	harness.connection.config.NonSubscribingMethods = map[uint32]struct{}{methodConstructor: {}}

	wrapped := encodeControlBody(t, &mtprototl.InvokeWithLayer{
		Layer: 228,
		QueryRaw: encodeControlBody(t, &mtprototl.InitConnection{
			APIID: 1, DeviceModel: "test", SystemVersion: "test", AppVersion: "test",
			SystemLangCode: "en", LangPack: "", LangCode: "en",
			QueryRaw: constructorBody(methodConstructor),
		}),
	})
	if err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
		Encrypted: &mtproto.InnerData{
			Salt: inboundSalt, SessionID: inboundSessionID,
			MsgID: requestID, SeqNo: 1, Data: wrapped,
		},
		AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
	}); err != nil {
		t.Fatalf("handle wrapped non-subscribing method: %v", err)
	}
	waitForWrittenFrames(t, harness.transport, 2)

	if application.request.Message.ConstructorID != methodConstructor || !application.request.Message.SuppressPush {
		t.Fatalf("normalized request = %+v, want constructor 0x%08x with push suppressed", application.request.Message, methodConstructor)
	}
	constructors := make([]uint32, 0, 3)
	for _, frame := range harness.transport.writtenFrames() {
		constructors = append(constructors, binaryConstructor(decryptWriterFrame(t, harness.authKey, frame).Data))
	}
	if containsConstructor(constructors, incidentalPush) {
		t.Fatalf("request-scoped sender push escaped method policy: %08x", constructors)
	}
	snapshot := loadConnectionSessionSnapshot(t, harness.store, harness.authKey.ID(), inboundSessionID)
	if snapshot.UserID != 42 || snapshot.PushSubscription {
		t.Fatalf("cold session snapshot = %+v, want bound without push subscription", snapshot)
	}
	if err := presence.publish(context.Background(), 42, constructorBody(0x21212121)); err == nil {
		t.Fatal("cold non-subscribing method made session push-reachable")
	}
	harness.connection.shutdown(io.EOF)
}

func TestConnectionNonSubscribingMethodKeepsExistingSubscription(t *testing.T) {
	const (
		bindConstructor       = uint32(0x28282828)
		nonSubscribingMethod  = uint32(0x29292929)
		incidentalSenderPush  = uint32(0x30303030)
		incidentalOutcomePush = uint32(0x31313131)
		laterServerPush       = uint32(0x32323232)
	)
	now := time.Unix(inboundNowSeconds, 0).UTC()
	application := &subscriptionApplicationStub{
		bindConstructor:       bindConstructor,
		suppressedConstructor: nonSubscribingMethod,
		incidentalSenderPush:  incidentalSenderPush,
		incidentalOutcomePush: incidentalOutcomePush,
	}
	presence := newSubscriptionPresenceStub()
	harness := newConnectionHarness(t, now, application, 20, nil)
	harness.connection.config.Presence = presence
	harness.connection.config.NonSubscribingMethods = map[uint32]struct{}{nonSubscribingMethod: {}}

	handle := func(messageID int64, sequence int32, constructorID uint32) {
		t.Helper()
		if err := harness.connection.handleEncrypted(context.Background(), DecodedFrame{
			Encrypted: &mtproto.InnerData{
				Salt: inboundSalt, SessionID: inboundSessionID,
				MsgID: messageID, SeqNo: sequence, Data: constructorBody(constructorID),
			},
			AuthKeyID: harness.authKey.ID(), AuthKey: harness.authKey,
		}); err != nil {
			t.Fatalf("handle method 0x%08x: %v", constructorID, err)
		}
	}
	handle(inboundMessageID(4), 1, bindConstructor)
	presence.waitForUser(t, 42)
	waitForWrittenFrames(t, harness.transport, 2)
	handle(inboundMessageID(8), 3, nonSubscribingMethod)
	waitForWrittenFrames(t, harness.transport, 3)

	if err := presence.publish(context.Background(), 42, constructorBody(laterServerPush)); err != nil {
		t.Fatalf("publish through existing subscription: %v", err)
	}
	waitForWrittenFrames(t, harness.transport, 4)
	constructors := make([]uint32, 0, 6)
	for _, frame := range harness.transport.writtenFrames() {
		constructors = append(constructors, binaryConstructor(decryptWriterFrame(t, harness.authKey, frame).Data))
	}
	if containsConstructor(constructors, incidentalSenderPush) || containsConstructor(constructors, incidentalOutcomePush) {
		t.Fatalf("method policy leaked request-scoped pushes: %08x", constructors)
	}
	if !containsConstructor(constructors, laterServerPush) {
		t.Fatalf("method policy revoked existing subscription: %08x", constructors)
	}
	snapshot, err := harness.store.Load(context.Background(), session.SessionKey{AuthKeyID: harness.authKey.ID(), SessionID: inboundSessionID})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.PushSubscription {
		t.Fatalf("method policy cleared durable subscription: %+v", snapshot)
	}
	harness.connection.shutdown(io.EOF)
}
