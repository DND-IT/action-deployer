// Package values updates the image.tag field in a Helm values YAML file
// without full parse-and-reserialize (preserves comments and formatting).
package values

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// SetImageTag finds the first `image:` block in the file at path and replaces
// the `tag:` value on the immediately following lines. Works for both
// `web.image.tag` and `worker.image.tag` without knowing the chart root.
//
// Returns an error if the file cannot be read, the image block is not found,
// or the tag field is not present within the block.
func SetImageTag(path, tag string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	updated, err := replaceImageTag(string(data), tag)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// replaceImageTag is the pure string manipulation logic, exposed for testing.
func replaceImageTag(content, newTag string) (string, error) {
	var (
		out        strings.Builder
		scanner    = bufio.NewScanner(strings.NewReader(content))
		inImage    bool
		tagReplaced bool
	)

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		switch {
		case !inImage && trimmed == "image:":
			inImage = true
			out.WriteString(line + "\n")

		case inImage && strings.HasPrefix(trimmed, "tag:"):
			// Preserve leading whitespace, replace value
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			out.WriteString(indent + "tag: " + newTag + "\n")
			inImage = false
			tagReplaced = true

		case inImage && (trimmed == "" || (!strings.HasPrefix(trimmed, "#") && !strings.Contains(line, ":"))):
			// Blank line or unindented content — left the image block without finding tag
			inImage = false
			out.WriteString(line + "\n")

		default:
			out.WriteString(line + "\n")
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scanning content: %w", err)
	}
	if !tagReplaced {
		return "", fmt.Errorf("tag field not found within image: block")
	}
	return out.String(), nil
}
