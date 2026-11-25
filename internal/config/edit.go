package config

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// Edits work on the YAML node tree rather than on File: comments, ordering
// and inline tokens are preserved (File would write tokens back redacted).

// ContextUpdate lists the fields to change in a context; nil fields are kept.
// Setting one token source removes the other ones.
type ContextUpdate struct {
	URL          *string
	Org          *string
	TokenFile    *string
	TokenCommand *string
}

// UseContext sets current-context, which must name an existing context.
func UseContext(path, name string) error {
	return edit(path, func(root *yaml.Node) error {
		contexts := mapGet(root, "contexts")
		if contexts == nil || mapGet(contexts, name) == nil {
			return fmt.Errorf("context %q not found in %s", name, path)
		}
		mapSet(root, "current-context", scalar(name))
		return nil
	})
}

// SetContext creates or updates a context. The first context created becomes the current one.
func SetContext(path, name string, u ContextUpdate) error {
	if name == "" {
		return errors.New("context name must not be empty")
	}
	return edit(path, func(root *yaml.Node) error {
		contexts := mapGet(root, "contexts")
		if contexts == nil || contexts.Kind != yaml.MappingNode {
			contexts = &yaml.Node{Kind: yaml.MappingNode}
			mapSet(root, "contexts", contexts)
		}
		ctx := mapGet(contexts, name)
		if ctx == nil || ctx.Kind != yaml.MappingNode {
			ctx = &yaml.Node{Kind: yaml.MappingNode}
			mapSet(contexts, name, ctx)
		}
		setOpt(ctx, "url", u.URL)
		setOpt(ctx, "org", u.Org)
		if u.TokenFile != nil {
			mapDelete(ctx, "token")
			mapDelete(ctx, "token-command")
			setOpt(ctx, "token-file", u.TokenFile)
		}
		if u.TokenCommand != nil {
			mapDelete(ctx, "token")
			mapDelete(ctx, "token-file")
			setOpt(ctx, "token-command", u.TokenCommand)
		}
		if cur := mapGet(root, "current-context"); cur == nil || cur.Value == "" {
			mapSet(root, "current-context", scalar(name))
		}
		return nil
	})
}

// edit loads path as a YAML document (or starts an empty one), applies fn and
// writes the result atomically, keeping the file private.
func edit(path string, fn func(root *yaml.Node) error) error {
	var doc yaml.Node
	b, err := os.ReadFile(path) //nolint:gosec // path chosen by the user
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return fmt.Errorf("config: %w", err)
	default:
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("config: parse %s: %w", path, err)
		}
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("config: %s: top level must be a mapping", path)
	}
	if err := fn(root); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("config: encode: %w", err)
	}
	return writeAtomic(path, buf.Bytes())
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, ".config-*.yaml")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	return nil
}

func scalar(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }

func mapGet(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mapSet(m *yaml.Node, key string, v *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = v
			return
		}
	}
	m.Content = append(m.Content, scalar(key), v)
}

func mapDelete(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return
		}
	}
}

// setOpt sets key when v is non-nil; an empty value removes the key.
func setOpt(m *yaml.Node, key string, v *string) {
	switch {
	case v == nil:
	case *v == "":
		mapDelete(m, key)
	default:
		mapSet(m, key, scalar(*v))
	}
}
