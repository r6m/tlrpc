package protocol

import (
	"errors"
	"sync"
	"testing"
	"time"
)

const (
	testNowSeconds = int64(1_000)
	testSalt       = int64(7)
	testSession    = int64(8)
)

func TestNewValidatorValidatesAndCopiesConfig(t *testing.T) {
	recent := []int64{clientMessageID(testNowSeconds, 4), clientMessageID(testNowSeconds, 8), clientMessageID(testNowSeconds, 12)}
	validator, err := NewValidator(Config{
		SessionID:            testSession,
		ServerSalt:           testSalt,
		SequenceNo:           2,
		RecentMessageIDs:     recent,
		RecentMessageIDLimit: 2,
		Clock:                fixedClock,
	})
	if err != nil {
		t.Fatal(err)
	}
	recent[2] = 0

	snapshot := validator.Snapshot()
	if snapshot.SessionID != testSession || snapshot.ServerSalt != testSalt || snapshot.SequenceNo != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	wantRecent := []int64{clientMessageID(testNowSeconds, 8), clientMessageID(testNowSeconds, 12)}
	assertIDs(t, snapshot.RecentMessageIDs, wantRecent)
	if snapshot.HighestMessageID != wantRecent[1] {
		t.Fatalf("highest message id = %d, want %d", snapshot.HighestMessageID, wantRecent[1])
	}

	for _, config := range []Config{
		{RecentMessageIDLimit: -1},
		{RecentMessageIDLimit: MaxRecentMessageIDLimit + 1},
		{ContentSequenceLimit: -1},
		{ContentSequenceLimit: MaxContentSequenceLimit + 1},
		{SequenceNo: -2},
		{SequenceNo: 1},
	} {
		if _, err := NewValidator(config); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewValidator(%+v) error = %v, want ErrInvalidConfig", config, err)
		}
	}
}

func TestValidatorBindsAndValidatesSessionID(t *testing.T) {
	validator := newTestValidator(t, Config{})
	first := contentMessage(clientMessageID(testNowSeconds, 4), 1)
	if err := validator.Validate(first); err != nil {
		t.Fatal(err)
	}
	if got := validator.Snapshot().SessionID; got != testSession {
		t.Fatalf("bound session id = %d, want %d", got, testSession)
	}

	wrong := contentMessage(clientMessageID(testNowSeconds, 8), 3)
	wrong.SessionID++
	err := validator.Validate(wrong)
	_ = assertBadMessage(t, err, ErrSessionIDMismatch, CodeSessionIDMismatch, wrong.MessageID, wrong.SequenceNo)
	assertSnapshot(t, validator.Snapshot(), Snapshot{
		SessionID:        testSession,
		ServerSalt:       testSalt,
		SequenceNo:       2,
		HighestMessageID: first.MessageID,
		RecentMessageIDs: []int64{first.MessageID},
	})

	zeroSession := contentMessage(clientMessageID(testNowSeconds, 12), 3)
	zeroSession.SessionID = 0
	_ = assertBadMessage(t, validator.Validate(zeroSession), ErrSessionIDMismatch, CodeSessionIDMismatch, zeroSession.MessageID, zeroSession.SequenceNo)
}

func TestValidatorReportsBadServerSalt(t *testing.T) {
	validator := newTestValidator(t, Config{SessionID: testSession})
	message := contentMessage(clientMessageID(testNowSeconds, 4), 1)
	message.ServerSalt = 99
	err := validator.Validate(message)
	bad := assertBadMessage(t, err, ErrBadServerSalt, CodeBadServerSalt, message.MessageID, message.SequenceNo)
	if bad.ExpectedServerSalt != testSalt {
		t.Fatalf("expected server salt = %d, want %d", bad.ExpectedServerSalt, testSalt)
	}
	if got := validator.Snapshot().RecentMessageIDs; len(got) != 0 {
		t.Fatalf("invalid salt mutated replay state: %v", got)
	}

	validator.SetServerSalt(99)
	if err := validator.Validate(message); err != nil {
		t.Fatalf("message with updated salt: %v", err)
	}
}

