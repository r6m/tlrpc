package session

import (
	"time"

	"github.com/r6m/tlrpc/crypto"
)

// Snapshot is the detached, encoding-neutral durable protocol state owned by
// Runtime v2. Application state belongs in application storage, keyed by Key.
type Snapshot struct {
	AuthKeyID          crypto.KeyID
	SessionID          int64
	Layer              int
	UserID             int64
	ServerSalt         int64
	SeqNo              int32
	ServerSeqNo        int32
	LastClientMsgID    int64
	RecentClientMsgIDs []int64
	Client             ClientMetadata
	NewSessionCreated  bool
	FirstClientMsgID   int64
	CreatedAt          time.Time
	LastActivity       time.Time
}

// ClientMetadata is protocol metadata declared by initConnection. It is named
// durable framework state rather than an entry in an application data map.
type ClientMetadata struct {
	APIID          int32
	DeviceModel    string
	SystemVersion  string
	AppVersion     string
	SystemLangCode string
	LangPack       string
	LangCode       string
}

// Key returns the complete MTProto identity carried by the snapshot.
func (s Snapshot) Key() SessionKey {
	return SessionKey{AuthKeyID: s.AuthKeyID, SessionID: s.SessionID}
}

// Clone detaches slices so callers and stores never share mutable containers.
func (s Snapshot) Clone() Snapshot {
	clone := s
	clone.RecentClientMsgIDs = append([]int64(nil), s.RecentClientMsgIDs...)
	return clone
}
