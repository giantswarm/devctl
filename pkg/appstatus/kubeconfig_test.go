package appstatus

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

type kubeconfigFixture struct {
	Ambient       string
	SwitchAmbient bool
	Status        string
	LoginExit     int
	StatusExit    int
	Barrier       string
}

type kubeconfigCall struct {
	Args       []string
	Kubeconfig string
	Selected   string
	DirMode    os.FileMode
}

// These native substitutes only read/write synthetic cluster names in test
// files. They never run the installed tools or read a real kubeconfig.
func init() {
	if os.Getenv("DEVCTL_APPSTATUS_KUBECONFIG_HELPER") != "1" {
		return
	}
	if err := runKubeconfigHelper(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(99)
	}
	os.Exit(0)
}

func runKubeconfigHelper() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(executable)
	var fixture kubeconfigFixture
	if err := readKubeconfigJSON(filepath.Join(dir, "fixture.json"), &fixture); err != nil {
		return err
	}
	config := os.Getenv("KUBECONFIG")
	info, err := os.Stat(filepath.Dir(config)) // #nosec G703 -- the parent sets only a synthetic kubeconfig path
	if err != nil {
		return err
	}
	call := kubeconfigCall{Args: os.Args[1:], Kubeconfig: config, DirMode: info.Mode().Perm()}
	name := strings.TrimSuffix(filepath.Base(executable), ".exe")
	switch name {
	case "tsh":
		if len(call.Args) != 3 || call.Args[0] != "kube" || call.Args[1] != "login" {
			return fmt.Errorf("unexpected login arguments %q", call.Args)
		}
		call.Selected = call.Args[2]
		// A failed login may have written part of its kubeconfig already.
		if err := os.WriteFile(config, []byte(call.Selected), 0o600); err != nil { // #nosec G703 -- synthetic cluster selection only
			return err
		}
		if err := writeKubeconfigJSON(filepath.Join(dir, "login-"+call.Selected+".json"), call); err != nil {
			return err
		}
		os.Exit(fixture.LoginExit)
	case "kubectl":
		if fixture.Barrier != "" {
			// Both logins finish before either read resolves current-context.
			conn, err := net.DialTimeout("tcp", fixture.Barrier, time.Second)
			if err != nil {
				return err
			}
			_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
			_, err = conn.Read(make([]byte, 1))
			_ = conn.Close()
			if err != nil {
				return err
			}
		}
		if fixture.SwitchAmbient {
			// Model a different terminal switching the ambient context just
			// before kubectl loads it. The private config must be unaffected.
			if err := os.WriteFile(fixture.Ambient, []byte("other-cluster"), 0o600); err != nil {
				return err
			}
		}
		selected, err := os.ReadFile(config) // #nosec G304 G703 -- synthetic file written by this fixture's tsh
		if err != nil {
			return err
		}
		call.Selected = string(selected)
		if err := writeKubeconfigJSON(filepath.Join(dir, "poll-"+call.Selected+".json"), call); err != nil {
			return err
		}
		status := fixture.Status
		if fixture.SwitchAmbient && call.Selected == "other-cluster" {
			status = "deployed"
		}
		_, _ = fmt.Fprint(os.Stdout, status)
		os.Exit(fixture.StatusExit)
	default:
		return fmt.Errorf("unexpected helper name %q", name)
	}
	return nil
}

func readKubeconfigJSON(path string, value any) error {
	data, err := os.ReadFile(path) // #nosec G304 G703 -- test-owned fixture and call records only
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func writeKubeconfigJSON(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600) // #nosec G703 -- test-owned fixture and call records only
}