func TestValidatorMessageIDFormatAndTimeWindow(t *testing.T) {
	tests := []struct {
		name string
		id   int64
		want error
		code int32
	}{
		{name: "zero low word", id: testNowSeconds << 32, want: ErrMessageIDFormat, code: CodeMessageIDFormat},
		{name: "not divisible by four", id: clientMessageID(testNowSeconds, 2), want: ErrMessageIDFormat, code: CodeMessageIDFormat},
		{name: "too old", id: clientMessageID(testNowSeconds-301, 4), want: ErrMessageIDTooLow, code: CodeMessageIDTooLow},
		{name: "too far ahead", id: clientMessageID(testNowSeconds+31, 4), want: ErrMessageIDTooHigh, code: CodeMessageIDTooHigh},
		{name: "past boundary", id: clientMessageID(testNowSeconds-300, 4)},
		{name: "future boundary", id: clientMessageID(testNowSeconds+30, 4)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := newTestValidator(t, Config{SessionID: testSession})
			message := contentMessage(test.id, 1)
			err := validator.Validate(message)
			if test.want == nil {
				if err != nil {
					t.Fatalf("Validate returned %v", err)
				}
				return
			}
			_ = assertBadMessage(t, err, test.want, test.code, message.MessageID, message.SequenceNo)
			if got := validator.Snapshot().RecentMessageIDs; len(got) != 0 {
				t.Fatalf("invalid message id mutated state: %v", got)
			}
		})
	}
}

func TestValidatorRejectsRecentReplayAndBoundsWindow(t *testing.T) {
	validator := newTestValidator(t, Config{SessionID: testSession, RecentMessageIDLimit: 3})
	ids := []int64{
		clientMessageID(testNowSeconds, 4),
		clientMessageID(testNowSeconds, 8),
		clientMessageID(testNowSeconds, 12),
		clientMessageID(testNowSeconds, 16),
	}
	for index, id := range ids[:3] {
		if err := validator.Validate(contentMessage(id, int32(index*2+1))); err != nil {
			t.Fatal(err)
		}
	}
	replay := contentMessage(ids[1], 3)
	_ = assertBadMessage(t, validator.Validate(replay), ErrReplayMessageID, CodeReplayMessageID, replay.MessageID, replay.SequenceNo)

	if err := validator.Validate(contentMessage(ids[3], 7)); err != nil {
		t.Fatal(err)
	}
	assertIDs(t, validator.Snapshot().RecentMessageIDs, ids[1:])

	// Eviction advances a durable floor, so bounded storage never turns an old
	// accepted request back into an executable request.
	replay = contentMessage(ids[0], 1)
	_ = assertBadMessage(t, validator.Validate(replay), ErrReplayMessageID, CodeReplayMessageID, replay.MessageID, replay.SequenceNo)
	if got := validator.Snapshot().MessageIDFloor; got != ids[0] {
		t.Fatalf("message ID floor = %d, want %d", got, ids[0])
	}
}

func TestValidatorRejectsFullWindowReplayAfterMoreThan64LaterIDsAcrossRestart(t *testing.T) {
	validator := newTestValidator(t, Config{SessionID: testSession})
	firstID := clientMessageID(testNowSeconds, 4)
	for index := 0; index < DefaultRecentMessageIDLimit+1; index++ {
		messageID := clientMessageID(testNowSeconds, uint32((index+1)*4))
		if err := validator.Validate(contentMessage(messageID, int32(index*2+1))); err != nil {
			t.Fatalf("validate message %d: %v", index, err)
		}
	}
	state := validator.Snapshot()
	if state.MessageIDFloor != firstID {
		t.Fatalf("message ID floor = %d, want %d", state.MessageIDFloor, firstID)
	}
	restored := newTestValidator(t, Config{
		SessionID: testSession, SequenceNo: state.SequenceNo,
		HighestMessageID: state.HighestMessageID, MessageIDFloor: state.MessageIDFloor,
		RecentMessageIDs: state.RecentMessageIDs, RecentSequenceNos: state.RecentSequenceNos,
	})
	replay := contentMessage(firstID, 1)
	_ = assertBadMessage(t, restored.Validate(replay), ErrReplayMessageID, CodeReplayMessageID, replay.MessageID, replay.SequenceNo)
}

