package client

import (
	"github.com/wlxwlxwlx/mcp-go/client/transport"
	"github.com/wlxwlxwlx/mcp-go/server"
)

// NewInProcessClient connect directly to a mcp server object in the same process
func NewInProcessClient(server *server.MCPServer) (*Client, error) {
	inProcessTransport := transport.NewInProcessTransport(server)
	return NewClient(inProcessTransport), nil
}
