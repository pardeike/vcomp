// Package mcp exposes optional filesystem-backed company conveniences over stdio.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vcomp/internal/bootstrap"
)

type Server struct{ Root, Role string }
type object = map[string]any

type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema object `json:"inputSchema"`
	Annotations object `json:"annotations"`
}

func New(root, role string) (*Server, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	if err := bootstrap.RoleName(role); err != nil {
		return nil, err
	}
	s := &Server{root, role}
	if _, err := s.employee(role); err != nil {
		return nil, err
	}
	if _, err := s.path("CONVENTIONS.md"); err != nil {
		return nil, err
	}
	return s, nil
}

// Every operation is bound to this company. This avoids accidental cross-company
// paths; employees retain their ordinary unrestricted filesystem tools.
func (s *Server) path(parts ...string) (string, error) {
	p := filepath.Join(append([]string{s.Root}, parts...)...)
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(s.Root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path is outside this company")
	}
	return resolved, nil
}
func (s *Server) employee(name string) (string, error) {
	if err := bootstrap.RoleName(name); err != nil {
		return "", err
	}

	dir, err := s.ordinaryDir("spaces", name)
	if err != nil {
		return "", fmt.Errorf("employee %q: %w", name, err)
	}
	if _, err := s.path("spaces", name, "role.md"); err != nil {
		return "", fmt.Errorf("employee %q: %w", name, err)
	}
	return dir, nil
}

// Mutations must address the named employee and inbox, not a symlink alias.
func (s *Server) ordinaryDir(parts ...string) (string, error) {
	p, err := s.path(parts...)
	if err != nil {
		return "", err
	}
	expected := filepath.Join(append([]string{s.Root}, parts...)...)
	if p != expected {
		return "", fmt.Errorf("expected an ordinary company directory: %s", expected)
	}
	info, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", p)
	}
	return p, nil
}

func (s *Server) Serve(in io.Reader, out io.Writer) error {
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 4096), 1<<20)
	enc := json.NewEncoder(out)
	initialized := false
	for scan.Scan() {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params"`
		}
		response := object{"jsonrpc": "2.0", "id": nil}
		rpcError := func(code int, msg string) { response["error"] = object{"code": code, "message": msg} }
		if err := json.Unmarshal(scan.Bytes(), &req); err != nil {
			if json.Valid(scan.Bytes()) {
				rpcError(-32600, "Invalid Request")
			} else {
				rpcError(-32700, "Parse error")
			}
		} else if req.JSONRPC != "2.0" || req.Method == "" || !validID(req.ID) {
			rpcError(-32600, "Invalid Request")
		} else {
			if len(req.ID) == 0 {
				continue
			}
			response["id"] = req.ID
			switch req.Method {
			case "initialize":
				var p struct {
					ProtocolVersion string `json:"protocolVersion"`
				}
				if err := json.Unmarshal(req.Params, &p); err != nil || p.ProtocolVersion == "" {
					rpcError(-32602, "Invalid initialization parameters")
					break
				}
				version := p.ProtocolVersion
				switch version {
				case "2024-11-05", "2025-03-26", "2025-06-18", "2025-11-25":
				default:
					version = "2025-11-25"
				}
				initialized = true
				response["result"] = object{"protocolVersion": version, "capabilities": object{"tools": object{}}, "serverInfo": object{"name": "vcomp", "version": "1"}, "instructions": "Optional conveniences for this employee's company. Files remain authoritative; normal filesystem tools are equally valid. Choose your own focus. company_overview identifies paths and observed state; role_read retrieves existing instructions. Tools add no duties or priorities."}
			case "ping":
				response["result"] = object{}
			case "tools/list":
				if !initialized {
					rpcError(-32000, "Initialize first")
					break
				}
				response["result"] = object{"tools": Tools()}
			case "tools/call":
				if !initialized {
					rpcError(-32000, "Initialize first")
					break
				}
				var p struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				}
				if err := json.Unmarshal(req.Params, &p); err != nil {
					rpcError(-32602, "Invalid tool call")
					break
				}
				value, err := s.Call(p.Name, p.Arguments)
				if err != nil {
					response["result"] = object{"content": []object{{"type": "text", "text": err.Error()}}, "isError": true}
				} else {
					b, err := json.Marshal(value)
					if err != nil {
						return err
					}
					response["result"] = object{"content": []object{{"type": "text", "text": string(b)}}}
				}
			default:
				rpcError(-32601, "Method not found")
			}
		}
		if err := enc.Encode(response); err != nil {
			return err
		}
	}
	return scan.Err()
}

