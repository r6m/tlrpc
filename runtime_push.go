package tlrpc

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/r6m/tlrpc/crypto"
	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/session"
)

type runtimePushBinding struct {
	binding Binding
	sender  runtimev2.Sender
}

// runtimePushRegistry stores semantic senders only. It never receives a raw
// transport, auth key, message-ID generator, sequence generator, or packet.
type runtimePushRegistry struct {
	mu     sync.RWMutex
	byKey  map[session.SessionKey][]runtimePushBinding
	byUser map[int64]map[session.SessionKey][]runtimev2.Sender
	server *Server
}

func newRuntimePushRegistry(server *Server) *runtimePushRegistry {
	return &runtimePushRegistry{
		byKey:  make(map[session.SessionKey][]runtimePushBinding),
		byUser: make(map[int64]map[session.SessionKey][]runtimev2.Sender),
		server: server,
	}
}

func (r *runtimePushRegistry) Update(snapshot session.Snapshot, sender runtimev2.Sender, acceptsPush bool) {
	if r == nil || sender == nil {
		return
	}
	key := snapshot.Key()
	binding := Binding{
		ConnectionID: connectionIDFromRuntimeSender(sender),
		AuthKeyID:    int64(snapshot.AuthKeyID), SessionID: snapshot.SessionID,
		ServerSalt: snapshot.ServerSalt, UserID: snapshot.UserID, Layer: snapshot.Layer,
	}
	r.mu.Lock()
	bindingsForKey := r.byKey[key]
	index := -1
	var previous runtimePushBinding
	for i, candidate := range bindingsForKey {
		if candidate.sender == sender {
			index = i
			previous = candidate
			break
		}
	}
	existed := index >= 0
	if existed {
		r.removeUserLocked(key, previous)
		bindingsForKey[index] = runtimePushBinding{binding: binding, sender: sender}
	} else {
		bindingsForKey = append(bindingsForKey, runtimePushBinding{binding: binding, sender: sender})
	}
	r.byKey[key] = bindingsForKey
	if binding.UserID != 0 && acceptsPush {
		bindings := r.byUser[binding.UserID]
		if bindings == nil {
			bindings = make(map[session.SessionKey][]runtimev2.Sender)
			r.byUser[binding.UserID] = bindings
		}
		bindings[key] = append(bindings[key], sender)
	}
	r.mu.Unlock()
	bindingChanged := !existed || previous.binding != binding
	if bindingChanged && r.server != nil {
		r.server.handleObservedSessionBound(binding)
	}
}

func connectionIDFromRuntimeSender(sender runtimev2.Sender) uint64 {
	identified, ok := sender.(interface{ ConnectionID() uint64 })
	if !ok {
		return 0
	}
	return identified.ConnectionID()
}

func (r *runtimePushRegistry) Remove(key session.SessionKey, sender runtimev2.Sender) {
	if r == nil || sender == nil {
		return
	}
	r.mu.Lock()
	bindings := r.byKey[key]
	index := -1
	var binding runtimePushBinding
	for i, candidate := range bindings {
		if candidate.sender == sender {
			index = i
			binding = candidate
			break
		}
	}
	exists := index >= 0
	if exists {
		bindings = append(bindings[:index], bindings[index+1:]...)
		if len(bindings) == 0 {
			delete(r.byKey, key)
		} else {
			r.byKey[key] = bindings
		}
		r.removeUserLocked(key, binding)
	}
	r.mu.Unlock()
	if exists && r.server != nil {
		r.server.handleObservedSessionUnbound(binding.binding)
	}
}

func (r *runtimePushRegistry) removeUserLocked(key session.SessionKey, binding runtimePushBinding) {
	if binding.binding.UserID == 0 {
		return
	}
	bindings := r.byUser[binding.binding.UserID]
	senders := bindings[key]
	for i, sender := range senders {
		if sender == binding.sender {
			senders = append(senders[:i], senders[i+1:]...)
			break
		}
	}
	if len(senders) == 0 {
		delete(bindings, key)
	} else {
		bindings[key] = senders
	}
	if len(bindings) == 0 {
		delete(r.byUser, binding.binding.UserID)
	}
}

