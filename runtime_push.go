package tlrpc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/r6m/tlrpc/crypto"
	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
	"github.com/r6m/tlrpc/session"
)

var (
	ErrPushProjectorRequired = errors.New("tlrpc: push projector is required")
	ErrPushBindingChanged    = errors.New("tlrpc: projected push binding changed")
)

// PushProjector builds one schema-defined push for the exact eligible session
// binding supplied by Runtime v2. Binding.Layer is the effective layer after
// runtime validation and schema-layer capping, not necessarily the raw value
// declared by the client.
type PushProjector func(context.Context, Binding) (TLObject, error)

type runtimePushBinding struct {
	mu        sync.Mutex
	binding   Binding
	sender    runtimev2.Sender
	reachable bool
}

// runtimePushRegistry stores semantic senders only. It never receives a raw
// transport, auth key, message-ID generator, sequence generator, or packet.
type runtimePushRegistry struct {
	mu     sync.RWMutex
	byKey  map[session.SessionKey][]*runtimePushBinding
	byUser map[int64]map[session.SessionKey][]*runtimePushBinding
	server *Server
}

func newRuntimePushRegistry(server *Server) *runtimePushRegistry {
	return &runtimePushRegistry{
		byKey:  make(map[session.SessionKey][]*runtimePushBinding),
		byUser: make(map[int64]map[session.SessionKey][]*runtimePushBinding),
		server: server,
	}
}

func (r *runtimePushRegistry) Update(snapshot session.Snapshot, leaseGeneration int64, sender runtimev2.Sender, acceptsPush bool) {
	if r == nil || sender == nil {
		return
	}
	key := snapshot.Key()
	layer := snapshot.Layer
	if layer == 0 && r.server != nil {
		layer = r.server.schemaLayer
	}
	binding := Binding{
		ConnectionID: connectionIDFromRuntimeSender(sender),
		AuthKeyID:    int64(snapshot.AuthKeyID), SessionID: snapshot.SessionID,
		LeaseGeneration: leaseGeneration,
		ServerSalt:      snapshot.ServerSalt, UserID: snapshot.UserID, Layer: layer,
	}
	for {
		r.mu.RLock()
		target, _ := findRuntimePushBinding(r.byKey[key], sender)
		r.mu.RUnlock()
		if target == nil {
			r.mu.Lock()
			if current, _ := findRuntimePushBinding(r.byKey[key], sender); current != nil {
				r.mu.Unlock()
				continue
			}
			target = &runtimePushBinding{binding: binding, sender: sender, reachable: binding.UserID != 0 && acceptsPush}
			r.byKey[key] = append(r.byKey[key], target)
			if target.reachable {
				r.addUserLocked(key, target, binding.UserID)
			}
			r.mu.Unlock()
			if r.server != nil {
				r.server.handleObservedSessionBound(binding)
			}
			return
		}

		target.mu.Lock()
		r.mu.Lock()
		if !runtimePushBindingRegistered(r.byKey[key], target) {
			r.mu.Unlock()
			target.mu.Unlock()
			continue
		}
		previous := target.binding
		r.removeUserLocked(key, target, previous.UserID)
		target.binding = binding
		target.reachable = binding.UserID != 0 && acceptsPush
		if target.reachable {
			r.addUserLocked(key, target, binding.UserID)
		}
		r.mu.Unlock()
		target.mu.Unlock()
		if previous != binding && r.server != nil {
			r.server.handleObservedSessionBound(binding)
		}
		return
	}
}

func findRuntimePushBinding(bindings []*runtimePushBinding, sender runtimev2.Sender) (*runtimePushBinding, int) {
	for index, candidate := range bindings {
		if candidate.sender == sender {
			return candidate, index
		}
	}
	return nil, -1
}

func runtimePushBindingRegistered(bindings []*runtimePushBinding, target *runtimePushBinding) bool {
	for _, candidate := range bindings {
		if candidate == target {
			return true
		}
	}
	return false
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
	for {
		r.mu.RLock()
		target, _ := findRuntimePushBinding(r.byKey[key], sender)
		r.mu.RUnlock()
		if target == nil {
			return
		}
		target.mu.Lock()
		r.mu.Lock()
		bindings := r.byKey[key]
		_, index := findRuntimePushBinding(bindings, sender)
		if index < 0 || bindings[index] != target {
			r.mu.Unlock()
			target.mu.Unlock()
			continue
		}
		binding := target.binding
		bindings = append(bindings[:index], bindings[index+1:]...)
		if len(bindings) == 0 {
			delete(r.byKey, key)
		} else {
			r.byKey[key] = bindings
		}
		r.removeUserLocked(key, target, binding.UserID)
		target.reachable = false
		r.mu.Unlock()
		target.mu.Unlock()
		if r.server != nil {
			r.server.handleObservedSessionUnbound(binding)
		}
		return
	}
}