func validID(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return true
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	switch value.(type) {
	case string, float64:
		return true
	}
	return false
}

func schema(props object, required ...string) object {
	s := object{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}
func textField(description string) object {
	return object{"type": "string", "description": description}
}
func Tools() []Tool {
	role := textField("Exact employee name from company_overview; omit for yourself.")
	offset := object{"type": "integer", "minimum": 0, "description": "Zero-based offset; continue from next_offset."}
	limit := object{"type": "integer", "minimum": 1, "maximum": 100, "description": "Maximum entries or lines; default 20."}
	topic := textField("Exact topic folder name returned by inbox_list.")
	makeTool := func(name, description string, props object, readOnly bool, required ...string) Tool {
		return Tool{name, description, schema(props, required...), object{"readOnlyHint": readOnly, "destructiveHint": name == "inbox_remove", "idempotentHint": readOnly, "openWorldHint": false}}
	}
	return []Tool{
		makeTool("company_overview", "Compact company paths, roster and dashboard observations. Live means a running process, not proof of useful work. Paginated employees; no message bodies or source code.", object{"offset": offset, "limit": limit}, true),
		makeTool("role_read", "Read existing role, company conventions, notes or personal goals in bounded pages. These are the same files accessible through ordinary tools.", object{"role": role, "document": object{"type": "string", "enum": []string{"role", "conventions", "notes", "goals"}}, "offset": offset, "limit": limit}, true, "document"),
		makeTool("inbox_list", "List existing inbox topics with short previews and modification times. Listing or reading never removes a message.", object{"role": role, "offset": offset, "limit": limit}, true),
		makeTool("inbox_read", "Read a topic's message.md in bounded pages and list attachments. Reading does not acknowledge or remove it.", object{"role": role, "topic": topic, "offset": offset, "limit": limit}, true, "topic"),
		makeTool("message_send", "Send an ordinary message to an existing colleague's inbox. Supplies your From identity and correct path. Does not interrupt or force the recipient to read it.", object{"recipient": textField("Exact existing employee name."), "subject": textField("Short single-line topic."), "body": textField("Message text.")}, false, "recipient", "subject", "body"),
		makeTool("inbox_remove", "Remove one specifically handled topic and its attachments from your own inbox. Use the exact topic name; this does not send a reply.", object{"topic": topic}, false, "topic"),
	}
}

type arguments struct {
	Role, Document, Topic, Recipient, Subject, Body string
	Offset, Limit                                   int
}

func parseArgs(name string, raw json.RawMessage) (arguments, error) {
	var a arguments
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	var props map[string]json.RawMessage
	if err := json.Unmarshal(raw, &props); err != nil || props == nil {
		return a, fmt.Errorf("arguments must be an object")
	}
	var spec *Tool
	for _, tool := range Tools() {
		if tool.Name == name {
			t := tool
			spec = &t
			break
		}
	}
	if spec == nil {
		return a, fmt.Errorf("unknown tool %q", name)
	}
	allowed := spec.InputSchema["properties"].(object)
	for key, value := range props {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return a, fmt.Errorf("%s must not be null", key)
		}
		if _, ok := allowed[key]; !ok {
			return a, fmt.Errorf("unknown argument %q for %s", key, name)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&a); err != nil {
		return a, err
	}
	required, _ := spec.InputSchema["required"].([]string)
	for _, key := range required {
		var value string
		if json.Unmarshal(props[key], &value) != nil || strings.TrimSpace(value) == "" {
			return a, fmt.Errorf("%s is required", key)
		}
	}
	if a.Offset < 0 {
		return a, fmt.Errorf("offset must be nonnegative")
	}
	if _, ok := props["limit"]; ok && (a.Limit < 1 || a.Limit > 100) {
		return a, fmt.Errorf("limit must be between 1 and 100")
	}
	if a.Limit == 0 {
		a.Limit = 20
	}
	return a, nil
}
