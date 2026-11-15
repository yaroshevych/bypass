package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	runpkg "bypass/internal/run"
	"bypass/internal/run/runtest"
	"bypass/internal/vault"
	tea "github.com/charmbracelet/bubbletea"
)

func TestRunStalledClientDoesNotBlockUpdateOrGrantSession(t *testing.T) {
	for _, accept := range []bool{false, true} {
		name := "disconnect"
		if accept {
			name = "accept"
		}
		t.Run(name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "bt-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			path := filepath.Join(dir, "run.sock")
			incoming := make(chan *runpkg.Pending, 1)
			server, err := runpkg.Listen(path, func(p *runpkg.Pending) { incoming <- p })
			if err != nil {
				t.Fatal(err)
			}
			defer server.Close()
			client, err := runtest.Connect(path)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			p := <-incoming
			p.Identity = runpkg.Identity{Name: "agent", Digest: "test"}
			m := New(Options{})
			defer m.Close()
			db := &vault.Database{Path: "/vault", RootUUID: "root"}
			m.ws.Open(db)
			session := m.ws.ActiveSession()
			targets := []runTarget{{ref: "ref", session: session.ID, entry: "entry", field: "Password", database: db}}
			m.runs = &runState{pending: []*runpkg.Pending{p}}
			secrets := newSecretValues(p.Context)
			secrets.Put("ref", []byte("synthetic"))
			type updateResult struct {
				cmd     tea.Cmd
				handled bool
			}
			updates := make(chan updateResult, 1)
			go func() {
				cmd, handled := m.updateRun(runResult{pending: p, targets: targets, secrets: secrets, mode: "s"})
				updates <- updateResult{cmd, handled}
			}()
			var update updateResult
			select {
			case update = <-updates:
			case <-time.After(time.Second):
				client.Close()
				<-updates
				t.Fatal("TUI update blocked waiting for the client")
			}
			if !update.handled || update.cmd == nil {
				t.Fatal("reply was not scheduled")
			}
			completed := make(chan runReplyResult, 1)
			go func() { completed <- update.cmd().(runReplyResult) }()
			if err := runtest.ReadDecision(client); err != nil {
				t.Fatal(err)
			}
			select {
			case <-completed:
				t.Fatal("reply finished before acknowledgement")
			default:
			}
			if len(m.runs.grants) != 0 {
				t.Fatal("grant recorded before acknowledgement")
			}
			_, handled := m.updateRun(RunDisconnectedMsg{})
			if !handled {
				t.Fatal("TUI did not process another update")
			}
			if accept {
				if _, err := client.Write([]byte{1}); err != nil {
					t.Fatal(err)
				}
			} else {
				client.Close()
			}
			select {
			case result := <-completed:
				m.updateRun(result)
			case <-time.After(time.Second):
				t.Fatal("reply did not finish")
			}
			if got := len(m.runs.grants); (got == 1) != accept {
				t.Fatalf("got %d grants, accepted=%v", got, accept)
			}
		})
	}
}
