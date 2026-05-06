package collectors

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadDir reads every *.yaml / *.yml file under dir, in lexical order, and
// returns the concatenated bindings. A missing dir returns no bindings and no
// error — operators who delete the dir get a heartbeat with an empty metrics
// map, not a hard failure.
func LoadDir(dir string) ([]Binding, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	var all []Binding
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		bs, err := parseFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		all = append(all, bs...)
	}
	return all, nil
}

func parseFile(path string) ([]Binding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var bindings []Binding
	if err := yaml.Unmarshal(data, &bindings); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}
	return bindings, nil
}
