package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunApplicationGracefulShutdown(t *testing.T) {
	port := reserveFreePort(t)
	dbPath := t.TempDir() + "/kiro.db"
	args := []string{
		"server",
		fmt.Sprintf("--server.port=%d", port),
		"--server.host=127.0.0.1",
		"--server.admin-api-key=admin-key",
		"--server.proxy-api-key=proxy-key",
		fmt.Sprintf("--storage.sqlite-path=%s", dbPath),
	}

	app, helpShown, err := buildApplication(context.Background(), args, ioDiscard())
	require.NoError(t, err)
	require.False(t, helpShown)
	t.Cleanup(func() { require.NoError(t, app.close()) })

	sigCh := make(chan os.Signal, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- runApplication(context.Background(), app, sigCh)
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	require.Eventually(t, func() bool {
		resp, err := http.Get(url)
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond)

	sigCh <- syscall.SIGTERM

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("graceful shutdown timed out")
	}
}

func TestRunMainReturnsStartupFailureWhenPortIsInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	port := listener.Addr().(*net.TCPAddr).Port

	var stderr bytes.Buffer
	exitCode := runMain(context.Background(), []string{
		fmt.Sprintf("--server.port=%d", port),
		"--server.host=127.0.0.1",
		"--server.admin-api-key=admin-key",
		"--server.proxy-api-key=proxy-key",
		fmt.Sprintf("--storage.sqlite-path=%s", t.TempDir()+"/kiro.db"),
	}, make(chan os.Signal, 1), &stderr)

	require.Equal(t, exitStartupFailure, exitCode)
	require.Contains(t, stderr.String(), "address already in use")
}

func reserveFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func ioDiscard() *bytes.Buffer {
	return &bytes.Buffer{}
}
