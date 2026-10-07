//go:build linux

package authstore

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

// The Secret Service API, https://specifications.freedesktop.org/secret-service-spec/latest/.
const (
	secretsBusName  = "org.freedesktop.secrets"
	secretsPath     = dbus.ObjectPath("/org/freedesktop/secrets")
	ifaceService    = "org.freedesktop.Secret.Service"
	ifaceCollection = "org.freedesktop.Secret.Collection"
	ifaceItem       = "org.freedesktop.Secret.Item"
	ifaceSession    = "org.freedesktop.Secret.Session"
	ifacePrompt     = "org.freedesktop.Secret.Prompt"
	// defaultAlias names the collection the Secret Service unlocks at login.
	defaultAlias = "default"
	// nullPath is the object path that stands for "none": no prompt needed, no
	// collection behind an alias.
	nullPath = dbus.ObjectPath("/")
	// plainSession: the secrets cross the session bus unencrypted, a socket
	// only this user reaches.
	plainSession = "plain"
	// secretContentType is libsecret's. go-keyring's "text/plain;
	// charset=utf8" names a charset that does not exist and oo7-daemon
	// refuses it; the content type is not checked on read, so records written
	// with it stay readable.
	secretContentType = "text/plain"
)

// secret is org.freedesktop.Secret.Item's secret struct, (oayays) on the wire.
type secret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

// secretService is the keychain on Linux: the Secret Service of the session
// bus (GNOME Keyring, KWallet, oo7-daemon, KeePassXC), spoken directly over
// D-Bus, one private connection per operation. The records are go-keyring's,
// attributes service and username, so records written before stay readable.
type secretService struct {
	// dial is the bus connection and the destination bus name; nil is the
	// session bus and org.freedesktop.secrets.
	dial func() (*dbus.Conn, string, error)
}

func (s secretService) get(user string) (string, error) {
	var value string
	err := s.with(func(c *secretServiceClient, collection dbus.ObjectPath) error {
		items, err := c.search(collection, user)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			return ErrNotFound
		}
		value, err = c.getSecret(items[0])
		return err
	})
	return value, err
}

func (s secretService) set(user, value string) error {
	return s.with(func(c *secretServiceClient, collection dbus.ObjectPath) error {
		return c.createItem(collection, user, value)
	})
}

func (s secretService) delete(user string) error {
	return s.with(func(c *secretServiceClient, collection dbus.ObjectPath) error {
		items, err := c.search(collection, user)
		if err != nil {
			return err
		}
		for _, item := range items {
			if err := c.deleteItem(item); err != nil {
				return err
			}
		}
		return nil
	})
}

// with runs fn against the unlocked default collection of a fresh session.
func (s secretService) with(fn func(c *secretServiceClient, collection dbus.ObjectPath) error) error {
	dial := s.dial
	if dial == nil {
		dial = dialSecretService
	}
	conn, dest, err := dial()
	if err != nil {
		return err
	}
	c := &secretServiceClient{conn: conn, dest: dest}
	defer c.close()
	if err := c.openSession(); err != nil {
		return err
	}
	collection, err := c.defaultCollection()
	if err != nil {
		return err
	}
	return fn(c, collection)
}

func dialSecretService() (*dbus.Conn, string, error) {
	conn, err := connectSessionBus()
	if err != nil {
		return nil, "", fmt.Errorf("connecting to the session bus for the keychain: %w", err)
	}
	return conn, secretsBusName, nil
}