func TestValidatorRejectsRestoredHighWaterMessageID(t *testing.T) {
	id := clientMessageID(testNowSeconds, 4)
	validator := newTestValidator(t, Config{SessionID: testSession, HighestMessageID: id})
	message := contentMessage(id, 1)
	_ = assertBadMessage(t, validator.Validate(message), ErrReplayMessageID, CodeReplayMessageID, id, 1)
}

func TestValidatorSequenceNumberRules(t *testing.T) {
	tests := []struct {
		name       string
		kind       MessageKind
		sequenceNo int32
		want       error
		code       int32
		wantState  int32
	}{
		{name: "expected content", kind: ContentRelated, sequenceNo: 3, wantState: 4},
		{name: "out of order higher content", kind: ContentRelated, sequenceNo: 7, wantState: 8},
		{name: "out of order lower content", kind: ContentRelated, sequenceNo: 1, wantState: 2},
		{name: "content must be odd", kind: ContentRelated, sequenceNo: 2, want: ErrExpectedContentSequence, code: CodeExpectedOddSequenceNo, wantState: 2},
		{name: "expected non-content", kind: NonContentRelated, sequenceNo: 2, wantState: 2},
		{name: "late non-content", kind: NonContentRelated, sequenceNo: 0, wantState: 2},
		{name: "future non-content", kind: NonContentRelated, sequenceNo: 8, wantState: 2},
		{name: "non-content must be even", kind: NonContentRelated, sequenceNo: 3, want: ErrExpectedNonContentSequence, code: CodeExpectedEvenSequenceNo, wantState: 2},
		{name: "negative", kind: NonContentRelated, sequenceNo: -2, want: ErrSequenceNoTooLow, code: CodeSequenceNoTooLow, wantState: 2},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := newTestValidator(t, Config{SessionID: testSession, SequenceNo: 2})
			message := Message{
				ServerSalt: testSalt,
				SessionID:  testSession,
				MessageID:  clientMessageID(testNowSeconds, uint32((index+1)*4)),
				SequenceNo: test.sequenceNo,
				Kind:       test.kind,
			}
			err := validator.Validate(message)
			if test.want == nil {
				if err != nil {
					t.Fatalf("Validate returned %v", err)
				}
			} else {
				_ = assertBadMessage(t, err, test.want, test.code, message.MessageID, message.SequenceNo)
			}
			if got := validator.Snapshot().SequenceNo; got != test.wantState {
				t.Fatalf("sequence state = %d, want %d", got, test.wantState)
			}
		})
	}
}

func TestValidatorRejectsMateriallyStaleAndArbitraryContentSequenceNumbers(t *testing.T) {
	validator := newTestValidator(t, Config{SessionID: testSession, SequenceNo: 200, ContentSequenceLimit: 8})
	if err := validator.Validate(contentMessage(clientMessageID(testNowSeconds, 4), 199)); err != nil {
		t.Fatalf("valid reordered sequence: %v", err)
	}
	duplicateSequence := contentMessage(clientMessageID(testNowSeconds, 8), 199)
	_ = assertBadMessage(t, validator.Validate(duplicateSequence), ErrSequenceNoTooLow, CodeSequenceNoTooLow, duplicateSequence.MessageID, duplicateSequence.SequenceNo)
	stale := contentMessage(clientMessageID(testNowSeconds, 12), 183)
	_ = assertBadMessage(t, validator.Validate(stale), ErrSequenceNoTooLow, CodeSequenceNoTooLow, stale.MessageID, stale.SequenceNo)
	farAhead := contentMessage(clientMessageID(testNowSeconds, 16), 219)
	_ = assertBadMessage(t, validator.Validate(farAhead), ErrSequenceNoTooHigh, CodeSequenceNoTooHigh, farAhead.MessageID, farAhead.SequenceNo)
}

