// Package harness supplies employee-scoped integrations for the supported CLIs.
package harness

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"vcomp/internal/bootstrap"
	"vcomp/internal/space"
)

const serverName = "vcomp-company"

//go:embed pi-mcp.js
var piExtension string

// WithMCP prepares optional company tools without changing global CLI settings.
// Existing processes are unaffected; this command is for a new harness start.
func WithMCP(root, role, cli string, argv []string, enabled bool, timeout time.Duration) ([]string, error) {
	switch cli {
	case "omp", "pi", "claude", "codex", "opencode":
	default:
		return argv, nil
	}
	if err := bootstrap.RoleName(role); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	cwd := filepath.Join(root, "spaces", role)
	if !enabled {
		if cli == "omp" {
			return argv, ompConfig(cwd, nil)
		}
		return argv, nil
	}
	binary, err := os.Executable()
	if err != nil {
		return nil, err
	}
	args := []string{"mcp", "-root", root, "-role", role}
	server := map[string]any{"type": "stdio", "command": binary, "args": args}
	configDir := filepath.Join(root, ".vcomp", "mcp", role)
	argv = append([]string(nil), argv...)
	switch cli {
	case "omp":
		server["cwd"] = cwd
		return argv, ompConfig(cwd, server)
	case "claude":
		path := filepath.Join(configDir, "claude.json")
		b, _ := json.Marshal(map[string]any{"mcpServers": map[string]any{serverName: server}})
		if err := write(path, string(b)+"\n"); err != nil {
			return nil, err
		}
		return append(argv, "--mcp-config", path), nil
	case "codex":
		commandJSON, _ := json.Marshal(binary)
		argsJSON, _ := json.Marshal(args)
		return append(argv, "-c", "mcp_servers."+serverName+".command="+string(commandJSON), "-c", "mcp_servers."+serverName+".args="+string(argsJSON)), nil
	case "opencode":
		cfg := map[string]any{}
		if raw := os.Getenv("OPENCODE_CONFIG_CONTENT"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &cfg); err != nil || cfg == nil {
				return nil, fmt.Errorf("invalid OPENCODE_CONFIG_CONTENT")
			}
		}
		servers, err := section(cfg, "mcp")
		if err != nil {
			return nil, err
		}
		servers[serverName] = map[string]any{"type": "local", "command": append([]string{binary}, args...), "enabled": true}
		b, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		return append([]string{"env", "OPENCODE_CONFIG_CONTENT=" + string(b)}, argv...), nil
	case "pi":
		path := filepath.Join(configDir, "pi-mcp.mjs")
		options, _ := json.Marshal(map[string]any{"command": binary, "args": args, "cwd": cwd, "timeout": timeout.Milliseconds()})
		if err := write(path, "const options = "+string(options)+";\n"+piExtension); err != nil {
			return nil, err
		}
		return append(argv, "--extension", path), nil
	}
	return argv, nil
}

func section(cfg map[string]any, key string) (map[string]any, error) {
	if old, ok := cfg[key]; ok {
		value, ok := old.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s must be an object", key)
		}
		return value, nil
	}
	value := map[string]any{}
	cfg[key] = value
	return value, nil
}

func ompConfig(cwd string, server map[string]any) error {
	path := filepath.Join(cwd, ".omp", "mcp.json")
	cfg := map[string]any{}
	b, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) && server == nil {
		return nil
	}
	if err == nil {
		if err := json.Unmarshal(b, &cfg); err != nil || cfg == nil {
			return fmt.Errorf("invalid %s", path)
		}
	}
	servers, err := section(cfg, "mcpServers")
	if err != nil {
		return err
	}
	if server == nil {
		delete(servers, serverName)
	} else {
		servers[serverName] = server
	}
	b, err = json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return write(path, string(b)+"\n")
}

func write(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return space.WriteFile(path, []byte(content), 0600)
}
