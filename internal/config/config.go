// Package config loads lazyobs settings: named contexts from a YAML file,
// overridden by environment variables and command-line flags, in the spirit
// of kubectl.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"

	"go.yaml.in/yaml/v3"

	"github.com/CentreonLabs/lazyobs/internal/secret"
)

// Environment variables read by Resolve.
const (
	EnvConfig  = "LAZYOBS_CONFIG"
	EnvContext = "LAZYOBS_CONTEXT"
	EnvURL     = "LAZYOBS_URL"
	EnvOrg     = "LAZYOBS_ORG"
	EnvToken   = "LAZYOBS_TOKEN" //nolint:gosec // variable name, not a credential
)

// File is the content of the configuration file.
type File struct {
	CurrentContext string             `yaml:"current-context"`
	Contexts       map[string]Context `yaml:"contexts"`
}

// Context is one named connection profile. At most one token source may be set.
type Context struct {
	URL          string        `yaml:"url,omitempty"`
	Org          string        `yaml:"org"`
	Token        secret.String `yaml:"token,omitempty"`
	TokenFile    string        `yaml:"token-file,omitempty"`
	TokenCommand string        `yaml:"token-command,omitempty"`
}

// Names returns the context names, sorted.
func (f *File) Names() []string {
	names := make([]string, 0, len(f.Contexts))
	for n := range f.Contexts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// ReadFile parses the configuration file at path. A missing file yields an
// empty configuration and exists=false.
func ReadFile(path string) (f *File, exists bool, err error) {
	b, err := os.ReadFile(path) //nolint:gosec // path chosen by the user
	if errors.Is(err, fs.ErrNotExist) {
		return &File{}, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("config: %w", err)
	}
	f = &File{}
	if err := yaml.Unmarshal(b, f); err != nil {
		return nil, true, fmt.Errorf("config: parse %s: %w", path, err)
	}
	return f, true, nil
}
