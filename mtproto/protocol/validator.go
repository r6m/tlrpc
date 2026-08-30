package protocol

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	// Canonical MTProto bad_msg_notification and bad_server_salt codes.
	CodeMessageIDTooLow        int32 = 16
	CodeMessageIDTooHigh       int32 = 17
	CodeMessageIDFormat        int32 = 18
	CodeSequenceNoTooLow       int32 = 32
	CodeSequenceNoTooHigh      int32 = 33
	CodeExpectedEvenSequenceNo int32 = 34
	CodeExpectedOddSequenceNo  int32 = 35
	CodeBadServerSalt          int32 = 48
	CodeInvalidContainer       int32 = 64
	// CodeReplayMessageID and CodeSessionIDMismatch name the compatibility
	// mappings used by TLRPC for duplicate and mismatched-session messages.
	CodeReplayMessageID   = CodeSequenceNoTooLow
	CodeSessionIDMismatch = CodeInvalidContainer

	DefaultRecentMessageIDLimit = 64
	MaxRecentMessageIDLimit     = 4096
)

const (
	clientMessagePastWindow   = 300 * time.Second
	clientMessageFutureWindow = 30 * time.Second
)

var (
	ErrSessionIDMismatch          = errors.New("mtproto protocol: session id mismatch")
	ErrBadServerSalt              = errors.New("mtproto protocol: bad server salt")
	ErrBadMessageID               = errors.New("mtproto protocol: bad message id")
	ErrMessageIDTooLow            = fmt.Errorf("%w: too low", ErrBadMessageID)
	ErrMessageIDTooHigh           = fmt.Errorf("%w: too high", ErrBadMessageID)
	ErrMessageIDFormat            = fmt.Errorf("%w: invalid client format", ErrBadMessageID)
	ErrReplayMessageID            = fmt.Errorf("%w: replayed", ErrBadMessageID)
	ErrBadSequenceNo              = errors.New("mtproto protocol: bad sequence number")
	ErrSequenceNoTooLow           = fmt.Errorf("%w: too low", ErrBadSequenceNo)
	ErrSequenceNoTooHigh          = fmt.Errorf("%w: too high", ErrBadSequenceNo)
	ErrExpectedNonContentSequence = fmt.Errorf("%w: expected non-content-related message", ErrBadSequenceNo)
	ErrExpectedContentSequence    = fmt.Errorf("%w: expected content-related message", ErrBadSequenceNo)
	ErrInvalidMessageKind         = errors.New("mtproto protocol: invalid message kind")
	ErrInvalidConfig              = errors.New("mtproto protocol: invalid validator config")
)

// MessageKind describes the sequence-number behavior of a decoded TL body.
// Classifying a body is intentionally left to the caller so this package stays
// independent of any generated or hand-written TL types.
type MessageKind uint8

const (
	ContentRelated MessageKind = iota + 1
	NonContentRelated
	Container
)

// ContainerMessage is one inner record of an MTProto msg_container. Inner
// records inherit the outer encrypted envelope's salt and session ID.
type ContainerMessage struct {
	MessageID  int64
	SequenceNo int32
	Kind       MessageKind
}

// Message contains the authenticated metadata needed for protocol validation.
// Children are required only for Kind == Container and are validated in wire
// order. Nested containers are rejected as invalid caller input.
type Message struct {
	ServerSalt int64
	SessionID  int64
	MessageID  int64
	SequenceNo int32
	Kind       MessageKind
	Children   []ContainerMessage
}

// Config initializes one session validator. SessionID may be zero to bind the
// validator to the first valid message's non-zero session ID. SequenceNo is the
// next expected even client sequence base and must therefore be non-negative
// and even. RecentMessageIDs is useful when restoring persisted state.
type Config struct {
	SessionID            int64
	ServerSalt           int64
	SequenceNo           int32
	HighestMessageID     int64
	RecentMessageIDs     []int64
	RecentMessageIDLimit int
	Clock                func() time.Time
}

// Snapshot is a race-safe copy of the validator's current protocol state.
type Snapshot struct {
	SessionID        int64
	ServerSalt       int64
	SequenceNo       int32
	HighestMessageID int64
	RecentMessageIDs []int64
}

// BadMessageError maps a validation failure to an MTProto protocol response.
// Code 48 is encoded as bad_server_salt using ExpectedServerSalt; the other
// codes are encoded as bad_msg_notification.
type BadMessageError struct {
	MessageID          int64
	SequenceNo         int32
	Code               int32
	ExpectedServerSalt int64
	Cause              error
}

