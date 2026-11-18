package handler

import (
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// N3 of docs/decisions/0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md: Register and
// addTool read h.InternalTools, which OnConfigReloaded replaces under toolsMu, and the config watcher is already
// running when Register is called. One goroutine replaces the inventory while Register runs repeatedly. Meaningful
// under -race only.
//
// The test serves no request while it registers: it opens no session, calls neither the tools/list middleware nor
// align_tools, and starts no reader of the inventory's schema maps. That matters because registration writes into
// those maps in place (util.SanitizeToolSchema, see addTool), and a reader running at the same time would race with it
// by a different race that is documented and left alone. The writer goroutine only unmarshals a fresh inventory and
// publishes it under the lock, and Register copies the slice header under the same lock, so the only two parties are
// ordered by it and the test cannot fail on the map writes. One writer, because concurrent OnConfigReloaded calls race
// with each other in syncSearchGates, which is not what this test asks.
func TestRegisterWhileConfigReloads(t *testing.T) {
	e := newLSEnv(t)
	h := e.h
	captureLog(t) // OnConfigReloaded and registration log at info level

	stop := make(chan struct{})
	var writer sync.WaitGroup
	writer.Add(1)
	go func() {
		defer writer.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			h.OnConfigReloaded(h.Config)
		}
	}()
	for range 20 {
		s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
		h.Register(s)
		h.RegisterPipelineTools(s)
		h.RegisterPrompts(s)
	}
	close(stop)
	writer.Wait()
}
