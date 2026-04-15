// Package values updates scalar values in a YAML file using a hybrid strategy:
// yaml.v3 Node tree locates the target node's line number, then byte-level line
// replacement preserves formatting, comments, and indentation exactly.
package values

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// UpdateOptions selects the update strategy.
type UpdateOptions struct {
	Mode string // "image" (default) | "key" | "marker"
	Key  string // dot-notation path, for "key" mode only (e.g. "image.tag")
}

// SetTag finds the target node(s) in the file and replaces their scalar value with tag.
// Returns the number of nodes updated. Returns an error if no target is found.
func SetTag(file, tag string, opts UpdateOptions) (int, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return 0, fmt.Errorf("reading %s: %w", file, err)
	}
	updated, n, err := applyTag(data, tag, opts)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", file, err)
	}
	if err := os.WriteFile(file, updated, 0o644); err != nil {
		return 0, fmt.Errorf("writing %s: %w", file, err)
	}
	return n, nil
}

// HasTarget reports whether the file contains at least one node matching opts.
// Used for atomic pre-flight before multi-file writes. File-read or YAML-parse
// errors surface as errors; "target not found" returns (false, nil).
func HasTarget(file string, opts UpdateOptions) (bool, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return false, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return false, fmt.Errorf("parsing %s: %w", file, err)
	}
	nodes, err := findTargets(&root, opts)
	if err != nil {
		return false, nil
	}
	return len(nodes) > 0, nil
}

// ReadTag reads the current value of the target node.
// Returns ("", nil) if not found (first deploy, file not yet wired).
func ReadTag(file string, opts UpdateOptions) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("reading %s: %w", file, err)
	}
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return "", fmt.Errorf("parsing %s: %w", file, err)
	}
	nodes, err := findTargets(&root, opts)
	if err != nil || len(nodes) == 0 {
		return "", nil
	}
	return nodes[0].Value, nil
}

// applyTag is the pure-data core: parse, find targets, replace lines.
func applyTag(data []byte, tag string, opts UpdateOptions) ([]byte, int, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, 0, fmt.Errorf("parsing YAML: %w", err)
	}
	targets, err := findTargets(&root, opts)
	if err != nil {
		return nil, 0, err
	}
	if len(targets) == 0 {
		return nil, 0, fmt.Errorf("no target found for mode=%q key=%q", effectiveMode(opts.Mode), opts.Key)
	}

	lines := bytes.Split(data, []byte("\n"))
	for _, node := range targets {
		idx := node.Line - 1
		if idx < 0 || idx >= len(lines) {
			return nil, 0, fmt.Errorf("target line %d out of range", node.Line)
		}
		lines[idx] = replaceValueAtColumn(lines[idx], node.Column-1, tag)
	}
	return bytes.Join(lines, []byte("\n")), len(targets), nil
}

// findTargets dispatches to the right finder for the mode.
func findTargets(root *yaml.Node, opts UpdateOptions) ([]*yaml.Node, error) {
	switch effectiveMode(opts.Mode) {
	case "key":
		if opts.Key == "" {
			return nil, fmt.Errorf("key mode requires values_key to be set")
		}
		node, err := findByKey(root, opts.Key)
		if err != nil {
			return nil, err
		}
		return []*yaml.Node{node}, nil
	case "marker":
		nodes := findByMarker(root)
		if len(nodes) == 0 {
			return nil, fmt.Errorf("marker # x-yaml-update not found")
		}
		return nodes, nil
	default: // "image"
		nodes := findImageTags(root)
		if len(nodes) == 0 {
			return nil, fmt.Errorf("no image block with tag: field found")
		}
		return nodes, nil
	}
}

func effectiveMode(m string) string {
	if m == "" {
		return "image"
	}
	return m
}

// findImageTags returns the value nodes of every `tag:` field that appears in a
// mapping containing both `repository` and `tag` keys (= Helm image block).
func findImageTags(root *yaml.Node) []*yaml.Node {
	var results []*yaml.Node
	walkNode(root, func(n *yaml.Node) {
		if n.Kind != yaml.MappingNode {
			return
		}
		var hasRepo bool
		var tagVal *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			val := n.Content[i+1]
			switch key.Value {
			case "repository":
				hasRepo = true
			case "tag":
				if val.Kind == yaml.ScalarNode {
					tagVal = val
				}
			}
		}
		if hasRepo && tagVal != nil {
			results = append(results, tagVal)
		}
	})
	return results
}

// findByKey follows a dot-notation path and returns the scalar value node at the leaf.
func findByKey(root *yaml.Node, path string) (*yaml.Node, error) {
	node := root
	if node.Kind == yaml.DocumentNode {
		if len(node.Content) == 0 {
			return nil, fmt.Errorf("empty document")
		}
		node = node.Content[0]
	}
	parts := strings.Split(path, ".")
	for i, part := range parts {
		if node.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("key %q: expected mapping at %q, got kind %d", path, strings.Join(parts[:i], "."), node.Kind)
		}
		var next *yaml.Node
		for j := 0; j+1 < len(node.Content); j += 2 {
			if node.Content[j].Value == part {
				next = node.Content[j+1]
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("key %q not found in YAML", path)
		}
		node = next
	}
	if node.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("key %q: value is not a scalar", path)
	}
	return node, nil
}

// findByMarker returns all scalar nodes whose LineComment contains "x-yaml-update".
func findByMarker(root *yaml.Node) []*yaml.Node {
	var results []*yaml.Node
	walkNode(root, func(n *yaml.Node) {
		if n.Kind == yaml.ScalarNode && strings.Contains(n.LineComment, "x-yaml-update") {
			results = append(results, n)
		}
	})
	return results
}

// walkNode visits every node in the tree depth-first.
func walkNode(n *yaml.Node, fn func(*yaml.Node)) {
	if n == nil {
		return
	}
	fn(n)
	for _, child := range n.Content {
		walkNode(child, fn)
	}
}

// replaceValueAtColumn replaces the value in line starting at colIdx (0-based)
// with newVal, preserving the prefix (key + colon + space) and any trailing
// whitespace + inline comment.
func replaceValueAtColumn(line []byte, colIdx int, newVal string) []byte {
	if colIdx < 0 || colIdx > len(line) {
		return line
	}
	prefix := line[:colIdx]
	rest := string(line[colIdx:])

	// End of value = first whitespace (before an inline comment) or EOL.
	// For version strings (no embedded spaces) this is safe.
	endIdx := strings.IndexAny(rest, " \t")
	suffix := ""
	if endIdx >= 0 {
		suffix = rest[endIdx:]
	}
	out := make([]byte, 0, len(prefix)+len(newVal)+len(suffix))
	out = append(out, prefix...)
	out = append(out, newVal...)
	out = append(out, suffix...)
	return out
}