func TestValidatorRejectsContentSequenceThatWouldOverflowDurableState(t *testing.T) {
	validator := newTestValidator(t, Config{SessionID: testSession})
	message := contentMessage(clientMessageID(testNowSeconds, 4), int32(1<<31-1))
	_ = assertBadMessage(t, validator.Validate(message), ErrSequenceNoTooHigh, CodeSequenceNoTooHigh, message.MessageID, message.SequenceNo)
	if got := validator.Snapshot().SequenceNo; got != 0 {
		t.Fatalf("overflowing sequence mutated state to %d", got)
	}
}

func TestValidatorValidatesContainerAndChildrenAtomically(t *testing.T) {
	validator := newTestValidator(t, Config{SessionID: testSession})
	outerID := clientMessageID(testNowSeconds, 20)
	message := Message{
		ServerSalt: testSalt,
		SessionID:  testSession,
		MessageID:  outerID,
		SequenceNo: 4,
		Kind:       Container,
		Children: []ContainerMessage{
			{MessageID: clientMessageID(testNowSeconds, 4), SequenceNo: 1, Kind: ContentRelated},
			{MessageID: clientMessageID(testNowSeconds, 8), SequenceNo: 2, Kind: NonContentRelated},
			{MessageID: clientMessageID(testNowSeconds, 12), SequenceNo: 3, Kind: ContentRelated},
		},
	}
	if err := validator.Validate(message); err != nil {
		t.Fatal(err)
	}
	snapshot := validator.Snapshot()
	if snapshot.SequenceNo != 4 || snapshot.HighestMessageID != outerID {
		t.Fatalf("container snapshot = %+v", snapshot)
	}
	assertIDs(t, snapshot.RecentMessageIDs, []int64{outerID, message.Children[0].MessageID, message.Children[1].MessageID, message.Children[2].MessageID})

	badOuter := message
	badOuter.MessageID = clientMessageID(testNowSeconds, 24)
	badOuter.SequenceNo = 5
	_ = assertBadMessage(t, validator.Validate(badOuter), ErrExpectedNonContentSequence, CodeExpectedEvenSequenceNo, badOuter.MessageID, badOuter.SequenceNo)

	before := validator.Snapshot()
	badChild := Message{
		ServerSalt: testSalt,
		SessionID:  testSession,
		MessageID:  clientMessageID(testNowSeconds, 32),
		SequenceNo: 6,
		Kind:       Container,
		Children: []ContainerMessage{
			{MessageID: clientMessageID(testNowSeconds, 24), SequenceNo: 5, Kind: ContentRelated},
			{MessageID: clientMessageID(testNowSeconds, 28), SequenceNo: 6, Kind: ContentRelated},
		},
	}
	_ = assertBadMessage(t, validator.Validate(badChild), ErrExpectedContentSequence, CodeExpectedOddSequenceNo, badChild.Children[1].MessageID, badChild.Children[1].SequenceNo)
	assertSnapshot(t, validator.Snapshot(), before)
}

func TestValidatorRejectsDuplicateWithinContainerAtomically(t *testing.T) {
	validator := newTestValidator(t, Config{SessionID: testSession})
	id := clientMessageID(testNowSeconds, 4)
	message := Message{
		ServerSalt: testSalt,
		SessionID:  testSession,
		MessageID:  clientMessageID(testNowSeconds, 12),
		SequenceNo: 2,
		Kind:       Container,
		Children: []ContainerMessage{
			{MessageID: id, SequenceNo: 1, Kind: ContentRelated},
			{MessageID: id, SequenceNo: 1, Kind: ContentRelated},
		},
	}
	_ = assertBadMessage(t, validator.Validate(message), ErrReplayMessageID, CodeReplayMessageID, id, 1)
	assertSnapshot(t, validator.Snapshot(), Snapshot{SessionID: testSession, ServerSalt: testSalt})
}

