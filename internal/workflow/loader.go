package workflow

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	// ErrMissingFrontMatter marks a workflow file without a YAML front matter section.
	ErrMissingFrontMatter = errors.New("missing_workflow_front_matter")

	// ErrUnterminatedFrontMatter marks a front matter section without a closing delimiter.
	ErrUnterminatedFrontMatter = errors.New("unterminated_workflow_front_matter")

	// ErrInvalidFrontMatterYAML marks a syntactically invalid YAML front matter section.
	ErrInvalidFrontMatterYAML = errors.New("invalid_workflow_front_matter_yaml")

	// ErrNonMapFrontMatter marks a front matter section that does not decode to an object.
	ErrNonMapFrontMatter = errors.New("workflow_front_matter_not_map")

	// ErrEmptyPromptBody marks a workflow file with no usable Markdown prompt body.
	ErrEmptyPromptBody = errors.New("empty_workflow_prompt_body")
)

// Definition is the raw repository-owned workflow contract.
type Definition struct {
	Path           string
	Config         map[string]any
	PromptTemplate string
}

// Load reads and parses a workflow definition from path.
func Load(path string) (Definition, error) {
	definition, _, err := LoadBytes(path)
	return definition, err
}

// LoadBytes reads and parses a workflow definition from path while returning
// the raw file bytes used for content-change detection.
func LoadBytes(path string) (Definition, []byte, error) {
	if err := RequireReadable(path); err != nil {
		return Definition{}, nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Definition{}, nil, fmt.Errorf("workflow %q cannot be read: %w", path, err)
	}

	definition, err := Parse(path, string(data))
	if err != nil {
		return Definition{}, data, err
	}
	return definition, data, nil
}

// Parse parses a workflow definition from content while using path in diagnostics.
func Parse(path string, content string) (Definition, error) {
	frontMatter, promptBody, err := splitFrontMatter(path, content)
	if err != nil {
		return Definition{}, err
	}

	config, err := parseFrontMatter(path, frontMatter)
	if err != nil {
		return Definition{}, err
	}

	promptTemplate := strings.TrimSpace(promptBody)
	if promptTemplate == "" {
		return Definition{}, fmt.Errorf("%w: workflow %q prompt body section is empty", ErrEmptyPromptBody, path)
	}

	return Definition{
		Path:           path,
		Config:         config,
		PromptTemplate: promptTemplate,
	}, nil
}

func splitFrontMatter(path string, content string) (string, string, error) {
	content = strings.TrimPrefix(content, "\ufeff")

	firstLine, rest, ok := strings.Cut(content, "\n")
	if !ok {
		if isFrontMatterDelimiter(firstLine) {
			return "", "", fmt.Errorf(
				"%w: workflow %q front matter section is missing closing delimiter",
				ErrUnterminatedFrontMatter,
				path,
			)
		}
		return "", "", fmt.Errorf(
			"%w: workflow %q is missing YAML front matter section",
			ErrMissingFrontMatter,
			path,
		)
	}
	if !isFrontMatterDelimiter(firstLine) {
		return "", "", fmt.Errorf(
			"%w: workflow %q is missing YAML front matter section",
			ErrMissingFrontMatter,
			path,
		)
	}

	for offset := 0; ; {
		line, _, found := strings.Cut(rest[offset:], "\n")
		if isFrontMatterDelimiter(line) {
			frontMatter := rest[:offset]
			bodyStart := offset + len(line)
			if found {
				bodyStart++
			}
			return frontMatter, rest[bodyStart:], nil
		}
		if !found {
			break
		}
		offset += len(line) + 1
	}

	return "", "", fmt.Errorf(
		"%w: workflow %q front matter section is missing closing delimiter",
		ErrUnterminatedFrontMatter,
		path,
	)
}

func parseFrontMatter(path string, frontMatter string) (map[string]any, error) {
	var root any
	if err := yaml.Unmarshal([]byte(frontMatter), &root); err != nil {
		return nil, fmt.Errorf(
			"%w: workflow %q front matter section contains invalid YAML: %v",
			ErrInvalidFrontMatterYAML,
			path,
			err,
		)
	}

	config, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf(
			"%w: workflow %q front matter section must decode to a map/object, got %T",
			ErrNonMapFrontMatter,
			path,
			root,
		)
	}

	return config, nil
}

func isFrontMatterDelimiter(line string) bool {
	return strings.TrimSpace(strings.TrimSuffix(line, "\r")) == "---"
}