func (e *BadMessageError) Error() string {
	return fmt.Sprintf("mtproto protocol: bad message id=%d seqno=%d code=%d: %v", e.MessageID, e.SequenceNo, e.Code, e.Cause)
}

func (e *BadMessageError) Unwrap() error { return e.Cause }

// Validator owns the bounded receive state for one MTProto session. Its
// methods are safe for concurrent use.
type Validator struct {
	mu    sync.Mutex
	clock func() time.Time
	limit int
	state validatorState
}

type validatorState struct {
	sessionID        int64
	serverSalt       int64
	sequenceNo       int32
	highestMessageID int64
	recentIDs        []int64
	recentSet        map[int64]struct{}
}

// NewValidator creates a validator with bounded duplicate-detection state.
func NewValidator(config Config) (*Validator, error) {
	limit := config.RecentMessageIDLimit
	if limit == 0 {
		limit = DefaultRecentMessageIDLimit
	}
	if limit < 1 || limit > MaxRecentMessageIDLimit {
		return nil, fmt.Errorf("%w: recent message id limit must be between 1 and %d", ErrInvalidConfig, MaxRecentMessageIDLimit)
	}
	if config.SequenceNo < 0 || config.SequenceNo&1 != 0 {
		return nil, fmt.Errorf("%w: sequence number must be a non-negative even value", ErrInvalidConfig)
	}
	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}

	recent := config.RecentMessageIDs
	if len(recent) > limit {
		recent = recent[len(recent)-limit:]
	}
	state := validatorState{
		sessionID:        config.SessionID,
		serverSalt:       config.ServerSalt,
		sequenceNo:       config.SequenceNo,
		highestMessageID: config.HighestMessageID,
		recentIDs:        make([]int64, 0, limit),
		recentSet:        make(map[int64]struct{}, len(recent)),
	}
	for _, id := range recent {
		if _, exists := state.recentSet[id]; exists {
			continue
		}
		state.recentIDs = append(state.recentIDs, id)
		state.recentSet[id] = struct{}{}
		if id > state.highestMessageID {
			state.highestMessageID = id
		}
	}

	return &Validator{clock: clock, limit: limit, state: state}, nil
}

