package cli

import (
	"path/filepath"
	"strings"
	"sync"

	"reasonix/internal/session"
)

// cliSessionServices is the process host registry shared by chat, run, serve,
// and ACP. Boot receives an existing service and never opens a second writer
// registry for the same root.
var cliSessionServices = struct {
	sync.Mutex
	byRoot map[string]*session.Service
}{byRoot: map[string]*session.Service{}}

func cliSessionService(sessionDir string) *session.Service {
	return cliSessionServiceForRoot(session.RootForLegacyDir(sessionDir))
}

func cliSessionServiceForRoot(root string) *session.Service {
	root = filepath.Clean(strings.TrimSpace(root))
	if root == "" || root == "." {
		return nil
	}
	cliSessionServices.Lock()
	defer cliSessionServices.Unlock()
	if service := cliSessionServices.byRoot[root]; service != nil {
		return service
	}
	service, err := session.NewService("local", session.NewFilesystemPersistence(root))
	if err != nil {
		return nil
	}
	cliSessionServices.byRoot[root] = service
	return service
}
