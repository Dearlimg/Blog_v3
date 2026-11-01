package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPServer struct {
	Name         string   `json:"name"`
	URL          string   `json:"url"`
	AllowedTools []string `json:"allowed_tools"`
	TokenEnv     string   `json:"token_env"`
}

type MCPRegistry struct {
	servers     map[string]MCPServer
	configError error
}

func newMCPRegistry(path string) *MCPRegistry {
	r := &MCPRegistry{servers: map[string]MCPServer{}}
	if path == "" {
		return r
	}
	data, err := os.ReadFile(path)
	if err != nil {
		r.configError = errors.New("MCP configuration unavailable")
		return r
	}
	var servers []MCPServer
	if json.Unmarshal(data, &servers) != nil {
		r.configError = errors.New("MCP configuration invalid")
		return r
	}
	for _, server := range servers {
		u, err := validWebURL(server.URL)
		if err != nil || u.Scheme != "https" || len(server.AllowedTools) == 0 {
			r.configError = errors.New("MCP requires HTTPS and an explicit tool allowlist")
			return r
		}
		if server.TokenEnv != "" && !strings.HasPrefix(server.TokenEnv, "MCP_") {
			r.configError = errors.New("MCP token environment must start with MCP_")
			return r
		}
		r.servers[server.Name] = server
	}
	return r
}

type mcpAuthTransport struct {
	base  http.RoundTripper
	token string
}

func (t mcpAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	copy := req.Clone(req.Context())
	if t.token != "" {
		copy.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(copy)
}

func (r *MCPRegistry) connect(ctx context.Context, name string) (*mcp.ClientSession, MCPServer, error) {
	if r.configError != nil {
		return nil, MCPServer{}, r.configError
	}
	server, ok := r.servers[name]
	if !ok {
		return nil, server, errors.New("MCP server is not configured by the owner")
	}
	web := NewWeb("")
	web.client.Timeout = 30 * time.Second
	web.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	web.client.Transport = mcpAuthTransport{base: web.client.Transport, token: os.Getenv(server.TokenEnv)}
	client := mcp.NewClient(&mcp.Implementation{Name: "durlim-blog", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: server.URL, HTTPClient: web.client, MaxRetries: -1}, nil)
	if err != nil {
		return nil, server, errors.New("MCP connection failed")
	}
	return session, server, nil
}

func (r *MCPRegistry) List(ctx context.Context, name string) (any, error) {
	session, server, err := r.connect(ctx, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	tools := []*mcp.Tool{}
	params := &mcp.ListToolsParams{}
	for range 10 {
		result, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, errors.New("MCP discovery failed")
		}
		for _, tool := range result.Tools {
			if slices.Contains(server.AllowedTools, tool.Name) {
				tools = append(tools, tool)
			}
		}
		if result.NextCursor == "" {
			return tools, nil
		}
		params.Cursor = result.NextCursor
	}
	return nil, errors.New("MCP tool list exceeded page limit")
}

func (r *MCPRegistry) Call(ctx context.Context, a Args) (any, error) {
	server, ok := r.servers[a.Name]
	if !ok || !slices.Contains(server.AllowedTools, a.Agent) {
		return nil, errors.New("MCP tool is not allowlisted")
	}
	session, _, err := r.connect(ctx, a.Name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = session.Close() }()
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: a.Agent, Arguments: a.Arguments})
	if err != nil {
		return nil, errors.New("MCP call failed")
	}
	return result, nil
}

func (r *MCPRegistry) Close() {}
