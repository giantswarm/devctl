//go:build linux

package authstore

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

const (
	fakeCollectionPath = dbus.ObjectPath("/org/freedesktop/secrets/collection/test")
	fakeSessionPath    = dbus.ObjectPath("/org/freedesktop/secrets/session/test")
	fakePromptPath     = dbus.ObjectPath("/org/freedesktop/secrets/prompt/test")
	ifaceProperties    = "org.freedesktop.DBus.Properties"
)

// fakeSecretService is a Secret Service with one collection, exported on a
// private bus connection; the client is pointed at its unique bus name. It
// records every call so a test can see what the client did.
type fakeSecretService struct {
	conn *dbus.Conn

	mu sync.Mutex
	// locked: the collection needs an Unlock with a prompt before use.
	locked bool
	// dismiss: every prompt completes dismissed.
	dismiss bool
	// promptOnDelete: Item.Delete returns a prompt, as a keyring asking for
	// confirmation does.
	promptOnDelete bool
	// noDefault: nothing has the default alias.
	noDefault bool
	// failSearch: SearchItems answers a D-Bus error.
	failSearch bool
	items      map[dbus.ObjectPath]fakeItem
	next       int
	calls      []string
}

type fakeItem struct {
	label      string
	attributes map[string]string
	value      []byte
}

