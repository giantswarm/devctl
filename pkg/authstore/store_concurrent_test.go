package authstore

import (
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
		})
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("command %d could not persist its record: %v", i, err)
		}
		record, err := store.Get(fmt.Sprintf("synthetic-%d", i))
		if err != nil || record.Login != fmt.Sprintf("identity-%d", i) {
			t.Errorf("command %d lost its record: %v", i, err)
		}
	}
	if record, err := store.Get(UserCircleCI); err != nil || record.Token != "synthetic-circleci" {
		t.Errorf("unrelated CircleCI identity was lost: %v", err)
	}
}
