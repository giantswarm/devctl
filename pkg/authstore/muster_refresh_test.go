package authstore

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/muster"
)

// Both commands read the expired pair before either starts renewing it.
// Reads performed after taking the refresh lock must see the latest pair.
type musterRefreshBarrier struct {
	Store
	reads atomic.Int32
	ready chan struct{}
}

type musterRefreshStore struct {
	Store
	lockErr   error
	readErr   error
	writeErr  error
	afterLock func()
	locked    bool
	unlocks   int
}

func (s *musterRefreshStore) Lock(user string) (func(), error) {
	if user != UserMuster {
		return nil, errors.New("locked the wrong identity")
	}
	if s.lockErr != nil {
		return nil, s.lockErr
	}
	s.locked = true
	if s.afterLock != nil {
		s.afterLock()
	}
	return func() { s.locked = false; s.unlocks++ }, nil
}

func (s *musterRefreshStore) Get(user string) (Record, error) {
	if s.locked && s.readErr != nil {
		return Record{}, s.readErr
	}
	return s.Store.Get(user)
}

func (s *musterRefreshStore) Set(user string, record Record) error {
	if !s.locked {
		return errors.New("refresh persisted without the lock")
	}
	if s.writeErr != nil {
		return s.writeErr
	}
	return s.Store.Set(user, record)
}

func TestRequireMusterRefreshLockLifecycle(t *testing.T) {
	failure := errors.New("synthetic storage failure")
	for _, name := range []string{"renewed while waiting", "deleted while waiting", "lock failure", "read failure", "refresh refused", "write failure"} {
		t.Run(name, func(t *testing.T) {
			m := muster.Start(muster.Config{RefreshToken: "synthetic-refresh"})
			defer m.Close()
			a, store := newMusterAuth(t, func() time.Time { return testNow })
			record := Record{Token: "stale", RefreshToken: "synthetic-refresh", ExpiresAt: testNow.Add(-time.Hour), Endpoint: m.MCPURL(), Issuer: m.URL}
			if name == "refresh refused" {
				record.RefreshToken = "refused"
			}
			if err := store.Set(UserMuster, record); err != nil {
				t.Fatal(err)
			}
			wrapped := &musterRefreshStore{Store: store}
			wantErr := failure
			wantUnlocks := 1
			switch name {
			case "renewed while waiting":
				wantErr = nil
				wrapped.afterLock = func() {
					record.Token, record.ExpiresAt = "renewed", testNow.Add(time.Hour)
					if err := store.Set(UserMuster, record); err != nil {
						t.Fatal(err)
					}
				}
			case "deleted while waiting":
				wantErr = ErrAuthRequired
				wrapped.readErr = ErrNotFound
			case "lock failure":
				wrapped.lockErr, wantUnlocks = failure, 0
			case "read failure":
				wrapped.readErr = failure
			case "refresh refused":
				wantErr = ErrAuthRequired
			case "write failure":
				wrapped.writeErr = failure
			}
			a.store = wrapped
			token, err := a.RequireMuster(context.Background())
			if !errors.Is(err, wantErr) {
				t.Fatalf("RequireMuster error = %v, want %v", err, wantErr)
			}
			if wantErr == nil && token.Value != "renewed" {
				t.Error("did not reuse the record persisted while waiting")
			}
			if wrapped.locked || wrapped.unlocks != wantUnlocks {
				t.Errorf("lock lifecycle: held=%t, unlocks=%d, want %d", wrapped.locked, wrapped.unlocks, wantUnlocks)
			}
			if name != "refresh refused" && name != "write failure" && len(m.Requests()) != 0 {
				t.Error("unexpected network access")
			}
		})
	}
}

func (s *musterRefreshBarrier) Get(user string) (Record, error) {
	record, err := s.Store.Get(user)
	if user == UserMuster {
		switch s.reads.Add(1) {
		case 1:
			<-s.ready
		case 2:
			close(s.ready)
		}
	}
	return record, err
}

func TestRequireMusterConcurrentRefresh(t *testing.T) {
	m := muster.Start(muster.Config{})
	defer m.Close()
	now := testNow
	a, store := newMusterAuth(t, func() time.Time { return now })
	if _, err := a.LoginMuster(context.Background(), m.MCPURL()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	barrier := &musterRefreshBarrier{Store: store, ready: make(chan struct{})}
	a.store = barrier
	other := *a
	tokens := make([]Token, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, auth := range []*Auth{a, &other} {
		wg.Go(func() { tokens[i], errs[i] = auth.RequireMuster(context.Background()) })
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("command %d could not use the renewed login: %v", i, err)
		}
	}
	if tokens[0].Value == "" || tokens[0].Value != tokens[1].Value {
		t.Errorf("commands did not share the renewed access token")
	}
	// One initial code exchange and one refresh, regardless of command count.
	exchanges := 0
	for _, request := range m.Requests() {
		if request.Path == "/oauth/token" {
			exchanges++
		}
	}
	if exchanges != 2 {
		t.Errorf("token exchanges = %d, want 2", exchanges)
	}
	stored, err := store.Get(UserMuster)
	if err != nil || stored.Token != tokens[0].Value {
		t.Errorf("renewed access token was not persisted: %v", err)
	}
}