// newFakeSecretService exports the fake and returns the client that talks to
// it. configure sets the fake's state before it goes on the bus.
func newFakeSecretService(t *testing.T, configure func(f *fakeSecretService)) (*fakeSecretService, secretService) {
	t.Helper()
	server, err := connectSessionBus()
	if err != nil {
		t.Skipf("no session bus, the Secret Service client is not exercised: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	f := &fakeSecretService{conn: server, items: map[dbus.ObjectPath]fakeItem{}}
	if configure != nil {
		configure(f)
	}
	f.export(t)
	dest := server.Names()[0]
	return f, secretService{dial: func() (*dbus.Conn, string, error) {
		conn, err := connectSessionBus()
		return conn, dest, err
	}}
}

func (f *fakeSecretService) export(t *testing.T) {
	t.Helper()
	exports := []struct {
		path    dbus.ObjectPath
		iface   string
		methods map[string]any
	}{
		{secretsPath, ifaceService, map[string]any{
			"OpenSession": f.openSession,
			"ReadAlias":   f.readAlias,
			"Unlock":      f.unlock,
		}},
		{fakeSessionPath, ifaceSession, map[string]any{"Close": f.closeSession}},
		{fakeCollectionPath, ifaceCollection, map[string]any{
			"SearchItems": f.searchItems,
			"CreateItem":  f.createItem,
		}},
		{fakeCollectionPath, ifaceProperties, map[string]any{"Get": f.getProperty}},
		{fakePromptPath, ifacePrompt, map[string]any{"Prompt": f.prompt}},
	}
	for _, e := range exports {
		if err := f.conn.ExportMethodTable(e.methods, e.path, e.iface); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *fakeSecretService) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

func (f *fakeSecretService) openSession(algorithm string, _ dbus.Variant) (dbus.Variant, dbus.ObjectPath, *dbus.Error) {
	f.record("OpenSession " + algorithm)
	if algorithm != plainSession {
		return dbus.Variant{}, "", dbus.NewError("org.freedesktop.DBus.Error.NotSupported", nil)
	}
	return dbus.MakeVariant(""), fakeSessionPath, nil
}

func (f *fakeSecretService) closeSession() *dbus.Error {
	f.record("Session.Close")
	return nil
}

func (f *fakeSecretService) readAlias(name string) (dbus.ObjectPath, *dbus.Error) {
	f.record("ReadAlias " + name)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.noDefault || name != defaultAlias {
		return nullPath, nil
	}
	return fakeCollectionPath, nil
}

func (f *fakeSecretService) getProperty(iface, name string) (dbus.Variant, *dbus.Error) {
	f.record(fmt.Sprintf("Get %s.%s", iface, name))
	f.mu.Lock()
	defer f.mu.Unlock()
	if iface != ifaceCollection || name != "Locked" {
		return dbus.Variant{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownProperty", nil)
	}
	return dbus.MakeVariant(f.locked), nil
}

func (f *fakeSecretService) unlock(objects []dbus.ObjectPath) ([]dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.record(fmt.Sprintf("Unlock %v", objects))
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range objects {
		if o != fakeCollectionPath {
			// The alias path is exactly what oo7-daemon and KeePassXC refuse.
			return nil, "", dbus.NewError("org.freedesktop.DBus.Error.UnknownObject", []any{string(o)})
		}
	}
	if !f.locked {
		return objects, nullPath, nil
	}
	return nil, fakePromptPath, nil
}

// prompt completes asynchronously, as a real prompt does once the person
// answered: unlocking the collection unless the prompt is dismissed.
func (f *fakeSecretService) prompt(windowID string) *dbus.Error {
	f.record("Prompt " + windowID)
	f.mu.Lock()
	dismissed := f.dismiss
	if !dismissed {
		f.locked = false
	}
	f.mu.Unlock()
	go func() {
		_ = f.conn.Emit(fakePromptPath, ifacePrompt+".Completed", dismissed, dbus.MakeVariant([]dbus.ObjectPath{fakeCollectionPath}))
	}()
	return nil
}

func (f *fakeSecretService) searchItems(attributes map[string]string) ([]dbus.ObjectPath, *dbus.Error) {
	f.record(fmt.Sprintf("SearchItems %s/%s", attributes["service"], attributes["username"]))
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failSearch {
		return nil, dbus.NewError("org.freedesktop.DBus.Error.Failed", []any{"the database is gone"})
	}
	var found []dbus.ObjectPath
	for path, item := range f.items {
		if item.attributes["service"] == attributes["service"] && item.attributes["username"] == attributes["username"] {
			found = append(found, path)
		}
	}
	return found, nil
}

func (f *fakeSecretService) createItem(properties map[string]dbus.Variant, s secret, replace bool) (dbus.ObjectPath, dbus.ObjectPath, *dbus.Error) {
	f.record(fmt.Sprintf("CreateItem replace=%t session=%s", replace, s.Session))
	var attributes map[string]string
	if err := properties[ifaceItem+".Attributes"].Store(&attributes); err != nil {
		return "", "", dbus.MakeFailedError(err)
	}
	label, _ := properties[ifaceItem+".Label"].Value().(string)
	f.mu.Lock()
	defer f.mu.Unlock()
	if replace {
		for path, item := range f.items {
			if item.attributes["service"] == attributes["service"] && item.attributes["username"] == attributes["username"] {
				f.items[path] = fakeItem{label: label, attributes: attributes, value: s.Value}
				return path, nullPath, nil
			}
		}
	}
	return f.addLocked(label, attributes, s.Value), nullPath, nil
}

// put stores an item directly, the fixture of a keyring that has the record.
func (f *fakeSecretService) put(t *testing.T, user, value string) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.addLocked("Password for '"+user+"' on 'devctl'", attributes(user), []byte(value))
}

func (f *fakeSecretService) addLocked(label string, attributes map[string]string, value []byte) dbus.ObjectPath {
	f.next++
	path := dbus.ObjectPath(fmt.Sprintf("%s/%d", fakeCollectionPath, f.next))
	f.items[path] = fakeItem{label: label, attributes: attributes, value: value}
	_ = f.conn.ExportMethodTable(map[string]any{
		"GetSecret": func(session dbus.ObjectPath) (secret, *dbus.Error) {
			f.record("GetSecret " + string(session))
			f.mu.Lock()
			defer f.mu.Unlock()
			item, ok := f.items[path]
			if !ok {
				return secret{}, dbus.NewError("org.freedesktop.DBus.Error.UnknownObject", nil)
			}
			return secret{Session: session, Parameters: []byte{}, Value: item.value, ContentType: secretContentType}, nil
		},
		"Delete": func() (dbus.ObjectPath, *dbus.Error) {
			f.record("Delete " + string(path))
			f.mu.Lock()
			defer f.mu.Unlock()
			delete(f.items, path)
			if f.promptOnDelete {
				return fakePromptPath, nil
			}
			return nullPath, nil
		},
	}, path, ifaceItem)
	return path
}

func (f *fakeSecretService) snapshot() (items []fakeItem, calls []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, item := range f.items {
		items = append(items, item)
	}
	return items, append([]string(nil), f.calls...)
}

func hasCall(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestSecretServiceRoundTrip(t *testing.T) {
	f, s := newFakeSecretService(t, nil)

	if _, err := s.get(UserGitHub); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get of a missing item = %v, want ErrNotFound", err)
	}
	if err := s.delete(UserGitHub); err != nil {
		t.Fatalf("delete of a missing item = %v", err)
	}

	if err := s.set(UserGitHub, `{"token":"ghu_secret"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.set(UserGitHub, `{"token":"ghu_newer"}`); err != nil {
		t.Fatal(err)
	}
	items, calls := f.snapshot()
	if len(items) != 1 {
		t.Fatalf("two sets left %d items, want the second to replace the first", len(items))
	}
	if items[0].label != "Password for 'github' on 'devctl'" || items[0].attributes["service"] != Service || items[0].attributes["username"] != UserGitHub {
		t.Fatalf("stored item = %+v, want go-keyring's label and attributes", items[0])
	}
	if !hasCall(calls, "CreateItem replace=true session="+string(fakeSessionPath)) {
		t.Fatalf("CreateItem must replace and carry the session, calls: %v", calls)
	}
	if hasCall(calls, "Unlock") {
		t.Fatalf("an unlocked collection was unlocked: %v", calls)
	}
	if !hasCall(calls, "Session.Close") {
		t.Fatalf("the session was not closed: %v", calls)
	}

	got, err := s.get(UserGitHub)
	if err != nil || got != `{"token":"ghu_newer"}` {
		t.Fatalf("get = %q, %v", got, err)
	}

	if err := s.delete(UserGitHub); err != nil {
		t.Fatal(err)
	}
	if items, _ := f.snapshot(); len(items) != 0 {
		t.Fatalf("after delete: %d items", len(items))
	}
	if _, err := s.get(UserGitHub); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
}

func TestSecretServiceLockedCollection(t *testing.T) {
	f, s := newFakeSecretService(t, func(f *fakeSecretService) { f.locked = true })
	f.put(t, UserCircleCI, "ccipat_secret")

	got, err := s.get(UserCircleCI)
	if err != nil || got != "ccipat_secret" {
		t.Fatalf("get = %q, %v", got, err)
	}
	_, calls := f.snapshot()
	if !hasCall(calls, "Unlock ["+string(fakeCollectionPath)+"]") {
		t.Fatalf("Unlock must name the resolved collection, calls: %v", calls)
	}
	if !hasCall(calls, "Prompt") {
		t.Fatalf("the unlock prompt was not run: %v", calls)
	}
	f.mu.Lock()
	locked := f.locked
	f.mu.Unlock()
	if locked {
		t.Fatal("the collection stayed locked")
	}
}

func TestSecretServiceDismissedPrompt(t *testing.T) {
	f, s := newFakeSecretService(t, func(f *fakeSecretService) { f.locked, f.dismiss = true, true })

	_, err := s.get(UserGitHub)
	want := "keychain Unlock on " + string(fakeCollectionPath)
	if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "dismissed") {
		t.Fatalf("err = %v, want it to contain %q and the dismissal", err, want)
	}
	if _, calls := f.snapshot(); hasCall(calls, "SearchItems") {
		t.Fatalf("the search ran on a locked collection: %v", calls)
	}
}

func TestSecretServiceDeletePrompt(t *testing.T) {
	f, s := newFakeSecretService(t, func(f *fakeSecretService) { f.promptOnDelete = true })
	f.put(t, UserGitHub, "ghu_secret")

	if err := s.delete(UserGitHub); err != nil {
		t.Fatal(err)
	}
	items, calls := f.snapshot()
	if len(items) != 0 || !hasCall(calls, "Prompt") {
		t.Fatalf("items = %d, calls = %v", len(items), calls)
	}
}

func TestSecretServiceNoDefaultCollection(t *testing.T) {
	_, s := newFakeSecretService(t, func(f *fakeSecretService) { f.noDefault = true })

	_, err := s.get(UserGitHub)
	if err == nil || !strings.Contains(err.Error(), "keychain ReadAlias default on "+string(secretsPath)) || !strings.Contains(err.Error(), "no collection has this alias") {
		t.Fatalf("err = %v, want it to name the alias", err)
	}
}

func TestSecretServiceErrorNamesCallAndCollection(t *testing.T) {
	_, s := newFakeSecretService(t, func(f *fakeSecretService) { f.failSearch = true })

	_, err := s.get(UserGitHub)
	// The owner note in the parentheses is this test binary, the process
	// behind the fake's bus name.
	want := "keychain SearchItems on " + string(fakeCollectionPath) + " ("
	if err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), "the database is gone") {
		t.Fatalf("err = %v, want it to contain %q and the D-Bus error", err, want)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatal("a failed search must not read as a missing record")
	}
}

func TestOSKeychainIsSecretService(t *testing.T) {
	if _, ok := osKeychain().(secretService); !ok {
		t.Fatalf("osKeychain() = %T, want the Secret Service client", osKeychain())
	}
}
