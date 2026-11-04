package merge

import (
	"io"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/giantswarm/devctl/v8/e2e/mock/sequence"
	"github.com/giantswarm/devctl/v8/pkg/agentcli"
	"github.com/giantswarm/devctl/v8/pkg/mergejob"
)

const childHoldEnv = "DEVCTL_TEST_DETACHED_CHILD_HOLD"

func TestDetachSerializesConcurrentStarts(t *testing.T) {
	f := &flag{Timeout: time.Minute, NoReleaseWait: true, Detach: true}
	first, firstOut, root := detachRunner(t, f)
	second, secondOut, _, _ := newRunner(t, sequence.Routes{}, loggedIn,
		&flag{Timeout: time.Minute, NoReleaseWait: true, Detach: true})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close() //nolint:errcheck // test listener
	t.Setenv(childHoldEnv, listener.Addr().String())

	firstStarting, releaseFirst := make(chan struct{}), make(chan struct{})
	first.executable = func() (string, error) {
		close(firstStarting)
		<-releaseFirst
		return os.Args[0], nil
	}
	secondRoot, secondStarting := make(chan struct{}), make(chan struct{})
	second.jobRoot = func() (string, error) {
		close(secondRoot)
		return root, nil
	}
	second.executable = func() (string, error) {
		close(secondStarting)
		return os.Args[0], nil
	}
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { firstDone <- first.run(t.Context(), []string{"o/r", "42"}) }()
	select {
	case <-firstStarting:
	case <-time.After(5 * time.Second):
		close(releaseFirst)
		t.Fatal("first start did not reach process creation")
	}
	go func() { secondDone <- second.run(t.Context(), []string{"o/r", "42"}) }()
	<-secondRoot
	// The first launcher has checked for an existing job but has not started
	// or recorded its child. The second must wait for that record before
	// deciding whether another merge may start.
	select {
	case <-secondStarting:
	case <-time.After(time.Second):
	}
	close(releaseFirst)
	firstErr, secondErr := <-firstDone, <-secondDone
	jobs, err := mergejob.List(root)
	if err != nil {
		t.Fatal(err)
	}
	// Release the synthetic children and wait for their documents through
	// their connections, including both children on the broken implementation.
	for range jobs {
		if err := listener.(*net.TCPListener).SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		conn, err := listener.(*net.TCPListener).AcceptTCP()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		_ = conn.CloseWrite()
		_, err = io.Copy(io.Discard, conn)
		_ = conn.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if agentcli.Exit(firstErr) != agentcli.ExitOK || agentcli.Exit(secondErr) != agentcli.ExitNotApplicable || len(jobs) != 1 {
		t.Fatalf("concurrent starts: first exit %d, second exit %d, jobs %d; documents: %s %s",
			agentcli.Exit(firstErr), agentcli.Exit(secondErr), len(jobs), firstOut, secondOut)
	}
	if !strings.Contains(secondErr.Error(), jobs[0].Handle) {
		t.Errorf("duplicate refusal does not name the active job: %v", secondErr)
	}
}