func (r *runtimePushRegistry) Publish(ctx context.Context, userID int64, body []byte) error {
	return r.publish(ctx, userID, nil, nil, body)
}

func (r *runtimePushRegistry) PublishExcept(ctx context.Context, userID int64, excluded session.SessionKey, body []byte) error {
	return r.publish(ctx, userID, &excluded, nil, body)
}

func (r *runtimePushRegistry) PublishExceptAuthKey(ctx context.Context, userID int64, excluded crypto.KeyID, body []byte) error {
	return r.publish(ctx, userID, nil, &excluded, body)
}

func (r *runtimePushRegistry) publish(ctx context.Context, userID int64, excludedSession *session.SessionKey, excludedAuthKey *crypto.KeyID, body []byte) error {
	if r == nil || userID <= 0 {
		return nil
	}
	r.mu.RLock()
	bindings := r.byUser[userID]
	senders := make([]runtimev2.Sender, 0, len(bindings))
	for key, sessionSenders := range bindings {
		if excludedSession != nil && key == *excludedSession || excludedAuthKey != nil && key.AuthKeyID == *excludedAuthKey {
			continue
		}
		senders = append(senders, sessionSenders...)
	}
	r.mu.RUnlock()
	var failures []error
	for _, sender := range senders {
		if err := sender.Push(ctx, append([]byte(nil), body...)); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Publish sends one schema-defined server push to every active session bound
// to userID through Runtime v2's per-connection writer.
func (s *Server) Publish(userID int64, update TLObject) error {
	return s.PublishContext(context.Background(), userID, update)
}

// PublishContext is Publish with caller-controlled cancellation and deadlines.
func (s *Server) PublishContext(ctx context.Context, userID int64, update TLObject) error {
	return s.publishContext(ctx, userID, nil, nil, update)
}

// PublishExcept sends one schema-defined server push to every active session
// bound to userID except the session identified by excluded's exact
// (AuthKeyID, SessionID) pair.
func (s *Server) PublishExcept(userID int64, excluded Binding, update TLObject) error {
	return s.PublishExceptContext(context.Background(), userID, excluded, update)
}

// PublishExceptContext is PublishExcept with caller-controlled cancellation
// and deadlines. Only excluded.AuthKeyID and excluded.SessionID identify the
// excluded session; the other Binding fields are ignored.
func (s *Server) PublishExceptContext(ctx context.Context, userID int64, excluded Binding, update TLObject) error {
	key := session.SessionKey{
		AuthKeyID: crypto.KeyID(excluded.AuthKeyID),
		SessionID: excluded.SessionID,
	}
	return s.publishContext(ctx, userID, &key, nil, update)
}

// PublishExceptAuthKey sends one schema-defined server push to every active
// authorization bound to userID except every session using excludedAuthKeyID.
func (s *Server) PublishExceptAuthKey(userID, excludedAuthKeyID int64, update TLObject) error {
	return s.PublishExceptAuthKeyContext(context.Background(), userID, excludedAuthKeyID, update)
}

// PublishExceptAuthKeyContext is PublishExceptAuthKey with caller-controlled
// cancellation and deadlines.
func (s *Server) PublishExceptAuthKeyContext(ctx context.Context, userID, excludedAuthKeyID int64, update TLObject) error {
	authKeyID := crypto.KeyID(excludedAuthKeyID)
	return s.publishContext(ctx, userID, nil, &authKeyID, update)
}

func (s *Server) publishContext(ctx context.Context, userID int64, excludedSession *session.SessionKey, excludedAuthKey *crypto.KeyID, update TLObject) error {
	if s == nil || s.runtimePushes == nil {
		return nil
	}
	if userID <= 0 {
		return ErrInvalidUserID
	}
	body, err := encodeTLObject(update)
	if err != nil {
		return fmt.Errorf("tlrpc: encode push: %w", err)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if excludedSession != nil {
		return s.runtimePushes.PublishExcept(ctx, userID, *excludedSession, body)
	}
	if excludedAuthKey != nil {
		return s.runtimePushes.PublishExceptAuthKey(ctx, userID, *excludedAuthKey, body)
	}
	return s.runtimePushes.Publish(ctx, userID, body)
}

var _ runtimev2.SessionPresence = (*runtimePushRegistry)(nil)