func TestValidatorRejectsInvalidMessageClassificationWithoutMutation(t *testing.T) {
	validator := newTestValidator(t, Config{SessionID: testSession})
	message := contentMessage(clientMessageID(testNowSeconds, 4), 1)
	message.Children = []ContainerMessage{{MessageID: clientMessageID(testNowSeconds, 8), Kind: ContentRelated}}
	if err := validator.Validate(message); !errors.Is(err, ErrInvalidMessageKind) {
		t.Fatalf("error = %v, want ErrInvalidMessageKind", err)
	}
	assertSnapshot(t, validator.Snapshot(), Snapshot{SessionID: testSession, ServerSalt: testSalt})
}

func TestValidatorConcurrentUse(t *testing.T) {
	const count = 256
	validator := newTestValidator(t, Config{SessionID: testSession, RecentMessageIDLimit: count, ContentSequenceLimit: count})
	start := make(chan struct{})
	errorsFound := make(chan error, count)
	var wait sync.WaitGroup
	for index := 0; index < count; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			message := contentMessage(clientMessageID(testNowSeconds, uint32((index+1)*4)), int32(index*2+1))
			if err := validator.Validate(message); err != nil {
				errorsFound <- err
			}
			_ = validator.Snapshot()
		}()
	}
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent Validate returned %v", err)
	}
	snapshot := validator.Snapshot()
	if snapshot.SequenceNo != count*2 || len(snapshot.RecentMessageIDs) != count {
		t.Fatalf("concurrent snapshot = sequence %d recent %d", snapshot.SequenceNo, len(snapshot.RecentMessageIDs))
	}

	copyOfRecent := snapshot.RecentMessageIDs
	copyOfRecent[0] = 0
	if validator.Snapshot().RecentMessageIDs[0] == 0 {
		t.Fatal("Snapshot exposed internal replay storage")
	}
}

func newTestValidator(t *testing.T, config Config) *Validator {
	t.Helper()
	config.ServerSalt = testSalt
	if config.Clock == nil {
		config.Clock = fixedClock
	}
	validator, err := NewValidator(config)
	if err != nil {
		t.Fatal(err)
	}
	return validator
}

func fixedClock() time.Time { return time.Unix(testNowSeconds, 0) }

func contentMessage(messageID int64, sequenceNo int32) Message {
	return Message{
		ServerSalt: testSalt,
		SessionID:  testSession,
		MessageID:  messageID,
		SequenceNo: sequenceNo,
		Kind:       ContentRelated,
	}
}

func clientMessageID(seconds int64, fraction uint32) int64 {
	return seconds<<32 | int64(fraction)
}

func assertBadMessage(t *testing.T, err, cause error, code int32, messageID int64, sequenceNo int32) *BadMessageError {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want cause %v", err, cause)
	}
	var bad *BadMessageError
	if !errors.As(err, &bad) {
		t.Fatalf("error type = %T, want *BadMessageError", err)
	}
	if bad.Code != code || bad.MessageID != messageID || bad.SequenceNo != sequenceNo {
		t.Fatalf("bad message = %+v, want id=%d seq=%d code=%d", bad, messageID, sequenceNo, code)
	}
	return bad
}

func assertIDs(t *testing.T, got, want []int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("message ids = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("message ids = %v, want %v", got, want)
		}
	}
}

func assertSnapshot(t *testing.T, got, want Snapshot) {
	t.Helper()
	if got.SessionID != want.SessionID || got.ServerSalt != want.ServerSalt || got.SequenceNo != want.SequenceNo || got.HighestMessageID != want.HighestMessageID {
		t.Fatalf("snapshot = %+v, want %+v", got, want)
	}
	assertIDs(t, got.RecentMessageIDs, want.RecentMessageIDs)
}