func (r *runtimePushRegistry) addUserLocked(key session.SessionKey, target *runtimePushBinding, userID int64) {
	bindings := r.byUser[userID]
	if bindings == nil {
		bindings = make(map[session.SessionKey][]*runtimePushBinding)
		r.byUser[userID] = bindings
	}
	bindings[key] = append(bindings[key], target)
}

func (r *runtimePushRegistry) removeUserLocked(key session.SessionKey, target *runtimePushBinding, userID int64) {
	if userID == 0 {
		return
	}
	bindings := r.byUser[userID]
	targets := bindings[key]
	for i, candidate := range targets {
		if candidate == target {
			targets = append(targets[:i], targets[i+1:]...)
			break
		}
	}
	if len(targets) == 0 {
		delete(bindings, key)
	} else {
		bindings[key] = targets
	}
	if len(bindings) == 0 {
		delete(r.byUser, userID)
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
	for key, sessionBindings := range bindings {
		if excludedSession != nil && key == *excludedSession || excludedAuthKey != nil && key.AuthKeyID == *excludedAuthKey {
			continue
		}
		for _, target := range sessionBindings {
			senders = append(senders, target.sender)
		}
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

func (r *runtimePushRegistry) publishProjected(ctx context.Context, userID int64, excludedSession *session.SessionKey, excludedAuthKey *crypto.KeyID, projector PushProjector, maxEncodedBytes int) error {
	if r == nil || userID <= 0 {
		return nil
	}
	if projector == nil {
		return ErrPushProjectorRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.RLock()
	bindings := r.byUser[userID]
	registrations := make([]*runtimePushBinding, 0, len(bindings))
	for key, sessionBindings := range bindings {
		if excludedSession != nil && key == *excludedSession || excludedAuthKey != nil && key.AuthKeyID == *excludedAuthKey {
			continue
		}
		registrations = append(registrations, sessionBindings...)
	}
	r.mu.RUnlock()
	targets := make([]projectedPushTarget, 0, len(registrations))
	for _, target := range registrations {
		target.mu.Lock()
		binding := target.binding
		reachable := target.reachable && binding.UserID == userID
		target.mu.Unlock()
		if !reachable {
			continue
		}
		key := session.SessionKey{AuthKeyID: crypto.KeyID(binding.AuthKeyID), SessionID: binding.SessionID}
		if excludedSession != nil && key == *excludedSession || excludedAuthKey != nil && key.AuthKeyID == *excludedAuthKey {
			continue
		}
		targets = append(targets, projectedPushTarget{registration: target, binding: binding})
	}

	var failures []error
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		object, err := projector(ctx, target.binding)
		if err != nil {
			failures = append(failures, fmt.Errorf("tlrpc: project push for auth key %d session %d generation %d layer %d: %w", target.binding.AuthKeyID, target.binding.SessionID, target.binding.LeaseGeneration, target.binding.Layer, err))
			continue
		}
		body, err := encodeTLObjectWithLimitsForLayer(object, EncodeLimits{MaxEncodedBytes: maxEncodedBytes}, target.binding.Layer)
		if err != nil {
			failures = append(failures, fmt.Errorf("tlrpc: encode projected push for auth key %d session %d generation %d layer %d: %w", target.binding.AuthKeyID, target.binding.SessionID, target.binding.LeaseGeneration, target.binding.Layer, err))
			continue
		}
		if err := r.pushProjectedIfCurrent(ctx, target, body); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

type projectedPushTarget struct {
	registration *runtimePushBinding
	binding      Binding
}

// pushProjectedIfCurrent serializes the final binding check with presence
// updates. Sender.Push copies the body into the exact session writer or its
// bounded recovery FIFO before returning, so the effective layer cannot drift
// between validation and acceptance.
func (r *runtimePushRegistry) pushProjectedIfCurrent(ctx context.Context, target projectedPushTarget, body []byte) error {
	current := target.registration
	current.mu.Lock()
	defer current.mu.Unlock()
	if current.reachable && current.binding == target.binding {
		if err := current.sender.Push(ctx, append([]byte(nil), body...)); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("%w: auth key %d session %d generation %d layer %d", ErrPushBindingChanged, target.binding.AuthKeyID, target.binding.SessionID, target.binding.LeaseGeneration, target.binding.Layer)
}

func (r *runtimePushRegistry) ActiveUserIDs() []int64 {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	userIDs := make([]int64, 0, len(r.byUser))
	for userID := range r.byUser {
		if userID > 0 {
			userIDs = append(userIDs, userID)
		}
	}
	r.mu.RUnlock()
	sort.Slice(userIDs, func(i, j int) bool {
		return userIDs[i] < userIDs[j]
	})
	return userIDs
}

func (r *runtimePushRegistry) HasActiveUser(userID int64) bool {
	if r == nil || userID <= 0 {
		return false
	}
	r.mu.RLock()
	active := len(r.byUser[userID]) != 0
	r.mu.RUnlock()
	return active
}

// Publish sends one schema-defined server push to every active session bound
// to userID through Runtime v2's per-connection writer.
func (s *Server) Publish(userID int64, update TLObject) error {
	return s.PublishContext(context.Background(), userID, update)
}

// ActiveUserIDs returns a sorted snapshot of positive user IDs that currently
// have at least one process-local push-reachable session in Runtime v2.
func (s *Server) ActiveUserIDs() []int64 {
	if s == nil || s.runtimePushes == nil {
		return nil
	}
	return s.runtimePushes.ActiveUserIDs()
}

// HasActiveUser reports whether userID currently has at least one process-local
// push-reachable session in Runtime v2 without allocating a full user snapshot.
func (s *Server) HasActiveUser(userID int64) bool {
	return s != nil && s.runtimePushes != nil && s.runtimePushes.HasActiveUser(userID)
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

// PublishProjected builds and sends one schema-defined push for every active
// session bound to userID. The projector receives each exact eligible binding.
func (s *Server) PublishProjected(userID int64, projector PushProjector) error {
	return s.PublishProjectedContext(context.Background(), userID, projector)
}

// PublishProjectedContext is PublishProjected with caller-controlled
// cancellation and deadlines.
func (s *Server) PublishProjectedContext(ctx context.Context, userID int64, projector PushProjector) error {
	return s.publishProjectedContext(ctx, userID, nil, nil, projector)
}

// PublishProjectedExcept projects a push for every active session bound to
// userID except the exact (AuthKeyID, SessionID) pair in excluded.
func (s *Server) PublishProjectedExcept(userID int64, excluded Binding, projector PushProjector) error {
	return s.PublishProjectedExceptContext(context.Background(), userID, excluded, projector)
}

// PublishProjectedExceptContext is PublishProjectedExcept with
// caller-controlled cancellation and deadlines. Fields of excluded other than
// AuthKeyID and SessionID do not participate in origin exclusion.
func (s *Server) PublishProjectedExceptContext(ctx context.Context, userID int64, excluded Binding, projector PushProjector) error {
	key := session.SessionKey{AuthKeyID: crypto.KeyID(excluded.AuthKeyID), SessionID: excluded.SessionID}
	return s.publishProjectedContext(ctx, userID, &key, nil, projector)
}

// PublishProjectedExceptAuthKey projects a push for every active session
// bound to userID except sessions using excludedAuthKeyID.
func (s *Server) PublishProjectedExceptAuthKey(userID, excludedAuthKeyID int64, projector PushProjector) error {
	return s.PublishProjectedExceptAuthKeyContext(context.Background(), userID, excludedAuthKeyID, projector)
}

// PublishProjectedExceptAuthKeyContext is PublishProjectedExceptAuthKey with
// caller-controlled cancellation and deadlines.
func (s *Server) PublishProjectedExceptAuthKeyContext(ctx context.Context, userID, excludedAuthKeyID int64, projector PushProjector) error {
	authKeyID := crypto.KeyID(excludedAuthKeyID)
	return s.publishProjectedContext(ctx, userID, nil, &authKeyID, projector)
}

func (s *Server) publishProjectedContext(ctx context.Context, userID int64, excludedSession *session.SessionKey, excludedAuthKey *crypto.KeyID, projector PushProjector) error {
	if s == nil || s.runtimePushes == nil {
		return nil
	}
	if userID <= 0 {
		return ErrInvalidUserID
	}
	if projector == nil {
		return ErrPushProjectorRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.runtimePushes.publishProjected(ctx, userID, excludedSession, excludedAuthKey, projector, s.maxEncodedResponseBytes)
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
