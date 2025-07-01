package mcptest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/wlxwlxwlx/mcp-go/mcp"
	"github.com/wlxwlxwlx/mcp-go/mcptest"
	"github.com/wlxwlxwlx/mcp-go/server"
)

func TestServerWithTool(t *testing.T) {
	ctx := context.Background()

	srv, err := mcptest.NewServer(t, server.ServerTool{
		Tool: mcp.NewTool("hello",
			mcp.WithDescription("Says hello to the provided name, or world."),
			mcp.WithString("name", mcp.Description("The name to say hello to.")),
		),
		Handler: helloWorldHandler,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	client := srv.Client()

	var req mcp.CallToolRequest
	req.Params.Name = "hello"
	req.Params.Arguments = map[string]any{
		"name": "Claude",
	}

	result, err := client.CallTool(ctx, req)
	if err != nil {
		t.Fatal("CallTool:", err)
	}

	got, err := resultToString(result)
	if err != nil {
		t.Fatal(err)
	}

	want := "Hello, Claude!"
	if got != want {
		t.Errorf("Got %q, want %q", got, want)
	}
}

func helloWorldHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Extract name from request arguments
	name, ok := request.GetArguments()["name"].(string)
	if !ok {
		name = "World"
	}

	return mcp.NewToolResultText(fmt.Sprintf("Hello, %s!", name)), nil
}

func resultToString(result *mcp.CallToolResult) (string, error) {
	var b strings.Builder

	for _, content := range result.Content {
		text, ok := content.(mcp.TextContent)
		if !ok {
			return "", fmt.Errorf("unsupported content type: %T", content)
		}
		b.WriteString(text.Text)
	}

	if result.IsError {
		return "", fmt.Errorf("%s", b.String())
	}

	return b.String(), nil
}

func TestServerWithPrompt(t *testing.T) {
	ctx := context.Background()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	prompt := mcp.Prompt{
		Name:        "greeting",
		Description: "A greeting prompt",
		Arguments: []mcp.PromptArgument{
			{
				Name:        "name",
				Description: "The name to greet",
				Required:    true,
			},
		},
	}
	handler := func(ctx context.Context, request mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		return &mcp.GetPromptResult{
			Description: "A greeting prompt",
			Messages: []mcp.PromptMessage{
				{
					Role:    mcp.RoleUser,
					Content: mcp.NewTextContent(fmt.Sprintf("Hello, %s!", request.Params.Arguments["name"])),
				},
			},
		}, nil
	}

	srv.AddPrompt(prompt, handler)

	err := srv.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var getReq mcp.GetPromptRequest
	getReq.Params.Name = "greeting"
	getReq.Params.Arguments = map[string]string{"name": "John"}
	getResult, err := srv.Client().GetPrompt(ctx, getReq)
	if err != nil {
		t.Fatal("GetPrompt:", err)
	}
	if getResult.Description != "A greeting prompt" {
		t.Errorf("Expected prompt description 'A greeting prompt', got %q", getResult.Description)
	}
	if len(getResult.Messages) != 1 {
		t.Fatalf("Expected 1 message, got %d", len(getResult.Messages))
	}
	if getResult.Messages[0].Role != mcp.RoleUser {
		t.Errorf("Expected message role 'user', got %q", getResult.Messages[0].Role)
	}
	content, ok := getResult.Messages[0].Content.(mcp.TextContent)
	if !ok {
		t.Fatalf("Expected TextContent, got %T", getResult.Messages[0].Content)
	}
	if content.Text != "Hello, John!" {
		t.Errorf("Expected message content 'Hello, John!', got %q", content.Text)
	}
}

func TestServerWithResource(t *testing.T) {
	ctx := context.Background()

	srv := mcptest.NewUnstartedServer(t)
	defer srv.Close()

	resource := mcp.Resource{
		URI:         "test://resource",
		Name:        "Test Resource",
		Description: "A test resource",
		MIMEType:    "text/plain",
	}

	handler := func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "test://resource",
				MIMEType: "text/plain",
				Text:     "This is a test resource content.",
			},
		}, nil
	}

	srv.AddResource(resource, handler)

	err := srv.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}

	var readReq mcp.ReadResourceRequest
	readReq.Params.URI = "test://resource"
	readResult, err := srv.Client().ReadResource(ctx, readReq)
	if err != nil {
		t.Fatal("ReadResource:", err)
	}
	if len(readResult.Contents) != 1 {
		t.Fatalf("Expected 1 content, got %d", len(readResult.Contents))
	}
	textContent, ok := readResult.Contents[0].(mcp.TextResourceContents)
	if !ok {
		t.Fatalf("Expected TextResourceContents, got %T", readResult.Contents[0])
	}
	want := "This is a test resource content."
	if textContent.Text != want {
		t.Errorf("Got %q, want %q", textContent.Text, want)
	}
}
