package server

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wlxwlxwlx/mcp-go/mcp"
)

// sessionTestClient implements the basic ClientSession interface for testing
type sessionTestClient struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
}

func (f sessionTestClient) SessionID() string {
	return f.sessionID
}

func (f sessionTestClient) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

// Initialize marks the session as initialized
// This implementation properly sets the initialized flag to true
// as required by the interface contract
func (f *sessionTestClient) Initialize() {
	f.initialized = true
}

// Initialized returns whether the session has been initialized
func (f sessionTestClient) Initialized() bool {
	return f.initialized
}

// sessionTestClientWithTools implements the SessionWithTools interface for testing
type sessionTestClientWithTools struct {
	sessionID           string
	notificationChannel chan mcp.JSONRPCNotification
	initialized         bool
	sessionTools        map[string]ServerTool
	mu                  sync.RWMutex // Mutex to protect concurrent access to sessionTools
}

func (f *sessionTestClientWithTools) SessionID() string {
	return f.sessionID
}

func (f *sessionTestClientWithTools) NotificationChannel() chan<- mcp.JSONRPCNotification {
	return f.notificationChannel
}

func (f *sessionTestClientWithTools) Initialize() {
	f.initialized = true
}

func (f *sessionTestClientWithTools) Initialized() bool {
	return f.initialized
}

func (f *sessionTestClientWithTools) GetSessionTools() map[string]ServerTool {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Return a copy of the map to prevent concurrent modification
	if f.sessionTools == nil {
		return nil
	}

	toolsCopy := make(map[string]ServerTool, len(f.sessionTools))
	for k, v := range f.sessionTools {
		toolsCopy[k] = v
	}
	return toolsCopy
}

func (f *sessionTestClientWithTools) SetSessionTools(tools map[string]ServerTool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Create a copy of the map to prevent concurrent modification
	if tools == nil {
		f.sessionTools = nil
		return
	}

	toolsCopy := make(map[string]ServerTool, len(tools))
	for k, v := range tools {
		toolsCopy[k] = v
	}
	f.sessionTools = toolsCopy
}

// Verify that both implementations satisfy their respective interfaces
var _ ClientSession = &sessionTestClient{}
var _ SessionWithTools = &sessionTestClientWithTools{}

func TestSessionWithTools_Integration(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))

	// Create session-specific tools
	sessionTool := ServerTool{
		Tool: mcp.NewTool("session-tool"),
		Handler: func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("session-tool result"), nil
		},
	}

	// Create a session with tools
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"session-tool": sessionTool,
		},
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Test that we can access the session-specific tool
	testReq := mcp.CallToolRequest{}
	testReq.Params.Name = "session-tool"
	testReq.Params.Arguments = map[string]interface{}{}

	// Call using session context
	sessionCtx := server.WithContext(context.Background(), session)

	// Check if the session was stored in the context correctly
	s := ClientSessionFromContext(sessionCtx)
	require.NotNil(t, s, "Session should be available from context")
	assert.Equal(t, session.SessionID(), s.SessionID(), "Session ID should match")

	// Check if the session can be cast to SessionWithTools
	swt, ok := s.(SessionWithTools)
	require.True(t, ok, "Session should implement SessionWithTools")

	// Check if the tools are accessible
	tools := swt.GetSessionTools()
	require.NotNil(t, tools, "Session tools should be available")
	require.Contains(t, tools, "session-tool", "Session should have session-tool")

	// Test session tool access with session context
	t.Run("test session tool access", func(t *testing.T) {
		// First test directly getting the tool from session tools
		tool, exists := tools["session-tool"]
		require.True(t, exists, "Session tool should exist in the map")
		require.NotNil(t, tool, "Session tool should not be nil")

		// Now test calling directly with the handler
		result, err := tool.Handler(sessionCtx, testReq)
		require.NoError(t, err, "No error calling session tool handler directly")
		require.NotNil(t, result, "Result should not be nil")
		require.Len(t, result.Content, 1, "Result should have one content item")

		textContent, ok := result.Content[0].(mcp.TextContent)
		require.True(t, ok, "Content should be TextContent")
		assert.Equal(t, "session-tool result", textContent.Text, "Result text should match")
	})
}

