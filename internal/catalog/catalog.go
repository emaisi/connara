package catalog

import (
	"apihub-go/internal/jsonutil"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"apihub-go/internal/model"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed defaults.json
var defaults embed.FS

type Catalog struct {
	providers map[string]model.Provider
	actions   map[string]model.Action
	schemas   sync.Map
}

type compiledSchema struct {
	schema *jsonschema.Schema
	err    error
}

func Load(extraDir string) (*Catalog, error) {
	raw, err := defaults.ReadFile("defaults.json")
	if err != nil {
		return nil, err
	}
	var providers []model.Provider
	if err := jsonutil.Unmarshal(raw, &providers); err != nil {
		return nil, fmt.Errorf("decode built-in catalog: %w", err)
	}

	c := &Catalog{providers: make(map[string]model.Provider), actions: make(map[string]model.Action)}
	for _, provider := range providers {
		if err := c.add(provider, true); err != nil {
			return nil, err
		}
	}
	if extraDir == "" {
		return c, nil
	}

	entries, err := os.ReadDir(extraDir)
	if err != nil {
		return nil, fmt.Errorf("read catalog directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(extraDir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read provider %s: %w", entry.Name(), err)
		}
		var provider model.Provider
		if err := jsonutil.Unmarshal(data, &provider); err != nil {
			return nil, fmt.Errorf("decode provider %s: %w", entry.Name(), err)
		}
		if err := c.add(provider, false); err != nil {
			return nil, fmt.Errorf("provider %s: %w", entry.Name(), err)
		}
	}
	return c, nil
}

func (c *Catalog) add(provider model.Provider, replace bool) error {
	provider.Service = strings.TrimSpace(provider.Service)
	if provider.Service == "" || provider.DisplayName == "" {
		return errors.New("service and displayName are required")
	}
	if _, exists := c.providers[provider.Service]; exists && !replace {
		// Keep the built-in executable adapter but allow external metadata-only providers.
		return nil
	}
	for i := range provider.Actions {
		action := &provider.Actions[i]
		if action.ID == "" || action.Service != provider.Service {
			return fmt.Errorf("invalid action %q", action.ID)
		}
		action.Executable = action.Runtime != nil && provider.BaseURL != ""
		c.actions[action.ID] = *action
	}
	c.providers[provider.Service] = provider
	return nil
}

func (c *Catalog) Providers() []model.Provider {
	items := make([]model.Provider, 0, len(c.providers))
	for _, provider := range c.providers {
		items = append(items, provider)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Service < items[j].Service })
	return items
}

func (c *Catalog) Provider(service string) (model.Provider, bool) {
	provider, ok := c.providers[service]
	return provider, ok
}

func (c *Catalog) Action(id string) (model.Action, bool) {
	action, ok := c.actions[id]
	return action, ok
}

func (c *Catalog) Actions(service, query string, limit int) []model.Action {
	query = strings.ToLower(strings.TrimSpace(query))
	items := make([]model.Action, 0)
	for _, action := range c.actions {
		if service != "" && action.Service != service {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(action.ID+" "+action.Description), query) {
			continue
		}
		items = append(items, action)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items
}

func (c *Catalog) ValidateInput(action model.Action, input any) error {
	rawSchema, err := json.Marshal(action.InputSchema)
	if err != nil {
		return fmt.Errorf("encode input schema: %w", err)
	}
	digest := sha256.Sum256(rawSchema)
	cacheKey := action.ID + ":" + hex.EncodeToString(digest[:])
	value, ok := c.schemas.Load(cacheKey)
	if !ok {
		compiler := jsonschema.NewCompiler()
		compiler.DefaultDraft(jsonschema.Draft2020)
		location := "https://apihub.local/schemas/" + action.ID
		err := compiler.AddResource(location, action.InputSchema)
		var schema *jsonschema.Schema
		if err == nil {
			schema, err = compiler.Compile(location)
		}
		value, _ = c.schemas.LoadOrStore(cacheKey, compiledSchema{schema: schema, err: err})
	}
	compiled := value.(compiledSchema)
	if compiled.err != nil {
		return fmt.Errorf("compile input schema: %w", compiled.err)
	}
	if err := compiled.schema.Validate(input); err != nil {
		return fmt.Errorf("input does not match action schema: %w", err)
	}
	return nil
}

func IsNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