func installKubeconfigHelpers(t *testing.T, fixture kubeconfigFixture) (string, string) {
	t.Helper()
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable) // #nosec G304 G703 -- only the running test executable
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"tsh", "kubectl"} {
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o700); err != nil { // #nosec G306 G703 -- executable fixture in t.TempDir
			t.Fatal(err)
		}
	}
	fixture.Ambient = filepath.Join(dir, "ambient-config")
	if err := os.WriteFile(fixture.Ambient, []byte("original-cluster"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeKubeconfigJSON(filepath.Join(dir, "fixture.json"), fixture); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("KUBECONFIG", fixture.Ambient)
	t.Setenv("DEVCTL_APPSTATUS_KUBECONFIG_HELPER", "1")
	return dir, fixture.Ambient
}

func kubeconfigClient(t *testing.T) *Client {
	t.Helper()
	logger := logrus.New()
	logger.SetOutput(io.Discard)
	client, err := New(Config{Logger: logger, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func checkPrivateKubeconfig(t *testing.T, dir, ambient, cluster string, polled bool) string {
	t.Helper()
	var login kubeconfigCall
	if err := readKubeconfigJSON(filepath.Join(dir, "login-"+cluster+".json"), &login); err != nil {
		t.Fatal(err)
	}
	if login.Kubeconfig == "" || login.Kubeconfig == ambient {
		t.Errorf("login used the ambient kubeconfig instead of an invocation-owned file")
		return login.Kubeconfig
	}
	if runtime.GOOS != "windows" && login.DirMode != 0o700 {
		t.Errorf("private directory mode: want 0700, got %04o", login.DirMode)
	}
	if polled {
		var poll kubeconfigCall
		if err := readKubeconfigJSON(filepath.Join(dir, "poll-"+cluster+".json"), &poll); err != nil {
			t.Fatal(err)
		}
		wantArgs := []string{"get", "app", "workload-example", "-n", "org-example", "-o", "jsonpath={.status.release.status}"}
		if poll.Kubeconfig != login.Kubeconfig || poll.Selected != cluster || !reflect.DeepEqual(poll.Args, wantArgs) {
			t.Errorf("poll did not retain the login's cluster, kubeconfig and App arguments: %+v", poll)
		}
	}
	if _, err := os.Stat(filepath.Dir(login.Kubeconfig)); !os.IsNotExist(err) { // #nosec G703 -- path recorded by the test's helper
		t.Errorf("private kubeconfig directory was not removed on return: %v", err)
	}
	return login.Kubeconfig
}

func TestWaitForAppDeploymentKubeconfigLifecycle(t *testing.T) {
	cases := []struct {
		name    string
		fixture kubeconfigFixture
		wantErr bool
	}{
		{name: "success", fixture: kubeconfigFixture{Status: "deployed"}},
		{name: "partial login failure", fixture: kubeconfigFixture{LoginExit: 7}, wantErr: true},
		{name: "pending timeout", fixture: kubeconfigFixture{Status: "pending-install"}, wantErr: true},
		{name: "failed read timeout", fixture: kubeconfigFixture{Status: "deployed", StatusExit: 1}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, ambient := installKubeconfigHelpers(t, tc.fixture)
			client := kubeconfigClient(t)
			timeout := 150 * time.Millisecond
			if !tc.wantErr {
				timeout = 5 * time.Second
			}
			err := client.WaitForAppDeployment(t.Context(), "workload-example", "org-example", "target-cluster", timeout)
			if (err != nil) != tc.wantErr {
				t.Errorf("want failure %t, got %v", tc.wantErr, err)
			}
			first := checkPrivateKubeconfig(t, dir, ambient, "target-cluster", tc.fixture.LoginExit == 0)
			if !tc.wantErr {
				if err := client.WaitForAppDeployment(t.Context(), "workload-example", "org-example", "second-cluster", 5*time.Second); err != nil {
					t.Fatal(err)
				}
				second := checkPrivateKubeconfig(t, dir, ambient, "second-cluster", true)
				if first == second {
					t.Error("sequential invocations reused the same kubeconfig")
				}
			}
			content, err := os.ReadFile(ambient) // #nosec G304 G703 -- synthetic ambient kubeconfig in t.TempDir
			if err != nil || string(content) != "original-cluster" {
				t.Errorf("ambient kubeconfig changed: %q, %v", content, err)
			}
		})
	}
}

func TestWaitForAppDeploymentDoesNotFollowContextSwitch(t *testing.T) {
	dir, ambient := installKubeconfigHelpers(t, kubeconfigFixture{SwitchAmbient: true, Status: "pending-install"})
	err := kubeconfigClient(t).WaitForAppDeployment(t.Context(), "workload-example", "org-example", "target-cluster", 150*time.Millisecond)
	if err == nil {
		t.Error("a deployed App on other-cluster satisfied the pending App on target-cluster")
	}
	checkPrivateKubeconfig(t, dir, ambient, "target-cluster", true)
	var other kubeconfigCall
	if err := readKubeconfigJSON(filepath.Join(dir, "poll-other-cluster.json"), &other); !os.IsNotExist(err) {
		t.Errorf("poll followed the ambient context switch: %v", err)
	}
}

func TestWaitForAppDeploymentConcurrentClusters(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		var connections []net.Conn
		defer func() {
			for _, conn := range connections {
				_ = conn.Close()
			}
		}()
		for range 2 {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections = append(connections, conn)
		}
		for _, conn := range connections {
			_ = conn.SetWriteDeadline(time.Now().Add(time.Second))
			_, _ = conn.Write([]byte{1})
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	dir, ambient := installKubeconfigHelpers(t, kubeconfigFixture{Status: "deployed", Barrier: listener.Addr().String()})
	client := kubeconfigClient(t)
	ctx := t.Context()
	results := make(chan error, 2)
	for _, cluster := range []string{"first-cluster", "second-cluster"} {
		go func() {
			results <- client.WaitForAppDeployment(ctx, "workload-example", "org-example", cluster, 5*time.Second)
		}()
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("concurrent wait failed: %v", err)
		}
	}
	first := checkPrivateKubeconfig(t, dir, ambient, "first-cluster", true)
	second := checkPrivateKubeconfig(t, dir, ambient, "second-cluster", true)
	if first == second {
		t.Error("concurrent waits shared a kubeconfig")
	}
}

func TestWaitForAppDeploymentRejectsNamesBeforeLogin(t *testing.T) {
	cases := []struct{ app, namespace, cluster string }{
		{app: "--all", namespace: "org-example", cluster: "target-cluster"},
		{app: "workload-example", namespace: "--all-namespaces", cluster: "target-cluster"},
		{app: "workload-example", namespace: "org-example", cluster: "--proxy=example.invalid"},
		{app: "../../other", namespace: "org-example", cluster: "target-cluster"},
		{app: "workload-example", namespace: "org-example", cluster: ""},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			dir, ambient := installKubeconfigHelpers(t, kubeconfigFixture{Status: "deployed"})
			err := kubeconfigClient(t).WaitForAppDeployment(t.Context(), tc.app, tc.namespace, tc.cluster, time.Second)
			if err == nil {
				t.Fatal("unsafe identifier was accepted")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), "login-") || strings.HasPrefix(entry.Name(), "poll-") {
					t.Error("validation ran after a subprocess")
				}
			}
			content, err := os.ReadFile(ambient) // #nosec G304 G703 -- synthetic ambient kubeconfig in t.TempDir
			if err != nil || string(content) != "original-cluster" {
				t.Errorf("invalid input changed the ambient kubeconfig: %q, %v", content, err)
			}
		})
	}
}