func TestMCPServer_ToolsWithSessionTools(t *testing.T) {
	// Basic test to verify that session-specific tools are returned correctly in a tools list
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))

	// Add global tools
	server.AddTools(
		ServerTool{Tool: mcp.NewTool("global-tool-1")},
		ServerTool{Tool: mcp.NewTool("global-tool-2")},
	)

	// Create a session with tools
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"session-tool-1": {Tool: mcp.NewTool("session-tool-1")},
			"global-tool-1":  {Tool: mcp.NewTool("global-tool-1", mcp.WithDescription("Overridden"))},
		},
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// List tools with session context
	sessionCtx := server.WithContext(context.Background(), session)
	header := map[string]string{"Authorization": "Bearer test"}
	resp := server.HandleMessage(sessionCtx, header, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/list"
	}`))

	jsonResp, ok := resp.(mcp.JSONRPCResponse)
	require.True(t, ok, "Response should be a JSONRPCResponse")

	result, ok := jsonResp.Result.(mcp.ListToolsResult)
	require.True(t, ok, "Result should be a ListToolsResult")

	// Should have 3 tools - 2 global tools (one overridden) and 1 session-specific tool
	assert.Len(t, result.Tools, 3, "Should have 3 tools")

	// Find the overridden tool and verify its description
	var found bool
	for _, tool := range result.Tools {
		if tool.Name == "global-tool-1" {
			assert.Equal(t, "Overridden", tool.Description, "Global tool should be overridden")
			found = true
			break
		}
	}
	assert.True(t, found, "Should find the overridden global tool")
}

func TestMCPServer_AddSessionTools(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
	ctx := context.Background()

	// Create a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session-specific tools
	err = server.AddSessionTools(session.SessionID(),
		ServerTool{Tool: mcp.NewTool("session-tool")},
	)
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/tools/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify tool was added to session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.Contains(t, session.GetSessionTools(), "session-tool")
}

func TestMCPServer_AddSessionTool(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
	ctx := context.Background()

	// Create a session
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Add session-specific tool using the new helper method
	err = server.AddSessionTool(
		session.SessionID(),
		mcp.NewTool("session-tool-helper"),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return mcp.NewToolResultText("helper result"), nil
		},
	)
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/tools/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify tool was added to session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.Contains(t, session.GetSessionTools(), "session-tool-helper")
}

func TestMCPServer_DeleteSessionTools(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0", WithToolCapabilities(true))
	ctx := context.Background()

	// Create a session with tools
	sessionChan := make(chan mcp.JSONRPCNotification, 10)
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: sessionChan,
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"session-tool-1": {
				Tool: mcp.NewTool("session-tool-1"),
			},
			"session-tool-2": {
				Tool: mcp.NewTool("session-tool-2"),
			},
		},
	}

	// Register the session
	err := server.RegisterSession(ctx, session)
	require.NoError(t, err)

	// Delete one of the session tools
	err = server.DeleteSessionTools(session.SessionID(), "session-tool-1")
	require.NoError(t, err)

	// Check that notification was sent
	select {
	case notification := <-sessionChan:
		assert.Equal(t, "notifications/tools/list_changed", notification.Method)
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received")
	}

	// Verify tool was removed from session
	assert.Len(t, session.GetSessionTools(), 1)
	assert.NotContains(t, session.GetSessionTools(), "session-tool-1")
	assert.Contains(t, session.GetSessionTools(), "session-tool-2")
}

func TestMCPServer_ToolFiltering(t *testing.T) {
	// Create a filter that filters tools by prefix
	filterByPrefix := func(prefix string) ToolFilterFunc {
		return func(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
			var filtered []mcp.Tool
			for _, tool := range tools {
				if len(tool.Name) >= len(prefix) && tool.Name[:len(prefix)] == prefix {
					filtered = append(filtered, tool)
				}
			}
			return filtered
		}
	}

	// Create a server with a tool filter
	server := NewMCPServer("test-server", "1.0.0",
		WithToolCapabilities(true),
		WithToolFilter(filterByPrefix("allow-")),
	)

	// Add tools with different prefixes
	server.AddTools(
		ServerTool{Tool: mcp.NewTool("allow-tool-1")},
		ServerTool{Tool: mcp.NewTool("allow-tool-2")},
		ServerTool{Tool: mcp.NewTool("deny-tool-1")},
		ServerTool{Tool: mcp.NewTool("deny-tool-2")},
	)

	// Create a session with tools
	session := &sessionTestClientWithTools{
		sessionID:           "session-1",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         true,
		sessionTools: map[string]ServerTool{
			"allow-session-tool": {
				Tool: mcp.NewTool("allow-session-tool"),
			},
			"deny-session-tool": {
				Tool: mcp.NewTool("deny-session-tool"),
			},
		},
	}

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// List tools with session context
	sessionCtx := server.WithContext(context.Background(), session)
	header := map[string]string{"Authorization": "Bearer test"}
	response := server.HandleMessage(sessionCtx, header, []byte(`{
		"jsonrpc": "2.0",
		"id": 1,
		"method": "tools/list"
	}`))
	resp, ok := response.(mcp.JSONRPCResponse)
	require.True(t, ok)

	result, ok := resp.Result.(mcp.ListToolsResult)
	require.True(t, ok)

	// Should only include tools with the "allow-" prefix
	assert.Len(t, result.Tools, 3)

	// Verify all tools start with "allow-"
	for _, tool := range result.Tools {
		assert.True(t, len(tool.Name) >= 6 && tool.Name[:6] == "allow-",
			"Tool should start with 'allow-', got: %s", tool.Name)
	}
}

func TestMCPServer_SendNotificationToSpecificClient(t *testing.T) {
	server := NewMCPServer("test-server", "1.0.0")

	session1Chan := make(chan mcp.JSONRPCNotification, 10)
	session1 := &sessionTestClient{
		sessionID:           "session-1",
		notificationChannel: session1Chan,
	}
	session1.Initialize()

	session2Chan := make(chan mcp.JSONRPCNotification, 10)
	session2 := &sessionTestClient{
		sessionID:           "session-2",
		notificationChannel: session2Chan,
	}
	session2.Initialize()

	session3 := &sessionTestClient{
		sessionID:           "session-3",
		notificationChannel: make(chan mcp.JSONRPCNotification, 10),
		initialized:         false, // Not initialized - deliberately not calling Initialize()
	}

	// Register sessions
	err := server.RegisterSession(context.Background(), session1)
	require.NoError(t, err)
	err = server.RegisterSession(context.Background(), session2)
	require.NoError(t, err)
	err = server.RegisterSession(context.Background(), session3)
	require.NoError(t, err)

	// Send notification to session 1
	err = server.SendNotificationToSpecificClient(session1.SessionID(), "test-method", map[string]any{
		"data": "test-data",
	})
	require.NoError(t, err)

	// Check that only session 1 received the notification
	select {
	case notification := <-session1Chan:
		assert.Equal(t, "test-method", notification.Method)
		assert.Equal(t, "test-data", notification.Params.AdditionalFields["data"])
	case <-time.After(100 * time.Millisecond):
		t.Error("Expected notification not received by session 1")
	}

	// Verify session 2 did not receive notification
	select {
	case notification := <-session2Chan:
		t.Errorf("Unexpected notification received by session 2: %v", notification)
	case <-time.After(100 * time.Millisecond):
		// Expected, no notification for session 2
	}

	// Test sending to non-existent session
	err = server.SendNotificationToSpecificClient("non-existent", "test-method", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Test sending to uninitialized session
	err = server.SendNotificationToSpecificClient(session3.SessionID(), "test-method", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not properly initialized")
}

func TestMCPServer_NotificationChannelBlocked(t *testing.T) {
	// Set up a hooks object to capture error notifications
	var mu sync.Mutex
	errorCaptured := false
	errorSessionID := ""
	errorMethod := ""

	hooks := &Hooks{}
	hooks.AddOnError(func(ctx context.Context, id any, method mcp.MCPMethod, message any, err error) {
		mu.Lock()
		defer mu.Unlock()

		errorCaptured = true
		// Extract session ID and method from the error message metadata
		if msgMap, ok := message.(map[string]interface{}); ok {
			if sid, ok := msgMap["sessionID"].(string); ok {
				errorSessionID = sid
			}
			if m, ok := msgMap["method"].(string); ok {
				errorMethod = m
			}
		}
		// Verify the error is a notification channel blocked error
		assert.True(t, errors.Is(err, ErrNotificationChannelBlocked))
	})

	// Create a server with hooks
	server := NewMCPServer("test-server", "1.0.0", WithHooks(hooks))

	// Create a session with a very small buffer that will get blocked
	smallBufferChan := make(chan mcp.JSONRPCNotification, 1)
	session := &sessionTestClient{
		sessionID:           "blocked-session",
		notificationChannel: smallBufferChan,
	}
	session.Initialize()

	// Register the session
	err := server.RegisterSession(context.Background(), session)
	require.NoError(t, err)

	// Fill the buffer first to ensure it gets blocked
	server.SendNotificationToSpecificClient(session.SessionID(), "first-message", nil)

	// This will cause the buffer to block
	err = server.SendNotificationToSpecificClient(session.SessionID(), "blocked-message", nil)
	assert.Error(t, err)
	assert.Equal(t, ErrNotificationChannelBlocked, err)

	// Wait a bit for the goroutine to execute
	time.Sleep(10 * time.Millisecond)

	// Verify the error was logged via hooks
	mu.Lock()
	localErrorCaptured := errorCaptured
	localErrorSessionID := errorSessionID
	localErrorMethod := errorMethod
	mu.Unlock()

	assert.True(t, localErrorCaptured, "Error hook should have been called")
	assert.Equal(t, "blocked-session", localErrorSessionID, "Session ID should be captured in the error hook")
	assert.Equal(t, "blocked-message", localErrorMethod, "Method should be captured in the error hook")

	// Also test SendNotificationToAllClients with a blocked channel
	// Reset the captured data
	mu.Lock()
	errorCaptured = false
	errorSessionID = ""
	errorMethod = ""
	mu.Unlock()

	// Send to all clients (which includes our blocked one)
	server.SendNotificationToAllClients("broadcast-message", nil)

	// Wait a bit for the goroutine to execute
	time.Sleep(10 * time.Millisecond)

	// Verify the error was logged via hooks
	mu.Lock()
	localErrorCaptured = errorCaptured
	localErrorSessionID = errorSessionID
	localErrorMethod = errorMethod
	mu.Unlock()

	assert.True(t, localErrorCaptured, "Error hook should have been called for broadcast")
	assert.Equal(t, "blocked-session", localErrorSessionID, "Session ID should be captured in the error hook")
	assert.Equal(t, "broadcast-message", localErrorMethod, "Method should be captured in the error hook")
}
