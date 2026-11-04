package authstore

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestFileStoreConcurrentIdentities(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring.json")
	store := &FileStore{Path: path}
	if err := store.Set(UserCircleCI, Record{Token: "synthetic-circleci"}); err != nil {
		t.Fatal(err)
	}
	const commands = 24
	start := make(chan struct{})
	errs := make([]error, commands)
	var wg sync.WaitGroup
	for i := range commands {
		wg.Go(func() {
			<-start
			// Each instance represents another command using the same file.
			s := &FileStore{Path: path}
			errs[i] = s.Set(fmt.Sprintf("synthetic-%d", i), Record{Login: fmt.Sprintf("identity-%d", i), Token: "synthetic-token"})
			if errs[i] == nil {
				_, errs[i] = s.Get(UserCircleCI)
			}
		})
	}
	close(start)
	wg.Wait()
	failed, lost := 0, 0
	for i, err := range errs {
		if err != nil {
			failed++
		}
		record, err := store.Get(fmt.Sprintf("synthetic-%d", i))
		if err != nil || record.Login != fmt.Sprintf("identity-%d", i) {
			lost++
		}
	}
	if failed != 0 || lost != 0 {
		t.Errorf("concurrent commands: %d failed operations, %d missing records", failed, lost)
	}
	if record, err := store.Get(UserCircleCI); err != nil || record.Token != "synthetic-circleci" {
		t.Errorf("unrelated CircleCI identity was lost: %v", err)
	}
}

func TestFileStoreConcurrentDeleteAndSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keyring.json")
	store := &FileStore{Path: path}
	const commands = 12
	for i := range commands {
		if err := store.Set(fmt.Sprintf("remove-%d", i), Record{Token: "synthetic-token"}); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range commands {
		wg.Go(func() {
			<-start
			s := &FileStore{Path: path}
			if err := s.Delete(fmt.Sprintf("remove-%d", i)); err != nil {
				t.Errorf("delete %d: %v", i, err)
			}
		})
		wg.Go(func() {
			<-start
			s := &FileStore{Path: path}
			if err := s.Set(fmt.Sprintf("keep-%d", i), Record{Token: "synthetic-token"}); err != nil {
				t.Errorf("set %d: %v", i, err)
			}
		})
	}
	close(start)
	wg.Wait()
	for i := range commands {
		if _, err := store.Get(fmt.Sprintf("remove-%d", i)); !errors.Is(err, ErrNotFound) {
			t.Errorf("deleted record %d returned: %v", i, err)
		}
		if _, err := store.Get(fmt.Sprintf("keep-%d", i)); err != nil {
			t.Errorf("new record %d was lost: %v", i, err)
		}
	}
}

func TestFileStoreOperationsUnderIdentityLock(t *testing.T) {
	s := &FileStore{Path: filepath.Join(t.TempDir(), "keyring.json")}
	unlock, err := s.Lock(UserGitHub)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := s.Set(UserGitHub, Record{Token: "synthetic-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(UserGitHub); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(UserGitHub); err != nil {
		t.Fatal(err)
	}
}