// Validate validates message and atomically advances state. For containers,
// the outer record and every child must pass before any state is committed.
func (v *Validator) Validate(message Message) error {
	if v == nil {
		return fmt.Errorf("%w: nil validator", ErrInvalidConfig)
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	candidate := v.state.clone()
	if err := v.validateEnvelope(&candidate, message); err != nil {
		return err
	}
	v.state = candidate
	return nil
}

func (v *Validator) validateEnvelope(state *validatorState, message Message) error {
	metadata := messageMetadata{messageID: message.MessageID, sequenceNo: message.SequenceNo}
	if message.SessionID == 0 || state.sessionID != 0 && message.SessionID != state.sessionID {
		return badMessage(metadata, CodeSessionIDMismatch, state.serverSalt, ErrSessionIDMismatch)
	}
	if message.ServerSalt != state.serverSalt {
		return badMessage(metadata, CodeBadServerSalt, state.serverSalt, ErrBadServerSalt)
	}
	if message.Kind != Container && len(message.Children) != 0 {
		return ErrInvalidMessageKind
	}
	if err := v.validateOne(state, metadata, message.Kind); err != nil {
		return err
	}
	if message.Kind == Container {
		for _, child := range message.Children {
			if child.Kind == Container || !validKind(child.Kind) {
				return ErrInvalidMessageKind
			}
			childMetadata := messageMetadata{messageID: child.MessageID, sequenceNo: child.SequenceNo}
			if err := v.validateOne(state, childMetadata, child.Kind); err != nil {
				return err
			}
		}
	}
	if state.sessionID == 0 {
		state.sessionID = message.SessionID
	}
	return nil
}

type messageMetadata struct {
	messageID  int64
	sequenceNo int32
}

func (v *Validator) validateOne(state *validatorState, metadata messageMetadata, kind MessageKind) error {
	if !validKind(kind) {
		return ErrInvalidMessageKind
	}
	if err := v.validateMessageID(state, metadata); err != nil {
		return err
	}
	if err := validateSequenceNo(state, metadata, kind); err != nil {
		return err
	}
	if metadata.messageID > state.highestMessageID {
		state.highestMessageID = metadata.messageID
	}
	if kind == ContentRelated && metadata.sequenceNo >= state.sequenceNo+1 {
		state.sequenceNo = metadata.sequenceNo + 1
	}
	state.remember(metadata.messageID, v.limit)
	return nil
}

func (v *Validator) validateMessageID(state *validatorState, metadata messageMetadata) error {
	if metadata.messageID%4 != 0 || uint32(metadata.messageID) == 0 {
		return badMessage(metadata, CodeMessageIDFormat, state.serverSalt, ErrMessageIDFormat)
	}
	now := v.clock()
	messageTime := time.Unix(metadata.messageID>>32, 0)
	if messageTime.Before(now.Add(-clientMessagePastWindow)) {
		return badMessage(metadata, CodeMessageIDTooLow, state.serverSalt, ErrMessageIDTooLow)
	}
	if messageTime.After(now.Add(clientMessageFutureWindow)) {
		return badMessage(metadata, CodeMessageIDTooHigh, state.serverSalt, ErrMessageIDTooHigh)
	}
	_, recentReplay := state.recentSet[metadata.messageID]
	if metadata.messageID == state.highestMessageID || recentReplay {
		return badMessage(metadata, CodeReplayMessageID, state.serverSalt, ErrReplayMessageID)
	}
	return nil
}

func validateSequenceNo(state *validatorState, metadata messageMetadata, kind MessageKind) error {
	if metadata.sequenceNo < 0 {
		return badMessage(metadata, CodeSequenceNoTooLow, state.serverSalt, ErrSequenceNoTooLow)
	}
	if kind == Container {
		if metadata.sequenceNo&1 != 0 {
			return badMessage(metadata, CodeExpectedEvenSequenceNo, state.serverSalt, ErrExpectedNonContentSequence)
		}
		return nil
	}

	contentRelated := kind == ContentRelated
	expected := state.sequenceNo
	if contentRelated {
		expected++
	}
	if metadata.sequenceNo == expected {
		return nil
	}
	if !contentRelated && metadata.sequenceNo&1 == 0 && metadata.sequenceNo < expected {
		return nil
	}
	if metadata.sequenceNo&1 != 0 && !contentRelated {
		return badMessage(metadata, CodeExpectedEvenSequenceNo, state.serverSalt, ErrExpectedNonContentSequence)
	}
	if metadata.sequenceNo&1 == 0 && contentRelated {
		return badMessage(metadata, CodeExpectedOddSequenceNo, state.serverSalt, ErrExpectedContentSequence)
	}
	// Content-related requests can be delivered out of order by concurrent
	// client senders. Preserve a high-water mark while replay state rejects an
	// exact duplicate.
	if contentRelated {
		return nil
	}
	if metadata.sequenceNo < expected {
		return badMessage(metadata, CodeSequenceNoTooLow, state.serverSalt, ErrSequenceNoTooLow)
	}
	return badMessage(metadata, CodeSequenceNoTooHigh, state.serverSalt, ErrSequenceNoTooHigh)
}

func validKind(kind MessageKind) bool {
	return kind == ContentRelated || kind == NonContentRelated || kind == Container
}

func badMessage(metadata messageMetadata, code int32, expectedSalt int64, cause error) error {
	return &BadMessageError{
		MessageID:          metadata.messageID,
		SequenceNo:         metadata.sequenceNo,
		Code:               code,
		ExpectedServerSalt: expectedSalt,
		Cause:              cause,
	}
}

func (state validatorState) clone() validatorState {
	state.recentIDs = append([]int64(nil), state.recentIDs...)
	state.recentSet = make(map[int64]struct{}, len(state.recentSet))
	for _, id := range state.recentIDs {
		state.recentSet[id] = struct{}{}
	}
	return state
}

func (state *validatorState) remember(messageID int64, limit int) {
	if len(state.recentIDs) == limit {
		oldest := state.recentIDs[0]
		delete(state.recentSet, oldest)
		copy(state.recentIDs, state.recentIDs[1:])
		state.recentIDs = state.recentIDs[:limit-1]
	}
	state.recentIDs = append(state.recentIDs, messageID)
	state.recentSet[messageID] = struct{}{}
}

// Snapshot returns a defensive copy of the current protocol state.
func (v *Validator) Snapshot() Snapshot {
	if v == nil {
		return Snapshot{}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return Snapshot{
		SessionID:        v.state.sessionID,
		ServerSalt:       v.state.serverSalt,
		SequenceNo:       v.state.sequenceNo,
		HighestMessageID: v.state.highestMessageID,
		RecentMessageIDs: append([]int64(nil), v.state.recentIDs...),
	}
}

// SetServerSalt updates the expected salt for subsequent messages.
func (v *Validator) SetServerSalt(serverSalt int64) {
	if v == nil {
		return
	}
	v.mu.Lock()
	v.state.serverSalt = serverSalt
	v.mu.Unlock()
}