// connectSessionBus is a private connection to the session bus of the
// environment. No bus is started for a shell without one: it would carry no
// Secret Service.
func connectSessionBus() (*dbus.Conn, error) {
	conn, err := dbus.SessionBusPrivateNoAutoStartup()
	if err != nil {
		return nil, err
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

// secretServiceClient is one session with one Secret Service.
type secretServiceClient struct {
	conn    *dbus.Conn
	dest    string
	session dbus.ObjectPath
	// owner is the process behind dest, resolved for the first error.
	owner string
}

func (c *secretServiceClient) service() dbus.BusObject {
	return c.conn.Object(c.dest, secretsPath)
}

func (c *secretServiceClient) openSession() error {
	var output dbus.Variant
	err := c.service().Call(ifaceService+".OpenSession", 0, plainSession, dbus.MakeVariant("")).Store(&output, &c.session)
	if err != nil {
		return c.fail("OpenSession", secretsPath, err)
	}
	return nil
}

func (c *secretServiceClient) close() {
	if c.session != "" {
		_ = c.conn.Object(c.dest, c.session).Call(ifaceSession+".Close", 0).Err
	}
	_ = c.conn.Close()
}

// defaultCollection is the collection behind the default alias, unlocked. A
// missing default collection is an error, never created here: which keyring
// holds the login secrets is the person's choice in the keychain application.
func (c *secretServiceClient) defaultCollection() (dbus.ObjectPath, error) {
	readAlias := "ReadAlias " + defaultAlias
	var collection dbus.ObjectPath
	if err := c.service().Call(ifaceService+".ReadAlias", 0, defaultAlias).Store(&collection); err != nil {
		return "", c.fail(readAlias, secretsPath, err)
	}
	if collection == nullPath {
		return "", c.fail(readAlias, secretsPath, errors.New("no collection has this alias: create a default keyring in the keychain application"))
	}
	var locked bool
	if err := c.conn.Object(c.dest, collection).StoreProperty(ifaceCollection+".Locked", &locked); err != nil {
		return "", c.fail("Get Locked", collection, err)
	}
	if !locked {
		return collection, nil
	}
	// Unlock takes the resolved collection: GNOME Keyring tolerates the alias
	// path here, oo7-daemon and KeePassXC do not.
	var unlocked []dbus.ObjectPath
	var prompt dbus.ObjectPath
	if err := c.service().Call(ifaceService+".Unlock", 0, []dbus.ObjectPath{collection}).Store(&unlocked, &prompt); err != nil {
		return "", c.fail("Unlock", collection, err)
	}
	if err := c.completePrompt("Unlock", collection, prompt); err != nil {
		return "", err
	}
	return collection, nil
}

func (c *secretServiceClient) search(collection dbus.ObjectPath, user string) ([]dbus.ObjectPath, error) {
	var items []dbus.ObjectPath
	err := c.conn.Object(c.dest, collection).Call(ifaceCollection+".SearchItems", 0, attributes(user)).Store(&items)
	if err != nil {
		return nil, c.fail("SearchItems", collection, err)
	}
	return items, nil
}

func (c *secretServiceClient) getSecret(item dbus.ObjectPath) (string, error) {
	var s secret
	if err := c.conn.Object(c.dest, item).Call(ifaceItem+".GetSecret", 0, c.session).Store(&s); err != nil {
		return "", c.fail("GetSecret", item, err)
	}
	return string(s.Value), nil
}

// createItem writes the record, replacing the item with the same attributes.
func (c *secretServiceClient) createItem(collection dbus.ObjectPath, user, value string) error {
	properties := map[string]dbus.Variant{
		ifaceItem + ".Label":      dbus.MakeVariant(fmt.Sprintf("Password for '%s' on '%s'", user, Service)),
		ifaceItem + ".Attributes": dbus.MakeVariant(attributes(user)),
	}
	s := secret{Session: c.session, Parameters: []byte{}, Value: []byte(value), ContentType: secretContentType}
	var item, prompt dbus.ObjectPath
	if err := c.conn.Object(c.dest, collection).Call(ifaceCollection+".CreateItem", 0, properties, s, true).Store(&item, &prompt); err != nil {
		return c.fail("CreateItem", collection, err)
	}
	return c.completePrompt("CreateItem", collection, prompt)
}

func (c *secretServiceClient) deleteItem(item dbus.ObjectPath) error {
	var prompt dbus.ObjectPath
	if err := c.conn.Object(c.dest, item).Call(ifaceItem+".Delete", 0).Store(&prompt); err != nil {
		return c.fail("Delete", item, err)
	}
	return c.completePrompt("Delete", item, prompt)
}

// completePrompt runs the prompt a call returned and waits for the person to
// finish it; a dismissed prompt fails the call. nullPath is no prompt.
func (c *secretServiceClient) completePrompt(call string, on, prompt dbus.ObjectPath) error {
	if prompt == nullPath {
		return nil
	}
	completed := ifacePrompt + ".Completed"
	match := []dbus.MatchOption{dbus.WithMatchObjectPath(prompt), dbus.WithMatchInterface(ifacePrompt), dbus.WithMatchMember("Completed")}
	if err := c.conn.AddMatchSignal(match...); err != nil {
		return c.fail(call, on, err)
	}
	defer func() { _ = c.conn.RemoveMatchSignal(match...) }()
	signals := make(chan *dbus.Signal, 16)
	c.conn.Signal(signals)
	defer c.conn.RemoveSignal(signals)

	if err := c.conn.Object(c.dest, prompt).Call(ifacePrompt+".Prompt", 0, "").Err; err != nil {
		return c.fail(call, on, fmt.Errorf("prompting: %w", err))
	}
	for signal := range signals {
		if signal.Path != prompt || signal.Name != completed {
			continue
		}
		if dismissed, _ := signal.Body[0].(bool); len(signal.Body) > 0 && dismissed {
			return c.fail(call, on, errors.New("the prompt was dismissed"))
		}
		return nil
	}
	return c.fail(call, on, errors.New("the bus connection closed before the prompt completed"))
}

// fail is the error of one call: "keychain <call> on <path> (<owner>): <err>",
// the owner being the process that provides the Secret Service when the bus
// tells, so a person knows which keychain application to look at.
func (c *secretServiceClient) fail(call string, on dbus.ObjectPath, err error) error {
	return fmt.Errorf("keychain %s on %s%s: %w", call, on, c.ownerNote(), err)
}

func (c *secretServiceClient) ownerNote() string {
	if c.owner == "" {
		c.owner = c.resolveOwner()
	}
	if c.owner == "" {
		return ""
	}
	return " (" + c.owner + ")"
}

// resolveOwner is the command name of the process owning dest, its unique bus
// name when the process is not readable, empty when the name has no owner.
func (c *secretServiceClient) resolveOwner() string {
	bus := c.conn.BusObject()
	var unique string
	if err := bus.Call("org.freedesktop.DBus.GetNameOwner", 0, c.dest).Store(&unique); err != nil {
		return ""
	}
	var pid uint32
	if err := bus.Call("org.freedesktop.DBus.GetConnectionUnixProcessID", 0, unique).Store(&pid); err != nil {
		return unique
	}
	comm, err := os.ReadFile("/proc/" + strconv.FormatUint(uint64(pid), 10) + "/comm")
	if err != nil {
		return unique
	}
	return strings.TrimSpace(string(comm))
}

// attributes identify one record: go-keyring's, so `secret-tool lookup
// service devctl username <user>` and records written before both match.
func attributes(user string) map[string]string {
	return map[string]string{"service": Service, "username": user}
}
